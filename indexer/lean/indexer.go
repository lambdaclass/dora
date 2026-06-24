// Package lean is the lean-consensus indexer. It ingests blocks from an
// ethlambda node via the /lean/v0 API (SSE for the head, range backfill for
// history) and persists them into the lean DB schema.
//
// Unlike Dora's Ethereum indexer this drops the two-tier finalized/unfinalized
// state-transition machinery: lean has no epoch aggregation and the explorer
// does not replay state. Blocks are written to the slots table as they arrive;
// finality flags and the checkpoints table are updated from the
// finalized_checkpoint SSE frame and the /fork_choice endpoint.
package lean

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/dora/clients/consensus/lean"
	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

// Indexer drives ingestion from a single lean consensus client.
type Indexer struct {
	ctx    context.Context
	logger logrus.FieldLogger
	client lean.ConsensusRPCClient
	spec   *lean.ChainSpec

	mu            sync.RWMutex
	headSlot      uint64
	headRoot      lean.Root
	finalizedSlot uint64
	finalizedRoot lean.Root
	justifiedSlot uint64
	running       bool

	// In-memory reorg-aware caches (ported from Dora's beacon indexer). They
	// hold a back-reference to this Indexer and drive ingestion: every block at
	// or above the finalized slot lives here (the unfinalized tier); blocks
	// below finalized are flushed to the DB (the finalized tier) and pruned.
	blockCache *blockCache
	forkCache  *forkCache

	// Canonical head selection (LMD-GHOST) state. canonicalComputation is the
	// latest-block marker used to short-circuit recomputation when the cache is
	// unchanged (mirrors Dora's canonicalComputation).
	canonicalHeadMutex   sync.Mutex
	canonicalHead        lean.Root
	canonicalComputation lean.Root
}

// finalizedCheckpoint returns the currently tracked finalized slot and root.
// It is consumed by the fork cache during fork detection.
func (idx *Indexer) finalizedCheckpoint() (lean.Slot, lean.Root) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return lean.Slot(idx.finalizedSlot), idx.finalizedRoot
}

// NewIndexer constructs a lean indexer over the given client, wiring its
// in-memory block and fork caches.
func NewIndexer(ctx context.Context, logger logrus.FieldLogger, client lean.ConsensusRPCClient) *Indexer {
	idx := &Indexer{
		ctx:    ctx,
		logger: logger,
		client: client,
	}
	idx.blockCache = newBlockCache(idx)
	idx.forkCache = newForkCache(idx)
	idx.forkCache.lastForkId = 1
	idx.forkCache.finalizedForkId = 1
	return idx
}

// Start initializes the chain spec, runs an initial backfill, then enters the
// SSE ingestion loop (reconnecting with backfill on drop). It blocks until ctx
// is cancelled.
func (idx *Indexer) Start() error {
	if err := idx.initSpec(); err != nil {
		return fmt.Errorf("init spec: %w", err)
	}
	idx.indexValidators()

	idx.mu.Lock()
	idx.running = true
	idx.mu.Unlock()

	// Seed the finalized anchor (and the genesis block) so canonical selection
	// has a real anchor and fork detection a finalized fork id.
	idx.seedFinalized()

	// Initial backfill from genesis (or last known head) to the current head.
	if err := idx.backfillToHead(); err != nil {
		idx.logger.WithError(err).Warn("initial backfill failed; continuing with live stream")
	}

	idx.streamLoop()
	return idx.ctx.Err()
}

// seedFinalized fetches the node's finalized checkpoint (preferring fork choice,
// falling back to the justified checkpoint) and seeds idx.finalizedSlot/Root and
// the fork cache's finalized anchor. It also fetches and caches the finalized
// block itself so the canonical walk has a concrete anchor block.
func (idx *Indexer) seedFinalized() {
	var finalizedSlot lean.Slot
	var finalizedRoot lean.Root

	if fc, err := idx.client.GetForkChoice(idx.ctx); err == nil && !fc.Finalized.Root.IsZero() {
		finalizedSlot = fc.Finalized.Slot
		finalizedRoot = fc.Finalized.Root
	} else if cp, err := idx.client.GetJustifiedCheckpoint(idx.ctx); err == nil {
		// No finalized checkpoint yet (fresh chain): anchor on justified.
		finalizedSlot = cp.Slot
		finalizedRoot = cp.Root
	}

	idx.mu.Lock()
	idx.finalizedSlot = uint64(finalizedSlot)
	idx.finalizedRoot = finalizedRoot
	idx.mu.Unlock()

	// Cache the anchor block so computeCanonicalChain can descend from it.
	if !finalizedRoot.IsZero() {
		if anchor, err := idx.client.GetBlockByID(idx.ctx, finalizedRoot.String()); err == nil && anchor != nil {
			block, _ := idx.blockCache.createOrGetBlock(finalizedRoot, anchor.Slot)
			block.SetBlock(anchor)
			block.forkId = idx.forkCache.finalizedForkId
			block.forkChecked = true
			idx.blockCache.addBlockToParentMap(block)
		}
	}

	idx.logger.WithFields(logrus.Fields{
		"finalized_slot": uint64(finalizedSlot),
		"finalized_root": finalizedRoot.String(),
	}).Info("seeded finalized anchor")
}

func (idx *Indexer) initSpec() error {
	genesis, err := idx.client.GetGenesis(idx.ctx)
	if err != nil {
		return fmt.Errorf("get genesis: %w", err)
	}
	spec, err := idx.client.GetSpec(idx.ctx)
	if err != nil {
		return fmt.Errorf("get spec: %w", err)
	}
	idx.spec = lean.NewChainSpec(genesis, spec)
	idx.logger.WithFields(logrus.Fields{
		"genesis_time":    idx.spec.GenesisTime,
		"ms_per_slot":     idx.spec.MillisecondsPerSlot,
		"validator_count": idx.spec.ValidatorCount,
	}).Info("initialized lean chain spec")
	return nil
}

// Spec returns the chain spec (available after Start has initialized it).
func (idx *Indexer) Spec() *lean.ChainSpec {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.spec
}

// indexValidators fetches the fork-choice validator count and, if a full
// validator registry endpoint were available, would persist it. ethlambda's
// read API exposes validator_count but not the full registry over a dedicated
// endpoint, so we record the count; the registry is best-effort.
func (idx *Indexer) indexValidators() {
	fc, err := idx.client.GetForkChoice(idx.ctx)
	if err != nil {
		idx.logger.WithError(err).Debug("could not fetch fork choice for validator count")
		return
	}
	idx.logger.WithField("validator_count", fc.ValidatorCount).Info("validator registry size")
}

// backfillToHead fetches blocks in pages from the last indexed slot up to the
// node's current head. It applies the two-tier model: blocks below the
// finalized slot are written straight to the DB finalized tier and never enter
// the cache; blocks at or above the finalized slot are loaded into the
// blockCache and fed through fork detection. After seeding the cache it runs
// canonical selection once and persists the canonical/orphaned diff.
func (idx *Indexer) backfillToHead() error {
	sync, err := idx.client.GetSyncState(idx.ctx)
	if err != nil {
		return fmt.Errorf("get sync state: %w", err)
	}
	target := sync.HeadSlot

	start := uint64(0)
	if head, ok, err := db.GetHeadSlot(idx.ctx); err == nil && ok {
		start = head + 1
	}

	_, finalizedRootSnapshot := idx.finalizedCheckpoint()
	finalizedSlot, _ := idx.finalizedCheckpoint()

	const pageSize = uint64(256)
	cachedAny := false
	for from := start; from <= target; from += pageSize {
		select {
		case <-idx.ctx.Done():
			return idx.ctx.Err()
		default:
		}
		blocks, err := idx.client.GetBlocksByRange(idx.ctx, from, pageSize)
		if err != nil {
			return fmt.Errorf("get blocks [%d,+%d): %w", from, pageSize, err)
		}
		// The range endpoint returns blocks without their roots. We still derive
		// a root per block from the parent-chain (the only available source),
		// then route each block to the finalized tier (DB) or unfinalized tier
		// (cache) by slot.
		roots := idx.resolveBackfillRoots(blocks)
		for i, b := range blocks {
			if b.Slot < finalizedSlot {
				// Finalized tier: write straight to the DB, never cache.
				if err := idx.persistFinalizedTierBlock(b, roots[i], dbtypes.Canonical, true); err != nil {
					idx.logger.WithError(err).WithField("slot", b.Slot).Warn("failed to persist finalized-tier block")
				}
				continue
			}
			// Unfinalized tier: load into the cache and run fork detection.
			idx.ingestCacheBlock(b, roots[i], 0)
			cachedAny = true
		}
		idx.logger.WithFields(logrus.Fields{"from": from, "count": len(blocks)}).Debug("backfilled block page")
	}

	_ = finalizedRootSnapshot
	if cachedAny {
		idx.computeAndPersistCanonical()
	}
	idx.updateFinality()
	return nil
}

// ingestCacheBlock loads a block into the unfinalized-tier cache: it creates the
// cache node, attaches the body, links it under its parent, and runs fork
// detection. recvDelay is the ms after the slot start at which the block was
// first seen (0 for backfill).
func (idx *Indexer) ingestCacheBlock(b *lean.Block, root lean.Root, recvDelay int32) *Block {
	block, _ := idx.blockCache.createOrGetBlock(root, b.Slot)
	block.SetBlock(b)
	if recvDelay > 0 {
		block.SetSeen(time.Now(), recvDelay)
	}
	idx.blockCache.addBlockToParentMap(block)
	if err := idx.forkCache.processBlock(block); err != nil {
		idx.logger.WithError(err).WithField("slot", b.Slot).Debug("fork detection failed")
	}
	return block
}

// streamLoop subscribes to the SSE stream and reconnects (with a backfill of
// the gap) on any drop, until ctx is cancelled.
func (idx *Indexer) streamLoop() {
	for {
		select {
		case <-idx.ctx.Done():
			return
		default:
		}

		ch, err := idx.client.StreamEvents(idx.ctx)
		if err != nil {
			idx.logger.WithError(err).Warn("event stream connect failed; retrying")
			if !idx.sleep(3 * time.Second) {
				return
			}
			continue
		}
		idx.logger.Info("event stream connected")

		for ev := range ch {
			idx.handleEvent(ev)
		}

		// Stream ended: backfill the gap, then reconnect.
		idx.logger.Warn("event stream closed; backfilling gap and reconnecting")
		if err := idx.backfillToHead(); err != nil {
			idx.logger.WithError(err).Warn("gap backfill failed")
		}
		if !idx.sleep(1 * time.Second) {
			return
		}
	}
}

func (idx *Indexer) sleep(d time.Duration) bool {
	select {
	case <-idx.ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func (idx *Indexer) handleEvent(ev lean.StreamEvent) {
	if ev.Err != nil {
		idx.logger.WithError(ev.Err).Debug("malformed SSE frame")
		return
	}
	switch ev.Type {
	case lean.StreamEventBlock:
		idx.onBlockEvent(ev.Block)
	case lean.StreamEventHead:
		idx.onHeadEvent(ev.Head)
	case lean.StreamEventFinalizedCheckpoint:
		idx.onFinalizedEvent(ev.Finalized)
	}
}

// onBlockEvent ingests a freshly imported block into the unfinalized-tier
// cache, runs fork detection + canonical selection, and persists the resulting
// canonical/orphaned diff (plus votes, duty, and the unfinalized-block blob).
func (idx *Indexer) onBlockEvent(d *lean.BlockEventData) {
	block, err := idx.client.GetBlockByID(idx.ctx, d.Root.String())
	if err != nil {
		idx.logger.WithError(err).WithField("root", d.Root).Warn("failed to fetch block from event")
		return
	}
	recvDelay := idx.recvDelayMs(uint64(block.Slot))
	cacheBlock := idx.ingestCacheBlock(block, d.Root, recvDelay)

	// Cache the raw block blob for reorg replay (unfinalized tier).
	if blob, err := json.Marshal(block); err == nil {
		ub := &dbtypes.UnfinalizedBlock{
			Root:      d.Root.Bytes(),
			Slot:      uint64(block.Slot),
			Status:    dbtypes.UnfinalizedBlockStatusProcessed,
			BlockData: blob,
		}
		err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
			return db.InsertUnfinalizedBlock(idx.ctx, ub, tx)
		})
		if err != nil {
			idx.logger.WithError(err).Debug("failed to cache unfinalized block blob")
		}
	}
	_ = cacheBlock

	idx.computeAndPersistCanonical()
}

// onHeadEvent recomputes the canonical head; if the head moved and an old head
// is known, it computes the reorg (depth/forward/common ancestor), logs it, and
// persists the canonical/orphaned status flips. Replaces the old fork-choice
// ancestor walk (reconcileCanonical).
func (idx *Indexer) onHeadEvent(d *lean.HeadEventData) {
	idx.mu.Lock()
	oldHeadRoot := idx.headRoot
	idx.headSlot = d.Slot
	idx.headRoot = d.Root
	idx.mu.Unlock()
	idx.logger.WithFields(logrus.Fields{"slot": d.Slot, "root": d.Root.String()}).Debug("head updated")

	newHead, _, changed := idx.computeCanonicalChain()

	if changed && !oldHeadRoot.IsZero() {
		oldBlock := idx.blockCache.getBlockByRoot(oldHeadRoot)
		newBlock := idx.blockCache.getBlockByRoot(newHead)
		if oldBlock != nil && newBlock != nil && oldBlock != newBlock {
			if reorg := idx.processReorg(oldBlock, newBlock); reorg != nil {
				idx.logger.WithFields(logrus.Fields{
					"depth":           reorg.Depth,
					"forward":         reorg.ForwardDistance,
					"old_head":        reorg.OldHead.String(),
					"new_head":        reorg.NewHead.String(),
					"common_ancestor": reorg.CommonAncestor.String(),
				}).Info("reorg detected")
			}
		}
	}

	idx.persistCanonicalDiff()
	idx.crossCheckHead(newHead)
	idx.updateFinality()
}

// onFinalizedEvent advances finalization: flush cache blocks at/below the
// finalized slot to the DB finalized tier and prune them.
func (idx *Indexer) onFinalizedEvent(d *lean.FinalizedCheckpointEventData) {
	idx.finalizeBelow(lean.Slot(d.Slot), d.Root)
	idx.markFinalized(d.Slot)
	idx.logger.WithFields(logrus.Fields{"slot": d.Slot, "root": d.Root.String()}).Info("finalized checkpoint")
}

// computeAndPersistCanonical recomputes the canonical chain, cross-checks it
// against the node, and persists the canonical/orphaned diff for cached blocks.
func (idx *Indexer) computeAndPersistCanonical() {
	head, _, _ := idx.computeCanonicalChain()
	idx.persistCanonicalDiff()
	idx.crossCheckHead(head)
}

// persistCanonicalDiff writes the current canonical/orphaned status for every
// cached (unfinalized-tier) block to the slots table. Each cache block's slot
// row reflects whether it lies on the canonical chain to the computed head.
func (idx *Indexer) persistCanonicalDiff() {
	head := idx.canonicalHead
	canonical := idx.canonicalSet(head)

	blocks := idx.blockCache.getAllBlocks()
	if len(blocks) == 0 {
		return
	}

	err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
		for _, block := range blocks {
			body := block.GetBody()
			if body == nil {
				continue
			}
			status := dbtypes.Orphaned
			if canonical[block.Root] {
				status = dbtypes.Canonical
			}
			if err := idx.persistCacheBlock(tx, block, status, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		idx.logger.WithError(err).Debug("canonical diff persist failed")
	}
}

// crossCheckHead compares the locally computed head against the node's
// authoritative fork-choice head and logs any divergence. The node is
// authoritative; we only log (do not override) so display stays consistent
// while divergence is observable.
func (idx *Indexer) crossCheckHead(computed lean.Root) {
	fc, err := idx.client.GetForkChoice(idx.ctx)
	if err != nil || fc == nil {
		return
	}
	if !fc.Head.IsZero() && fc.Head != computed {
		idx.logger.WithFields(logrus.Fields{
			"computed_head": computed.String(),
			"node_head":     fc.Head.String(),
		}).Warnf("computed head diverges from node fork-choice head")
	}
}

// persistFinalizedTierBlock writes a finalized-tier (below-finalized) block
// straight to the DB without caching it: the slot row (finalized), the proposer
// duty, and the per-validator votes, in a single transaction.
func (idx *Indexer) persistFinalizedTierBlock(b *lean.Block, root lean.Root, status dbtypes.SlotStatus, finalized bool) error {
	slotRow := &dbtypes.Slot{
		Slot:             uint64(b.Slot),
		Root:             root.Bytes(),
		ParentRoot:       b.ParentRoot.Bytes(),
		StateRoot:        b.StateRoot.Bytes(),
		Proposer:         b.ProposerIndex,
		Status:           status,
		AttestationCount: uint64(len(b.Body.Attestations)),
		Finalized:        finalized,
	}
	if blob, err := json.Marshal(b); err == nil {
		slotRow.BlockSize = uint64(len(blob))
	}

	return db.RunDBTransaction(func(tx *sqlx.Tx) error {
		if err := db.InsertSlot(idx.ctx, slotRow, tx); err != nil {
			return err
		}
		duty := &dbtypes.ValidatorDuty{Slot: uint64(b.Slot), Proposer: b.ProposerIndex, Fulfilled: status == dbtypes.Canonical}
		if err := db.InsertValidatorDuty(idx.ctx, duty, tx); err != nil {
			return err
		}
		for _, att := range b.Body.Attestations {
			for _, vi := range setBits(att.AggregationBits) {
				vote := &dbtypes.Vote{
					Slot:           uint64(att.Data.Slot),
					ValidatorIndex: vi,
					SourceSlot:     uint64(att.Data.Source.Slot),
					SourceRoot:     att.Data.Source.Root.Bytes(),
					TargetSlot:     uint64(att.Data.Target.Slot),
					TargetRoot:     att.Data.Target.Root.Bytes(),
					HeadRoot:       att.Data.Head.Root.Bytes(),
				}
				if err := db.InsertVote(idx.ctx, vote, tx); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// updateFinality refreshes the justified checkpoint from the node and records it.
func (idx *Indexer) updateFinality() {
	cp, err := idx.client.GetJustifiedCheckpoint(idx.ctx)
	if err != nil {
		idx.logger.WithError(err).Debug("justified checkpoint fetch failed")
		return
	}
	idx.mu.Lock()
	idx.justifiedSlot = uint64(cp.Slot)
	idx.mu.Unlock()
	err = db.RunDBTransaction(func(tx *sqlx.Tx) error {
		row := &dbtypes.Checkpoint{Slot: uint64(cp.Slot), Root: cp.Root.Bytes(), Type: dbtypes.CheckpointJustified}
		return db.InsertCheckpoint(idx.ctx, row, tx)
	})
	if err != nil {
		idx.logger.WithError(err).Debug("justified checkpoint write failed")
		return
	}
	idx.markJustified(uint64(cp.Slot))
}

func (idx *Indexer) markFinalized(slot uint64) {
	err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
		_, e := tx.ExecContext(idx.ctx, `UPDATE slots SET finalized = $1 WHERE slot <= $2 AND status = $3`,
			true, slot, dbtypes.Canonical)
		return e
	})
	if err != nil {
		idx.logger.WithError(err).Debug("mark finalized failed")
	}
}

func (idx *Indexer) markJustified(slot uint64) {
	err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
		_, e := tx.ExecContext(idx.ctx, `UPDATE slots SET justified = $1 WHERE slot = $2 AND status = $3`,
			true, slot, dbtypes.Canonical)
		return e
	})
	if err != nil {
		idx.logger.WithError(err).Debug("mark justified failed")
	}
}

// recvDelayMs returns the milliseconds between the slot's wall-clock start and now.
func (idx *Indexer) recvDelayMs(slot uint64) int32 {
	if idx.spec == nil {
		return 0
	}
	delay := time.Since(idx.spec.SlotToTime(slot)).Milliseconds()
	if delay < 0 {
		delay = 0
	}
	return int32(delay)
}

// HeadState returns a snapshot of the tracked head/finality slots.
func (idx *Indexer) HeadState() (headSlot, justifiedSlot, finalizedSlot uint64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.headSlot, idx.justifiedSlot, idx.finalizedSlot
}
