package lean

import "time"

// ChainSpec holds the lean-consensus timing parameters, derived from
// GET /lean/v0/config/spec plus GET /lean/v0/genesis. Lean has no epoch
// concept; slot timing is the only thing the explorer needs for wall-clock
// conversions.
type ChainSpec struct {
	GenesisTime             uint64
	MillisecondsPerSlot     uint64
	IntervalsPerSlot        uint64
	MillisecondsPerInterval uint64
	HistoricalRootsLimit    uint64
	ForkDigest              string
	ValidatorCount          uint64
}

// NewChainSpec builds a ChainSpec from the genesis and spec endpoint responses.
func NewChainSpec(genesis *Genesis, spec *Spec) *ChainSpec {
	return &ChainSpec{
		GenesisTime:             genesis.GenesisTime,
		ValidatorCount:          genesis.ValidatorCount,
		MillisecondsPerSlot:     spec.MillisecondsPerSlot,
		IntervalsPerSlot:        spec.IntervalsPerSlot,
		MillisecondsPerInterval: spec.MillisecondsPerInterval,
		HistoricalRootsLimit:    spec.HistoricalRootsLimit,
		ForkDigest:              spec.ForkDigest,
	}
}

// SlotDuration returns the wall-clock duration of one slot.
func (s *ChainSpec) SlotDuration() time.Duration {
	return time.Duration(s.MillisecondsPerSlot) * time.Millisecond
}

// GenesisTimestamp returns the genesis time as a time.Time.
func (s *ChainSpec) GenesisTimestamp() time.Time {
	return time.Unix(int64(s.GenesisTime), 0)
}

// SlotToTime returns the wall-clock start time of the given slot.
func (s *ChainSpec) SlotToTime(slot uint64) time.Time {
	return s.GenesisTimestamp().Add(time.Duration(slot) * s.SlotDuration())
}

// TimeToSlot returns the slot active at the given wall-clock time. Times before
// genesis return 0.
func (s *ChainSpec) TimeToSlot(t time.Time) uint64 {
	if s.MillisecondsPerSlot == 0 {
		return 0
	}
	genesis := s.GenesisTimestamp()
	if t.Before(genesis) {
		return 0
	}
	elapsed := t.Sub(genesis)
	return uint64(elapsed.Milliseconds()) / s.MillisecondsPerSlot
}

// CurrentSlot returns the slot active right now.
func (s *ChainSpec) CurrentSlot() uint64 {
	return s.TimeToSlot(time.Now())
}
