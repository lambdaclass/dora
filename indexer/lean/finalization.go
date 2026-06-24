package lean

import (
	"encoding/json"

	"github.com/jmoiron/sqlx"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

// finalizeBelow advances finalization to (finalizedSlot, finalizedRoot): it
// flushes every cached block at or below the finalized slot to the DB finalized
// tier (canonical with finalized=true, or orphaned), then prunes those blocks
// from the in-memory cache.
//
// This is the lean port of Dora's finalizeEpoch, with the epoch loop removed:
// it operates directly on the raw finalized slot range. The two-tier model is
//
//	unfinalized tier = in-memory cache (slot >= finalizedSlot)
//	finalized tier   = DB (slot <= finalizedSlot)
//
// All DB writes happen in a single transaction.
func (idx *Indexer) finalizeBelow(finalizedSlot leanapi.Slot, finalizedRoot leanapi.Root) {
	idx.mu.Lock()
	idx.finalizedSlot = uint64(finalizedSlot)
	idx.finalizedRoot = finalizedRoot
	idx.mu.Unlock()

	// Update the fork cache's finalized anchor (drops pre-finalized forks).
	idx.forkCache.setFinalizedSlot(finalizedSlot, finalizedRoot)

	// Recompute the canonical chain so the flush below knows which blocks are
	// canonical vs orphaned at finalization time.
	_, canonical, _ := idx.computeCanonicalChain()

	// Collect the blocks to flush (slot <= finalizedSlot). getCleanupBlocks
	// returns blocks strictly below finalizedSlot; we also flush blocks AT the
	// finalized slot (the finalized checkpoint block itself).
	flush := idx.blockCache.getCleanupBlocks(finalizedSlot)
	for _, block := range idx.blockCache.getBlocksBySlot(finalizedSlot) {
		flush = append(flush, block)
	}

	if len(flush) > 0 {
		err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
			for _, block := range flush {
				status := dbtypes.Orphaned
				finalized := false
				if canonical[block.Root] {
					status = dbtypes.Canonical
					finalized = true
				}
				if err := idx.persistCacheBlock(tx, block, status, finalized); err != nil {
					return err
				}
			}
			// Drop the cached unfinalized-block blobs below the finalized slot.
			return db.DeleteUnfinalizedBlocksBelow(idx.ctx, uint64(finalizedSlot), tx)
		})
		if err != nil {
			idx.logger.WithError(err).Warn("finalization flush failed")
			return
		}
	}

	// Record the finalized checkpoint.
	err := db.RunDBTransaction(func(tx *sqlx.Tx) error {
		cp := &dbtypes.Checkpoint{
			Slot: uint64(finalizedSlot),
			Root: finalizedRoot.Bytes(),
			Type: dbtypes.CheckpointFinalized,
		}
		return db.InsertCheckpoint(idx.ctx, cp, tx)
	})
	if err != nil {
		idx.logger.WithError(err).Warn("failed to record finalized checkpoint")
	}

	// Prune: remove every block at or below the finalized slot from the cache.
	pruned := 0
	for _, block := range flush {
		idx.blockCache.removeBlock(block)
		pruned++
	}

	idx.logger.WithField("finalized_slot", uint64(finalizedSlot)).
		WithField("pruned", pruned).
		Info("finalized: flushed cache to DB and pruned")
}

// persistCacheBlock writes a finalized cache block to the DB: the slot row (with
// the given canonical status and finalized flag), the proposer duty, and the
// per-validator votes from its attestations. The unfinalized-blocks blob is not
// written here; it is dropped on finalization.
func (idx *Indexer) persistCacheBlock(tx *sqlx.Tx, block *Block, status dbtypes.SlotStatus, finalized bool) error {
	body := block.GetBody()
	if body == nil {
		// No body cached: write a minimal slot row so the row reflects the
		// finalized status, but skip votes/duty we cannot derive.
		slotRow := &dbtypes.Slot{
			Slot:       uint64(block.Slot),
			Root:       block.Root.Bytes(),
			ParentRoot: block.GetParentRoot().Bytes(),
			StateRoot:  block.GetStateRoot().Bytes(),
			Proposer:   block.GetProposerIndex(),
			Status:     status,
			Finalized:  finalized,
			RecvDelay:  block.GetRecvDelay(),
		}
		return db.InsertSlot(idx.ctx, slotRow, tx)
	}

	slotRow := &dbtypes.Slot{
		Slot:             uint64(block.Slot),
		Root:             block.Root.Bytes(),
		ParentRoot:       body.ParentRoot.Bytes(),
		StateRoot:        body.StateRoot.Bytes(),
		Proposer:         body.ProposerIndex,
		Status:           status,
		AttestationCount: uint64(len(body.Body.Attestations)),
		Finalized:        finalized,
		RecvDelay:        block.GetRecvDelay(),
	}
	if blob, err := json.Marshal(body); err == nil {
		slotRow.BlockSize = uint64(len(blob))
	}

	if err := db.InsertSlot(idx.ctx, slotRow, tx); err != nil {
		return err
	}

	duty := &dbtypes.ValidatorDuty{Slot: uint64(block.Slot), Proposer: body.ProposerIndex, Fulfilled: status == dbtypes.Canonical}
	if err := db.InsertValidatorDuty(idx.ctx, duty, tx); err != nil {
		return err
	}

	for _, att := range body.Body.Attestations {
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
}
