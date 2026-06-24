package lean

import (
	"testing"
	"time"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// rootN returns a deterministic non-zero root with byte 0 set to n.
func rootN(n byte) leanapi.Root {
	var r leanapi.Root
	r[0] = n
	return r
}

func TestBlockSetBlockDerivesHeaderFields(t *testing.T) {
	b := newBlock(rootN(10), 5)
	body := &leanapi.Block{
		Slot:          5,
		ProposerIndex: 7,
		ParentRoot:    rootN(9),
		StateRoot:     rootN(99),
		Body:          leanapi.BlockBody{Attestations: nil},
	}
	b.SetBlock(body)

	if got := b.GetParentRoot(); got != rootN(9) {
		t.Errorf("parent root = %v, want %v", got, rootN(9))
	}
	if got := b.GetStateRoot(); got != rootN(99) {
		t.Errorf("state root = %v, want %v", got, rootN(99))
	}
	if got := b.GetProposerIndex(); got != 7 {
		t.Errorf("proposer index = %d, want 7", got)
	}
	if b.GetBody() != body {
		t.Errorf("body not retained")
	}
}

func TestBlockSeenTracksEarliest(t *testing.T) {
	b := newBlock(rootN(1), 1)
	t1 := time.Unix(1000, 0)
	t0 := time.Unix(900, 0)

	b.SetSeen(t1, 500)
	if b.GetRecvDelay() != 500 {
		t.Fatalf("recvDelay = %d, want 500", b.GetRecvDelay())
	}

	// A later sighting with a larger delay must not overwrite the earliest.
	b.SetSeen(t1.Add(time.Second), 800)
	if b.GetRecvDelay() != 500 {
		t.Errorf("recvDelay = %d, want 500 (earliest kept)", b.GetRecvDelay())
	}
	if !b.GetSeenTime().Equal(t1) {
		t.Errorf("seenTime = %v, want %v", b.GetSeenTime(), t1)
	}

	// An earlier sighting with a smaller delay updates both.
	b.SetSeen(t0, 100)
	if b.GetRecvDelay() != 100 {
		t.Errorf("recvDelay = %d, want 100", b.GetRecvDelay())
	}
	if !b.GetSeenTime().Equal(t0) {
		t.Errorf("seenTime = %v, want %v", b.GetSeenTime(), t0)
	}
}

func TestBlockDispose(t *testing.T) {
	b := newBlock(rootN(1), 1)
	b.SetBlock(&leanapi.Block{Slot: 1})
	b.Dispose()

	if !b.isDisposed {
		t.Errorf("block not marked disposed")
	}
	if b.GetBody() != nil {
		t.Errorf("body returned after dispose")
	}
}
