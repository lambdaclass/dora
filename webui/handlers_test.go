package webui

import (
	"testing"

	"github.com/ethpandaops/dora/dbtypes"
)

// TestVotesToAttestations asserts each lean vote becomes a single-validator
// attestation row whose Validators field carries the vote's validator index.
// That populated Validators array is what makes the attestations tab's
// validatorsLoaded() return true client-side, so the duties AJAX (a non-route)
// never fires and the "Loading committee duties..." spinner does not hang.
func TestVotesToAttestations(t *testing.T) {
	s := &Server{}
	slot := &dbtypes.Slot{Slot: 7, Root: []byte{0xaa}}
	votes := []*dbtypes.Vote{
		{Slot: 7, ValidatorIndex: 3, SourceSlot: 5, TargetSlot: 7, HeadRoot: []byte{0xbb}},
		{Slot: 7, ValidatorIndex: 11, SourceSlot: 5, TargetSlot: 7, HeadRoot: []byte{0xcc}},
	}

	atts := s.votesToAttestations(slot, votes)
	if len(atts) != len(votes) {
		t.Fatalf("got %d attestations, want %d", len(atts), len(votes))
	}

	for i, att := range atts {
		v := votes[i]
		if len(att.Validators) != 1 || att.Validators[0] != v.ValidatorIndex {
			t.Errorf("row %d: Validators = %v, want [%d]", i, att.Validators, v.ValidatorIndex)
		}
		if len(att.AggregationBits) != 1 || att.AggregationBits[0] != 0x01 {
			t.Errorf("row %d: AggregationBits = %v, want [0x01]", i, att.AggregationBits)
		}
		if len(att.CommitteeIndex) != 1 || att.CommitteeIndex[0] != 0 {
			t.Errorf("row %d: CommitteeIndex = %v, want [0]", i, att.CommitteeIndex)
		}
		if att.BeaconBlockSlot != slot.Slot {
			t.Errorf("row %d: BeaconBlockSlot = %d, want %d", i, att.BeaconBlockSlot, slot.Slot)
		}
	}
}
