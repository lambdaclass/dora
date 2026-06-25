package webui

import (
	"testing"

	"github.com/ethpandaops/dora/dbtypes"
)

// TestVotesToAttestations asserts lean per-validator votes are aggregated by
// attestation data into cards over the full validator committee. Votes sharing
// the same data (head/source/target) collapse into one card with a bit set per
// voting validator, so the attestations tab reads "K of N attesting" rather than
// a misleading per-vote "1 of 1". A non-empty Validators committee also keeps the
// client's validatorsLoaded() true so the duties AJAX (a non-route) never fires
// and the "Loading committee duties..." spinner cannot hang.
func TestVotesToAttestations(t *testing.T) {
	s := &Server{}
	slot := &dbtypes.Slot{Slot: 7, Root: []byte{0xaa}}
	// Validators 0 and 2 vote the same data (head 0xbb); validator 1 votes a
	// different head (0xcc) — e.g. a fork. Committee size 3.
	votes := []*dbtypes.Vote{
		{Slot: 7, ValidatorIndex: 0, SourceSlot: 5, TargetSlot: 7, HeadRoot: []byte{0xbb}},
		{Slot: 7, ValidatorIndex: 2, SourceSlot: 5, TargetSlot: 7, HeadRoot: []byte{0xbb}},
		{Slot: 7, ValidatorIndex: 1, SourceSlot: 5, TargetSlot: 7, HeadRoot: []byte{0xcc}},
	}

	atts := s.votesToAttestations(slot, votes, 3)

	// Two distinct attestation data → two aggregated cards (not one per vote).
	if len(atts) != 2 {
		t.Fatalf("got %d attestations, want 2 (grouped by data)", len(atts))
	}

	for i, att := range atts {
		if len(att.Validators) != 3 {
			t.Errorf("att %d: committee size = %d, want 3 (full validator set)", i, len(att.Validators))
		}
		if att.TotalActive != 3 {
			t.Errorf("att %d: TotalActive = %d, want 3", i, att.TotalActive)
		}
		if att.BeaconBlockSlot != slot.Slot {
			t.Errorf("att %d: BeaconBlockSlot = %d, want %d", i, att.BeaconBlockSlot, slot.Slot)
		}
		if len(att.CommitteeIndex) != 1 || att.CommitteeIndex[0] != 0 {
			t.Errorf("att %d: CommitteeIndex = %v, want [0]", i, att.CommitteeIndex)
		}
	}

	// First card (head 0xbb, first seen) aggregates validators 0 and 2 → bits 0,2 → 0x05.
	if got := att0Bits(atts); got != 0x05 {
		t.Errorf("aggregated card bits = %#x, want 0x05 (validators 0 and 2)", got)
	}
	// Second card (head 0xcc) has validator 1 → bit 1 → 0x02.
	if got := atts[1].AggregationBits[0]; got != 0x02 {
		t.Errorf("second card bits = %#x, want 0x02 (validator 1)", got)
	}
}

func att0Bits(atts []*SlotPageAttestation) byte {
	if len(atts) == 0 || len(atts[0].AggregationBits) == 0 {
		return 0
	}
	return atts[0].AggregationBits[0]
}

// TestVotesToAttestationsEmpty: no votes → no cards, and a slot with one vote
// still yields a full-committee card so the bit-per-validator view holds.
func TestVotesToAttestationsEmpty(t *testing.T) {
	s := &Server{}
	slot := &dbtypes.Slot{Slot: 1}
	if got := s.votesToAttestations(slot, nil, 3); len(got) != 0 {
		t.Errorf("no votes: got %d cards, want 0", len(got))
	}
	one := []*dbtypes.Vote{{Slot: 1, ValidatorIndex: 2, HeadRoot: []byte{0x01}}}
	atts := s.votesToAttestations(slot, one, 3)
	if len(atts) != 1 || len(atts[0].Validators) != 3 || atts[0].AggregationBits[0] != 0x04 {
		t.Errorf("single vote: got cards=%d committee=%d bits=%#x, want 1/3/0x04", len(atts), len(atts[0].Validators), atts[0].AggregationBits[0])
	}
}
