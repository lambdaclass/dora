package webui

import "time"

// SlotPageData mirrors Dora's slot-detail model (types/models.SlotPageData),
// reduced to a lean-local struct. Field names/types match what Dora's
// templates/slot/{slot,overview,attestations}.html read.
//
// Lean reality folds onto the eth model as follows:
//   - Epoch == Slot (the overview "Epoch" row shows the slot number).
//   - The eth-only sub-tabs (transactions, blobs, withdrawals, deposits,
//     builder/PTC/inclusion-list/proof/access-list, slashings, voluntary exits)
//     are driven by *Count fields; every one is left at 0, so neither the tab
//     nor its sub-template is rendered. The only tabs shown are Overview and
//     Attestations.
//   - No execution layer: PayloadHeader and ExecutionData stay nil, so the
//     execution-payload / payload-header overview sections are skipped.
//   - One vote per validator: each lean Vote becomes one Attestation row with a
//     single-bit aggregation (committee index 0), so the attestations tab lists
//     the slot's votes.
type SlotPageData struct {
	Slot           uint64                `json:"slot"`
	Epoch          uint64                `json:"epoch"`
	EpochFinalized bool                  `json:"epoch_finalized"`
	Ts             time.Time             `json:"time"`
	NextSlot       uint64                `json:"next_slot"`
	PreviousSlot   uint64                `json:"prev_slot"`
	Status         uint16                `json:"status"`
	Future         bool                  `json:"future"`
	Proposer       uint64                `json:"proposer"`
	ProposerName   string                `json:"proposer_name"`
	Block          *SlotPageBlockData    `json:"block"`
	Badges         []*SlotPageBlockBadge `json:"badges"`
	SlotBlocks     []*SlotPageSlotBlock  `json:"slot_blocks"`
	TracoorUrl     string                `json:"tracoor_url"`
}

// SlotPageSlotBlock represents a block entry for the slot (multi-block display).
// Lean shows a single canonical block, so this slice has at most one entry and
// the multi-block banner (gt len 1) never renders.
type SlotPageSlotBlock struct {
	BlockRoot []byte `json:"block_root"`
	Status    uint16 `json:"status"`
	IsCurrent bool   `json:"is_current"`
}

// SlotPageBlockBadge is a status pill rendered next to the slot title.
type SlotPageBlockBadge struct {
	Title       string `json:"title"`
	Icon        string `json:"icon"`
	Description string `json:"descr"`
	ClassName   string `json:"class"`
}

// SlotPageBlockData mirrors Dora's block model. The eth-only count fields and
// payload/execution sub-structs are present for template compatibility but stay
// zero/nil in lean, so the eth-only tabs and overview sections are not rendered.
type SlotPageBlockData struct {
	BlockRoot              []byte                  `json:"blockroot"`
	ParentRoot             []byte                  `json:"parentroot"`
	StateRoot              []byte                  `json:"stateroot"`
	BodyRoot               []byte                  `json:"bodyroot"`
	Signature              []byte                  `json:"signature"`
	RandaoReveal           []byte                  `json:"randaoreveal"`
	Graffiti               []byte                  `json:"graffiti"`
	Eth1dataDepositroot    []byte                  `json:"eth1data_depositroot"`
	Eth1dataDepositcount   uint64                  `json:"eth1data_depositcount"`
	Eth1dataBlockhash      []byte                  `json:"eth1data_blockhash"`
	SyncAggregateBits      []byte                  `json:"syncaggregate_bits"`
	SyncAggregateSignature []byte                  `json:"syncaggregate_signature"`
	SyncAggParticipation   float64                 `json:"syncaggregate_participation"`
	ValidatorNames         []SlotPageValidatorName `json:"validator_names"`

	ProposerSlashingsCount      uint64 `json:"proposer_slashings_count"`
	AttesterSlashingsCount      uint64 `json:"attester_slashings_count"`
	AttestationsCount           uint64 `json:"attestations_count"`
	DepositsCount               uint64 `json:"deposits_count"`
	WithdrawalsCount            uint64 `json:"withdrawals_count"`
	BLSChangesCount             uint64 `json:"bls_changes_count"`
	VoluntaryExitsCount         uint64 `json:"voluntaryexits_count"`
	SlashingsCount              uint64 `json:"slashings_count"`
	BlobsCount                  uint64 `json:"blobs_count"`
	ExecutionProofsCount        uint64 `json:"execution_proofs_count"`
	TransactionsCount           uint64 `json:"transactions_count"`
	DepositRequestsCount        uint64 `json:"deposit_receipts_count"`
	WithdrawalRequestsCount     uint64 `json:"withdrawal_requests_count"`
	ConsolidationRequestsCount  uint64 `json:"consolidation_requests_count"`
	BuilderDepositRequestsCount uint64 `json:"builder_deposit_requests_count"`
	BuilderExitRequestsCount    uint64 `json:"builder_exit_requests_count"`
	RequestsFromParentPayload   bool   `json:"requests_from_parent_payload"`
	BidsCount                   uint64 `json:"bids_count"`
	PtcVotesCount               uint64 `json:"ptc_votes_count"`
	InclusionListsCount         uint64 `json:"inclusion_lists_count"`

	SlotsPerEpoch        uint64 `json:"slots_per_epoch"`
	TargetCommitteeSize  uint64 `json:"target_committee_size"`
	MaxCommitteesPerSlot uint64 `json:"max_committees_per_slot"`

	PayloadHeader          *SlotPagePayloadHeader `json:"payload_header"`
	ExecutionData          *SlotPageExecutionData `json:"execution_data"`
	PayloadDataUnavailable bool                   `json:"payload_data_unavailable"`

	Attestations []*SlotPageAttestation `json:"attestations"`
}

// SlotPageValidatorName maps a validator index to its display name.
type SlotPageValidatorName struct {
	Key   uint64 `json:"k"`
	Value string `json:"v"`
}

// SlotPagePayloadHeader / SlotPageExecutionData exist only so the overview
// template's `with .Block.PayloadHeader` / `with .Block.ExecutionData` blocks
// type-check; both are always nil in lean (no execution layer).
type SlotPagePayloadHeader struct {
	PayloadStatus uint16 `json:"payload_status"`
}

type SlotPageExecutionData struct {
	BlockNumber uint64    `json:"block_number"`
	Time        time.Time `json:"time"`
}

// SlotPageAttestation is one attestation row in the attestations tab. In lean
// each row is a single validator's vote: AggregationBits has one bit set,
// CommitteeIndex is [0], and the source/target checkpoints come straight from
// the Vote. The JS in attestations.html reads these (base64-encoded by
// includeJSON) to render the bitfield and links.
type SlotPageAttestation struct {
	Slot           uint64   `json:"slot"`
	CommitteeIndex []uint64 `json:"committeeindex"`
	TotalActive    uint64   `json:"total_active"`

	AggregationBits    []byte   `json:"aggregationbits"`
	Validators         []uint64 `json:"validators"`
	IncludedValidators []uint64 `json:"included_validators"`

	PayloadStatus *uint64 `json:"payload_status,omitempty"`

	Signature []byte `json:"signature"`

	BeaconBlockRoot []byte `json:"beaconblockroot"`
	BeaconBlockSlot uint64 `json:"beaconblockslot"`
	SourceEpoch     uint64 `json:"source_epoch"`
	SourceRoot      []byte `json:"source_root"`
	TargetEpoch     uint64 `json:"target_epoch"`
	TargetRoot      []byte `json:"target_root"`
}
