package dbtypes

// dbtypes holds the row structs persisted by the lean explorer. The schema is
// lean-consensus native (3SF-mini): no epochs, no execution layer, no balances.

// ExplorerState is a generic key/value store row (pruning state, sync markers).
type ExplorerState struct {
	Key   string `db:"key"`
	Value string `db:"value"`
}

// ValidatorName maps a validator index to a human-readable name.
type ValidatorName struct {
	Index uint64 `db:"index"`
	Name  string `db:"name"`
}

// SlotStatus describes the canonical state of a slot.
type SlotStatus uint8

const (
	// Missing: no canonical block was proposed for this slot.
	Missing SlotStatus = iota
	// Canonical: a block on the canonical chain.
	Canonical
	// Orphaned: a block that was replaced by a reorg.
	Orphaned
)

// SlotHeader is the minimal slot identity used in list views.
type SlotHeader struct {
	Slot     uint64     `db:"slot"`
	Proposer uint64     `db:"proposer"`
	Status   SlotStatus `db:"status"`
}

// Slot is a per-slot row in the lean chain. There is no epoch aggregation;
// finality is tracked via the justified/finalized flags and the checkpoints table.
type Slot struct {
	Slot             uint64     `db:"slot"`
	Proposer         uint64     `db:"proposer"`
	Status           SlotStatus `db:"status"`
	Root             []byte     `db:"root"`
	ParentRoot       []byte     `db:"parent_root"`
	StateRoot        []byte     `db:"state_root"`
	AttestationCount uint64     `db:"attestation_count"`
	Justified        bool       `db:"justified"`
	Finalized        bool       `db:"finalized"`
	BlockSize        uint64     `db:"block_size"`
	RecvDelay        int32      `db:"recv_delay"`
}

// CheckpointType distinguishes justified from finalized checkpoints.
type CheckpointType uint8

const (
	CheckpointJustified CheckpointType = iota
	CheckpointFinalized
)

// Checkpoint records a point in finality history (justified or finalized).
type Checkpoint struct {
	Slot uint64         `db:"slot"`
	Root []byte         `db:"root"`
	Type CheckpointType `db:"type"`
}

// Validator is a lean validator registry entry. Validators carry two XMSS
// public keys (attestation + proposal); both are opaque bytes here.
type Validator struct {
	Index             uint64 `db:"index"`
	AttestationPubkey []byte `db:"attestation_pubkey"`
	ProposalPubkey    []byte `db:"proposal_pubkey"`
}

// ValidatorDuty records the proposer assignment for a slot and whether it was
// fulfilled (a canonical block was seen).
type ValidatorDuty struct {
	Slot      uint64 `db:"slot"`
	Proposer  uint64 `db:"proposer"`
	Fulfilled bool   `db:"fulfilled"`
}

// Vote is a single validator attestation, used to compute participation.
// source/target are (slot, root) checkpoints; head is the attested head root.
type Vote struct {
	Slot           uint64 `db:"slot"`
	ValidatorIndex uint64 `db:"validator_index"`
	SourceSlot     uint64 `db:"source_slot"`
	SourceRoot     []byte `db:"source_root"`
	TargetSlot     uint64 `db:"target_slot"`
	TargetRoot     []byte `db:"target_root"`
	HeadRoot       []byte `db:"head_root"`
}

// UnfinalizedBlockStatus tracks the processing/canonical state of an
// unfinalized block held in the near-head cache.
type UnfinalizedBlockStatus uint32

const (
	// UnfinalizedBlockStatusNew: freshly received, not yet processed.
	UnfinalizedBlockStatusNew UnfinalizedBlockStatus = iota
	// UnfinalizedBlockStatusProcessed: processed and on the canonical chain.
	UnfinalizedBlockStatusProcessed
	// UnfinalizedBlockStatusOrphaned: replaced by a reorg.
	UnfinalizedBlockStatusOrphaned
)

// UnfinalizedBlock caches the SSZ/JSON blob of a block near the head so reorgs
// can be replayed without re-fetching from the node.
type UnfinalizedBlock struct {
	Root      []byte                 `db:"root"`
	Slot      uint64                 `db:"slot"`
	Status    UnfinalizedBlockStatus `db:"status"`
	BlockData []byte                 `db:"block_data"`
}
