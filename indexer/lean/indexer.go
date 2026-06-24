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
	// hold a back-reference to this Indexer, exactly as Dora's caches do. The
	// caches are not yet wired into the ingestion path; a later task connects
	// them.
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

// NewIndexer constructs a lean indexer over the given client.
func NewIndexer(ctx context.Context, logger logrus.FieldLogger, client lean.ConsensusRPCClient) *Indexer {
	return &Indexer{
		ctx:    ctx,
		logger: logger,
		client: client,
	}
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

	// Initial backfill from genesis (or last known head) to the current head.
	if err := idx.backfillToHead(); err != nil {
		idx.logger.WithError(err).Warn("initial backfill failed; continuing with live stream")
	}

	idx.streamLoop()
	return idx.ctx.Err()
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
// node's current head and persists them.
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

	const pageSize = uint64(256)
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
		// The range endpoint returns blocks without their roots. We chain roots
		// from each block's child parent_root (blocks come back slot-ordered),
		// and for the last block resolve its root via the block-header lookup.
		roots := idx.resolveBackfillRoots(blocks)
		for i, b := range blocks {
			if err := idx.persistBlock(b, roots[i], dbtypes.Canonical, 0); err != nil {
				idx.logger.WithError(err).WithField("slot", b.Slot).Warn("failed to persist backfilled block")
			}
		}
		idx.logger.WithFields(logrus.Fields{"from": from, "count": len(blocks)}).Debug("backfilled block page")
	}
	idx.updateFinality()
	return nil
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

// onBlockEvent fetches and persists a freshly imported block.
func (idx *Indexer) onBlockEvent(d *lean.BlockEventData) {
	block, err := idx.client.GetBlockByID(idx.ctx, d.Root.String())
	if err != nil {
		idx.logger.WithError(err).WithField("root", d.Root).Warn("failed to fetch block from event")
		return
	}
	recvDelay := idx.recvDelayMs(uint64(block.Slot))
	if err := idx.persistBlock(block, d.Root, dbtypes.Canonical, recvDelay); err != nil {
		idx.logger.WithError(err).WithField("slot", block.Slot).Warn("failed to persist block")
	}
}

// onHeadEvent updates the tracked head and refreshes finality + reorg status.
func (idx *Indexer) onHeadEvent(d *lean.HeadEventData) {
	idx.mu.Lock()
	idx.headSlot = d.Slot
	idx.headRoot = d.Root
	idx.mu.Unlock()
	idx.logger.WithFields(logrus.Fields{"slot": d.Slot, "root": d.Root.String()}).Debug("head updated")
	idx.reconcileCanonical()
	idx.updateFinality()
}

// onFinalizedEvent records a finalized checkpoint and marks slots finalized.
func (idx *Indexer) onFinalizedEvent(d *lean.FinalizedCheckpointEventData) {
	idx.mu.Lock()
	idx.finalizedSlot = d.Slot
	idx.mu.Unlock()
	err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
		cp := &dbtypes.Checkpoint{Slot: d.Slot, Root: d.Root.Bytes(), Type: dbtypes.CheckpointFinalized}
		if err := db.InsertCheckpoint(idx.ctx, cp, tx); err != nil {
			return err
		}
		return db.DeleteUnfinalizedBlocksBelow(idx.ctx, d.Slot, tx)
	})
	if err != nil {
		idx.logger.WithError(err).Warn("failed to record finalized checkpoint")
		return
	}
	idx.markFinalized(d.Slot)
	idx.logger.WithFields(logrus.Fields{"slot": d.Slot, "root": d.Root.String()}).Info("finalized checkpoint")
}

// reconcileCanonical walks the fork-choice tree and marks any slot rows not on
// the canonical chain as orphaned. This is the reorg handler.
func (idx *Indexer) reconcileCanonical() {
	fc, err := idx.client.GetForkChoice(idx.ctx)
	if err != nil {
		idx.logger.WithError(err).Debug("fork choice fetch failed during reconcile")
		return
	}
	// Build the canonical ancestor set by walking from head to finalized.
	byRoot := make(map[lean.Root]lean.ForkChoiceNode, len(fc.Nodes))
	for _, n := range fc.Nodes {
		byRoot[n.Root] = n
	}
	canonical := make(map[lean.Root]bool)
	cur := fc.Head
	for {
		node, ok := byRoot[cur]
		if !ok {
			break
		}
		canonical[cur] = true
		if node.ParentRoot.IsZero() || node.ParentRoot == cur {
			break
		}
		cur = node.ParentRoot
	}
	// Any fork-choice node not in the canonical set is an orphan.
	err = db.RunDBTransaction(func(tx *sqlx.Tx) error {
		for _, n := range fc.Nodes {
			status := dbtypes.Orphaned
			if canonical[n.Root] {
				status = dbtypes.Canonical
			}
			existing, gerr := db.GetSlotByRoot(idx.ctx, n.Root.Bytes())
			if gerr != nil || existing == nil {
				continue
			}
			if existing.Status != status {
				existing.Status = status
				if err := db.InsertSlot(idx.ctx, existing, tx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		idx.logger.WithError(err).Debug("canonical reconcile write failed")
	}
}

// persistBlock writes a block (and its attestations as votes, and its proposer
// duty) into the DB inside a single transaction.
func (idx *Indexer) persistBlock(b *lean.Block, root lean.Root, status dbtypes.SlotStatus, recvDelay int32) error {
	slotRow := &dbtypes.Slot{
		Slot:             uint64(b.Slot),
		Root:             root.Bytes(),
		ParentRoot:       b.ParentRoot.Bytes(),
		StateRoot:        b.StateRoot.Bytes(),
		Proposer:         b.ProposerIndex,
		Status:           status,
		AttestationCount: uint64(len(b.Body.Attestations)),
		RecvDelay:        recvDelay,
	}
	if blob, err := json.Marshal(b); err == nil {
		slotRow.BlockSize = uint64(len(blob))
	}

	return db.RunDBTransaction(func(tx *sqlx.Tx) error {
		if err := db.InsertSlot(idx.ctx, slotRow, tx); err != nil {
			return err
		}
		duty := &dbtypes.ValidatorDuty{Slot: uint64(b.Slot), Proposer: b.ProposerIndex, Fulfilled: true}
		if err := db.InsertValidatorDuty(idx.ctx, duty, tx); err != nil {
			return err
		}
		// Persist each aggregated attestation as per-validator votes.
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
		// Cache the raw block near head for reorg replay.
		if blob, err := json.Marshal(b); err == nil {
			ub := &dbtypes.UnfinalizedBlock{
				Root:      root.Bytes(),
				Slot:      uint64(b.Slot),
				Status:    dbtypes.UnfinalizedBlockStatusProcessed,
				BlockData: blob,
			}
			if err := db.InsertUnfinalizedBlock(idx.ctx, ub, tx); err != nil {
				return err
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
