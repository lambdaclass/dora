package webui

import "time"

// EpochsPageData mirrors Dora's epochs-list model
// (types/models.EpochsPageData), reduced to a lean-local struct. Field
// names/types match templates/epochs/epochs.html so the real template executes
// unchanged.
//
// Lean has NO epoch concept (one slot per "epoch"). The page is rendered with
// Dora's real chrome but an empty body: Epochs is nil and EpochCount is 0, which
// makes the template take its empty-state branch (the professor SVG). The handler
// also surfaces a muted "epochs do not apply" note above the table.
type EpochsPageData struct {
	Epochs     []*EpochsPageDataEpoch `json:"epochs"`
	EpochCount uint64
	FirstEpoch uint64
	LastEpoch  uint64

	IsDefaultPage    bool   `json:"default_page"`
	TotalPages       uint64 `json:"total_pages"`
	PageSize         uint64 `json:"page_size"`
	CurrentPageIndex uint64 `json:"page_index"`
	CurrentPageEpoch uint64 `json:"page_epoch"`
	PrevPageIndex    uint64 `json:"prev_page_index"`
	PrevPageEpoch    uint64 `json:"prev_page_epoch"`
	NextPageIndex    uint64 `json:"next_page_index"`
	NextPageEpoch    uint64 `json:"next_page_epoch"`
	LastPageEpoch    uint64 `json:"last_page_epoch"`

	UrlParams []urlParam `json:"url_params"`
	MaxEpoch  uint64     `json:"max_epoch"`
}

// EpochsPageDataEpoch is one epochs-list row. Unused in lean (the list is always
// empty), but defined so the template's range body type-checks.
type EpochsPageDataEpoch struct {
	Epoch                   uint64    `json:"epoch"`
	Ts                      time.Time `json:"ts"`
	Finalized               bool      `json:"finalized"`
	Justified               bool      `json:"justified"`
	Synchronized            bool      `json:"synchronized"`
	CanonicalBlockCount     uint64    `json:"canonical_block_count"`
	AttestationCount        uint64    `json:"attestation_count"`
	DepositCount            uint64    `json:"deposit_count"`
	ExitCount               uint64    `json:"exit_count"`
	ProposerSlashingCount   uint64    `json:"proposer_slashing_count"`
	AttesterSlashingCount   uint64    `json:"attester_slashing_count"`
	EligibleEther           uint64    `json:"eligibleether"`
	TargetVoted             uint64    `json:"target_voted"`
	HeadVoted               uint64    `json:"head_voted"`
	TotalVoted              uint64    `json:"total_voted"`
	TargetVoteParticipation float64   `json:"target_vote_participation"`
	HeadVoteParticipation   float64   `json:"head_vote_participation"`
	TotalVoteParticipation  float64   `json:"total_vote_participation"`
	SlotsPerEpoch           uint64    `json:"slots_per_epoch"`
	ProposalParticipation   float64   `json:"proposal_participation"`
	EthTransactionCount     uint64    `json:"eth_transaction_count"`
	BlobCount               uint64    `json:"blob_count"`
}

// EpochPageData mirrors Dora's epoch-detail model
// (types/models.EpochPageData), reduced to a lean-local struct. Field
// names/types match templates/epoch/epoch.html so the real template executes
// unchanged.
//
// Lean has no epochs, so the detail page is rendered with Dora's real chrome but
// a zeroed body: every count/participation field reads 0/—. The handler also
// surfaces a muted "epochs do not apply" note.
type EpochPageData struct {
	Epoch                   uint64               `json:"epoch"`
	PreviousEpoch           uint64               `json:"prev_epoch"`
	NextEpoch               uint64               `json:"next_epoch"`
	Ts                      time.Time            `json:"ts"`
	Synchronized            bool                 `json:"synchronized"`
	Finalized               bool                 `json:"finalized"`
	AttestationCount        uint64               `json:"attestation_count"`
	DepositCount            uint64               `json:"deposit_count"`
	ExitCount               uint64               `json:"exit_count"`
	WithdrawalCount         uint64               `json:"withdrawal_count"`
	WithdrawalAmount        uint64               `json:"withdrawal_amount"`
	ProposerSlashingCount   uint64               `json:"proposer_slashing_count"`
	AttesterSlashingCount   uint64               `json:"attester_slashing_count"`
	EligibleEther           uint64               `json:"eligibleether"`
	TargetVoted             uint64               `json:"target_voted"`
	HeadVoted               uint64               `json:"head_voted"`
	TotalVoted              uint64               `json:"total_voted"`
	TargetVoteParticipation float64              `json:"target_vote_participation"`
	HeadVoteParticipation   float64              `json:"head_vote_participation"`
	TotalVoteParticipation  float64              `json:"total_vote_participation"`
	SyncParticipation       float64              `json:"sync_participation"`
	ValidatorCount          uint64               `json:"validator_count"`
	AverageValidatorBalance uint64               `json:"avg_validator_balance"`
	BlockCount              uint64               `json:"block_count"`
	CanonicalCount          uint64               `json:"canonical_count"`
	MissedCount             uint64               `json:"missed_count"`
	ScheduledCount          uint64               `json:"scheduled_count"`
	OrphanedCount           uint64               `json:"orphaned_count"`
	EthTransactionCount     uint64               `json:"eth_transaction_count"`
	BlobCount               uint64               `json:"blob_count"`
	Slots                   []*EpochPageDataSlot `json:"slots"`
}

// EpochPageDataSlot is one slot row in the epoch-detail table. Unused in lean
// (Slots is always nil), defined so the template's range body type-checks.
type EpochPageDataSlot struct {
	Slot                  uint64    `json:"slot"`
	Epoch                 uint64    `json:"epoch"`
	Ts                    time.Time `json:"ts"`
	Scheduled             bool      `json:"scheduled"`
	Status                uint8     `json:"status"`
	PayloadStatus         uint8     `json:"payload_status"`
	Proposer              uint64    `json:"proposer"`
	ProposerName          string    `json:"proposer_name"`
	AttestationCount      uint64    `json:"attestation_count"`
	DepositCount          uint64    `json:"deposit_count"`
	ExitCount             uint64    `json:"exit_count"`
	ProposerSlashingCount uint64    `json:"proposer_slashing_count"`
	AttesterSlashingCount uint64    `json:"attester_slashing_count"`
	SyncParticipation     float64   `json:"sync_participation"`
	EthTransactionCount   uint64    `json:"eth_transaction_count"`
	BlobCount             uint64    `json:"blob_count"`
	EthBlockNumber        uint64    `json:"eth_block_number"`
	WithEthBlock          bool      `json:"with_eth_block"`
	Graffiti              []byte    `json:"graffiti"`
	BlockRoot             []byte    `json:"block_root"`
}
