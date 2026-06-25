package webui

import "time"

// ForksPageData mirrors Dora's forks model (types/models.ForksPageData),
// reduced to a lean-local struct. Field names/types match
// templates/forks/forks.html so the real template executes unchanged.
//
// In lean the fork list is derived from the node's fork-choice tree
// (GET /lean/v0/fork_choice): each leaf (a node that is no node's parent) is a
// fork head. The canonical fork (index 0) is the one whose head matches the
// fork-choice head root. Each fork carries a single client row: the connected
// ethlambda node, since lean explores exactly one node.
type ForksPageData struct {
	Forks     []*ForksPageDataFork `json:"forks"`
	ForkCount uint64               `json:"fork_count"`
}

// ForksPageDataFork is one fork (one head) with the clients that follow it.
type ForksPageDataFork struct {
	HeadSlot    uint64                 `json:"head_slot"`
	HeadRoot    []byte                 `json:"head_root"`
	Clients     []*ForksPageDataClient `json:"clients"`
	ClientCount uint64                 `json:"client_count"`
}

// ForksPageDataClient is one client row under a fork. In lean this is always the
// connected node. Status maps to Dora's "online" so it renders "Connected";
// Distance is the slot gap between the fork head and the client head (0 for the
// canonical fork the node follows).
type ForksPageDataClient struct {
	Index       uint64    `json:"index"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Status      string    `json:"status"`
	HeadSlot    uint64    `json:"head_slot"`
	Distance    uint64    `json:"distance"`
	LastRefresh time.Time `json:"refresh"`
	LastError   string    `json:"error"`
}

// ChainForksPageData mirrors Dora's chain-forks model
// (types/models.ChainForksPageData). templates/chain_forks/chain_forks.html only
// reads .ChainSpecs (the rest of Dora's chain-forks machinery is driven by an
// AJAX data endpoint that lean does not serve, so the diagram area stays empty).
type ChainForksPageData struct {
	ChainSpecs *ChainSpecs `json:"chain_specs"`
}

// ChainSpecs carries the timing values the chain-forks page template reads. In
// lean SlotsPerEpoch is 1 and the epoch-window selectors are derived from the
// slot duration.
type ChainSpecs struct {
	SlotsPerEpoch  uint64 `json:"slots_per_epoch"`
	SlotDurationMs uint64 `json:"slot_duration_ms"`
	GenesisTime    uint64 `json:"genesis_time"`
	CurrentSlot    uint64 `json:"current_slot"`
	EpochsFor12h   uint64 `json:"epochs_for_12h"`
	EpochsFor1d    uint64 `json:"epochs_for_1d"`
	EpochsFor7d    uint64 `json:"epochs_for_7d"`
	EpochsFor14d   uint64 `json:"epochs_for_14d"`
}
