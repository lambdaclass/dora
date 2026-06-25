package webui

import "time"

// IndexPageData mirrors Dora's homepage model (types/models.IndexPageData),
// reduced to a lean-local struct. Field names/types match exactly what Dora's
// templates/index/*.html templates read so the real templates execute against
// it unchanged. Eth-only economics fields (balances, churn, deposits) are kept
// for template compatibility but always populated with zero/empty values in the
// lean explorer; the templates render them as 0 / "—" / greyed.
type IndexPageData struct {
	NetworkName             string    `json:"netname"`
	DepositContract         string    `json:"depaddr"`
	ShowSyncingMessage      bool      `json:"show_sync"`
	SlotsPerEpoch           uint64    `json:"slots_per_epoch"`
	SlotDurationMs          uint64    `json:"slot_duration_ms"`
	EpochDurationMs         uint64    `json:"epoch_duration_ms"`
	CurrentEpoch            uint64    `json:"cur_epoch"`
	CurrentFinalizedEpoch   int64     `json:"finalized_epoch"`
	CurrentJustifiedEpoch   int64     `json:"justified_epoch"`
	CurrentSlot             uint64    `json:"cur_slot"`
	CurrentScheduledCount   uint64    `json:"cur_scheduled"`
	CurrentEpochProgress    float64   `json:"cur_epoch_prog"`
	ActiveValidatorCount    uint64    `json:"active_val"`
	EnteringValidatorCount  uint64    `json:"entering_val"`
	EnteringEtherAmount     uint64    `json:"entering_ether"`
	ExitingValidatorCount   uint64    `json:"exiting_val"`
	ValidatorsPerEpoch      uint64    `json:"churn_epoch"`
	EtherChurnPerEpoch      uint64    `json:"churn_ether"`
	ValidatorsPerDay        uint64    `json:"churn_day"`
	EtherChurnPerDay        uint64    `json:"churn_ether_day"`
	TotalEligibleEther      uint64    `json:"eligible"`
	AverageValidatorBalance uint64    `json:"avg_balance"`
	NewDepositProcessAfter  string    `json:"queue_delay"`
	GenesisTime             time.Time `json:"genesis_time"`
	GenesisForkVersion      []byte    `json:"genesis_version"`
	GenesisValidatorsRoot   []byte    `json:"genesis_valroot"`

	NetworkForks     []*IndexPageDataForks  `json:"forks"`
	RecentBlocks     []*IndexPageDataBlocks `json:"blocks"`
	RecentBlockCount uint64                 `json:"block_count"`
	RecentEpochs     []*IndexPageDataEpochs `json:"epochs"`
	RecentEpochCount uint64                 `json:"epoch_count"`
	RecentSlots      []*IndexPageDataSlots  `json:"slots"`
	RecentSlotCount  uint64                 `json:"slot_count"`
	ForkTreeWidth    int32                  `json:"forktree_width"`
}

// IndexPageDataForks describes a single network fork badge. In lean there is one
// active fork derived from the chain spec's fork digest.
type IndexPageDataForks struct {
	Name             string  `json:"name"`
	Epoch            uint64  `json:"epoch"`
	Version          []byte  `json:"version"`
	Active           bool    `json:"active"`
	Time             uint64  `json:"time"`
	Type             string  `json:"type"`
	MaxBlobsPerBlock *uint64 `json:"max_blobs_per_block,omitempty"`
	ForkDigest       []byte  `json:"fork_digest"`
}

// IndexPageDataEpochs is unused in lean (no epochs); the recent-epochs panel is
// rendered with an empty slice. Kept for template field-name compatibility.
type IndexPageDataEpochs struct {
	Epoch                 uint64    `json:"epoch"`
	Ts                    time.Time `json:"ts"`
	Finalized             bool      `json:"finalized"`
	Justified             bool      `json:"justified"`
	EligibleEther         uint64    `json:"eligible"`
	TargetVoted           uint64    `json:"voted"`
	VoteParticipation     float64   `json:"votep"`
	BlockCount            uint64    `json:"blocks"`
	SlotsPerEpoch         uint64    `json:"slots_per_epoch"`
	ProposalParticipation float64   `json:"proposalp"`
}

// IndexPageDataBlocks describes a recent block row. WithEthBlock is always false
// in lean (no execution layer) so the eth-block column renders "-".
type IndexPageDataBlocks struct {
	Epoch         uint64    `json:"epoch"`
	Slot          uint64    `json:"slot"`
	WithEthBlock  bool      `json:"has_block"`
	EthBlock      uint64    `json:"eth_block"`
	EthBlockLink  string    `json:"eth_link"`
	Ts            time.Time `json:"ts"`
	Proposer      uint64    `json:"proposer"`
	ProposerName  string    `json:"proposer_name"`
	Status        uint64    `json:"status"`
	PayloadStatus uint8     `json:"payload_status"`
	BlockRoot     []byte    `json:"block_root"`
}

// IndexPageDataSlots describes a recent slot row, including its fork-graph tiles.
// In lean the fork graph is a single straight column (no competing forks shown).
type IndexPageDataSlots struct {
	Epoch         uint64                    `json:"epoch"`
	Slot          uint64                    `json:"slot"`
	EthBlock      uint64                    `json:"eth_block"`
	Ts            time.Time                 `json:"ts"`
	Proposer      uint64                    `json:"proposer"`
	ProposerName  string                    `json:"proposer_name"`
	Status        uint64                    `json:"status"`
	PayloadStatus uint8                     `json:"payload_status"`
	BlockRoot     []byte                    `json:"block_root"`
	ParentRoot    []byte                    `json:"parent_root"`
	ForkGraph     []*IndexPageDataForkGraph `json:"fork_graph"`
}

// IndexPageDataForkGraph is one drawn fork lane for a slot row.
type IndexPageDataForkGraph struct {
	Index int32    `json:"index"`
	Left  int32    `json:"left"`
	Tiles []string `json:"tiles"`
	Block bool     `json:"block"`
}
