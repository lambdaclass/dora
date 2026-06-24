package db

import (
	"context"

	"github.com/ethpandaops/dora/dbtypes"
	"github.com/jmoiron/sqlx"
)

// This file holds the lean-consensus DB writers and readers. All upserts use
// EngineQuery to bridge the SQLite/Postgres "ON CONFLICT" syntax differences.

// InsertSlot upserts a slot row (keyed by slot+root).
func InsertSlot(ctx context.Context, slot *dbtypes.Slot, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO slots (slot, root, parent_root, state_root, proposer, status,
				attestation_count, justified, finalized, block_size, recv_delay)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (slot, root) DO UPDATE SET
				parent_root = excluded.parent_root,
				state_root = excluded.state_root,
				proposer = excluded.proposer,
				status = excluded.status,
				attestation_count = excluded.attestation_count,
				justified = excluded.justified,
				finalized = excluded.finalized,
				block_size = excluded.block_size,
				recv_delay = excluded.recv_delay`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO slots (slot, root, parent_root, state_root, proposer, status,
				attestation_count, justified, finalized, block_size, recv_delay)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
	}),
		slot.Slot, slot.Root, slot.ParentRoot, slot.StateRoot, slot.Proposer, slot.Status,
		slot.AttestationCount, slot.Justified, slot.Finalized, slot.BlockSize, slot.RecvDelay)
	return err
}

// GetSlotsByRange returns canonical+orphaned slots in [minSlot, maxSlot],
// newest first, capped by limit.
func GetSlotsByRange(ctx context.Context, minSlot, maxSlot uint64, limit uint64) ([]*dbtypes.Slot, error) {
	slots := []*dbtypes.Slot{}
	err := ReaderDb.SelectContext(ctx, &slots, `
		SELECT slot, root, parent_root, state_root, proposer, status,
			attestation_count, justified, finalized, block_size, recv_delay
		FROM slots
		WHERE slot >= $1 AND slot <= $2
		ORDER BY slot DESC, status ASC
		LIMIT $3`, minSlot, maxSlot, limit)
	if err != nil {
		return nil, err
	}
	return slots, nil
}

// GetSlotByRoot returns the slot row for a block root, or nil if absent.
func GetSlotByRoot(ctx context.Context, root []byte) (*dbtypes.Slot, error) {
	slot := &dbtypes.Slot{}
	err := ReaderDb.GetContext(ctx, slot, `
		SELECT slot, root, parent_root, state_root, proposer, status,
			attestation_count, justified, finalized, block_size, recv_delay
		FROM slots WHERE root = $1`, root)
	if err != nil {
		return nil, err
	}
	return slot, nil
}

// GetHeadSlot returns the highest canonical slot number, or false if empty.
func GetHeadSlot(ctx context.Context) (uint64, bool, error) {
	var slot *uint64
	err := ReaderDb.GetContext(ctx, &slot, `SELECT MAX(slot) FROM slots WHERE status = $1`, dbtypes.Canonical)
	if err != nil {
		return 0, false, err
	}
	if slot == nil {
		return 0, false, nil
	}
	return *slot, true, nil
}

// InsertCheckpoint upserts a finality checkpoint (keyed by slot+type).
func InsertCheckpoint(ctx context.Context, cp *dbtypes.Checkpoint, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO checkpoints (slot, root, type) VALUES ($1,$2,$3)
			ON CONFLICT (slot, type) DO UPDATE SET root = excluded.root`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO checkpoints (slot, root, type) VALUES ($1,$2,$3)`,
	}), cp.Slot, cp.Root, cp.Type)
	return err
}

// GetCheckpoints returns checkpoints of a type, newest first, capped by limit.
func GetCheckpoints(ctx context.Context, cpType dbtypes.CheckpointType, limit uint64) ([]*dbtypes.Checkpoint, error) {
	cps := []*dbtypes.Checkpoint{}
	err := ReaderDb.SelectContext(ctx, &cps, `
		SELECT slot, root, type FROM checkpoints WHERE type = $1 ORDER BY slot DESC LIMIT $2`,
		cpType, limit)
	if err != nil {
		return nil, err
	}
	return cps, nil
}

// InsertValidator upserts a validator registry entry.
func InsertValidator(ctx context.Context, v *dbtypes.Validator, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO validators ("index", attestation_pubkey, proposal_pubkey) VALUES ($1,$2,$3)
			ON CONFLICT ("index") DO UPDATE SET
				attestation_pubkey = excluded.attestation_pubkey,
				proposal_pubkey = excluded.proposal_pubkey`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO validators ("index", attestation_pubkey, proposal_pubkey) VALUES ($1,$2,$3)`,
	}), v.Index, v.AttestationPubkey, v.ProposalPubkey)
	return err
}

// GetValidators returns the full validator registry ordered by index.
func GetValidators(ctx context.Context) ([]*dbtypes.Validator, error) {
	vs := []*dbtypes.Validator{}
	err := ReaderDb.SelectContext(ctx, &vs, `
		SELECT "index", attestation_pubkey, proposal_pubkey FROM validators ORDER BY "index" ASC`)
	if err != nil {
		return nil, err
	}
	return vs, nil
}

// GetValidatorCount returns the number of validators in the registry.
func GetValidatorCount(ctx context.Context) (uint64, error) {
	var count uint64
	err := ReaderDb.GetContext(ctx, &count, `SELECT COUNT(*) FROM validators`)
	return count, err
}

// InsertValidatorDuty upserts the proposer duty for a slot.
func InsertValidatorDuty(ctx context.Context, d *dbtypes.ValidatorDuty, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO validator_duties (slot, proposer, fulfilled) VALUES ($1,$2,$3)
			ON CONFLICT (slot) DO UPDATE SET proposer = excluded.proposer, fulfilled = excluded.fulfilled`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO validator_duties (slot, proposer, fulfilled) VALUES ($1,$2,$3)`,
	}), d.Slot, d.Proposer, d.Fulfilled)
	return err
}

// InsertVote upserts a single attestation/vote (keyed by slot+validator).
func InsertVote(ctx context.Context, v *dbtypes.Vote, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO votes (slot, validator_index, source_slot, source_root, target_slot, target_root, head_root)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (slot, validator_index) DO UPDATE SET
				source_slot = excluded.source_slot, source_root = excluded.source_root,
				target_slot = excluded.target_slot, target_root = excluded.target_root,
				head_root = excluded.head_root`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO votes (slot, validator_index, source_slot, source_root, target_slot, target_root, head_root)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
	}), v.Slot, v.ValidatorIndex, v.SourceSlot, v.SourceRoot, v.TargetSlot, v.TargetRoot, v.HeadRoot)
	return err
}

// GetVotesForSlot returns all votes recorded for a slot.
func GetVotesForSlot(ctx context.Context, slot uint64) ([]*dbtypes.Vote, error) {
	votes := []*dbtypes.Vote{}
	err := ReaderDb.SelectContext(ctx, &votes, `
		SELECT slot, validator_index, source_slot, source_root, target_slot, target_root, head_root
		FROM votes WHERE slot = $1 ORDER BY validator_index ASC`, slot)
	if err != nil {
		return nil, err
	}
	return votes, nil
}

// CountVotesForSlot returns the number of distinct validators that voted for a slot.
func CountVotesForSlot(ctx context.Context, slot uint64) (uint64, error) {
	var count uint64
	err := ReaderDb.GetContext(ctx, &count, `SELECT COUNT(*) FROM votes WHERE slot = $1`, slot)
	return count, err
}

// InsertUnfinalizedBlock upserts a near-head block blob.
func InsertUnfinalizedBlock(ctx context.Context, b *dbtypes.UnfinalizedBlock, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, EngineQuery(map[dbtypes.DBEngineType]string{
		dbtypes.DBEnginePgsql: `
			INSERT INTO unfinalized_blocks (root, slot, status, block_data) VALUES ($1,$2,$3,$4)
			ON CONFLICT (root) DO UPDATE SET slot = excluded.slot, status = excluded.status, block_data = excluded.block_data`,
		dbtypes.DBEngineSqlite: `
			INSERT OR REPLACE INTO unfinalized_blocks (root, slot, status, block_data) VALUES ($1,$2,$3,$4)`,
	}), b.Root, b.Slot, b.Status, b.BlockData)
	return err
}

// DeleteUnfinalizedBlocksBelow prunes unfinalized blocks at or below a slot
// (called once they are finalized and written to the slots table).
func DeleteUnfinalizedBlocksBelow(ctx context.Context, slot uint64, tx *sqlx.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM unfinalized_blocks WHERE slot <= $1`, slot)
	return err
}
