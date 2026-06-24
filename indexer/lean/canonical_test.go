package lean

import (
	"testing"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// makeBits builds an SSZ bitlist that, when decoded by setBits, yields exactly
// the given validator indices. setBits treats the highest set bit as the
// length sentinel, so we set each validator bit plus a sentinel one position
// above the largest validator index.
func makeBits(validators []uint64) leanapi.HexBytes {
	maxIdx := uint64(0)
	for _, v := range validators {
		if v > maxIdx {
			maxIdx = v
		}
	}
	sentinel := maxIdx + 1
	nbytes := int(sentinel/8) + 1
	bits := make([]byte, nbytes)
	for _, v := range validators {
		bits[v/8] |= 1 << (v % 8)
	}
	bits[sentinel/8] |= 1 << (sentinel % 8)
	return leanapi.HexBytes(bits)
}

// voteBlock attaches a body with a single aggregated attestation: the given
// validators vote for headRoot at attSlot.
func voteBlock(b *Block, attSlot leanapi.Slot, headRoot leanapi.Root, validators []uint64) {
	body := b.GetBody()
	body.Body.Attestations = append(body.Body.Attestations, leanapi.AggregatedAttestation{
		AggregationBits: makeBits(validators),
		Data: leanapi.AttestationData{
			Slot: attSlot,
			Head: leanapi.Checkpoint{Root: headRoot, Slot: attSlot},
		},
	})
}

func TestMakeBitsRoundTrip(t *testing.T) {
	got := setBits(makeBits([]uint64{0, 2, 5}))
	want := []uint64{0, 2, 5}
	if len(got) != len(want) {
		t.Fatalf("setBits = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("setBits = %v, want %v", got, want)
		}
	}
}

func TestComputeCanonicalChainLinear(t *testing.T) {
	idx := newTestIndexer()

	a := addBlock(idx.blockCache, rootN(1), leanapi.Root{}, 1)
	b := addBlock(idx.blockCache, rootN(2), rootN(1), 2)
	c := addBlock(idx.blockCache, rootN(3), rootN(2), 3)

	// All validators vote for the tip.
	voteBlock(c, 3, c.Root, []uint64{0, 1, 2})

	head, canonical, changed := idx.computeCanonicalChain()
	if head != c.Root {
		t.Errorf("head = %v, want tip c (root 3)", head)
	}
	if !changed {
		t.Errorf("changed = false on first computation")
	}
	for _, r := range []leanapi.Root{a.Root, b.Root, c.Root} {
		if !canonical[r] {
			t.Errorf("root %v missing from canonical set", r)
		}
	}

	// Recompute without cache changes: short-circuits, head unchanged.
	head2, _, changed2 := idx.computeCanonicalChain()
	if head2 != c.Root || changed2 {
		t.Errorf("recompute = (%v, changed=%v), want (c, false)", head2, changed2)
	}
}

func TestComputeCanonicalChainVotesPickSibling(t *testing.T) {
	idx := newTestIndexer()

	addBlock(idx.blockCache, rootN(1), leanapi.Root{}, 1)
	left := addBlock(idx.blockCache, rootN(2), rootN(1), 2)
	right := addBlock(idx.blockCache, rootN(3), rootN(1), 2)

	// 2 validators favor left, 1 favors right -> left wins.
	voteBlock(left, 2, left.Root, []uint64{0, 1})
	voteBlock(right, 2, right.Root, []uint64{2})

	head, _, _ := idx.computeCanonicalChain()
	if head != left.Root {
		t.Errorf("head = %v, want left (root 2)", head)
	}
}

func TestComputeCanonicalChainReorgOnVoteShift(t *testing.T) {
	idx := newTestIndexer()

	parent := addBlock(idx.blockCache, rootN(1), leanapi.Root{}, 1)
	left := addBlock(idx.blockCache, rootN(2), rootN(1), 2)
	right := addBlock(idx.blockCache, rootN(3), rootN(1), 2)

	// First: left is favored.
	voteBlock(left, 2, left.Root, []uint64{0, 1})
	voteBlock(right, 2, right.Root, []uint64{2})

	head1, _, _ := idx.computeCanonicalChain()
	if head1 != left.Root {
		t.Fatalf("initial head = %v, want left", head1)
	}

	// Shift the latest votes to the right sibling (higher attestation slot wins
	// per validator), and add a follow-on block on right. GHOST descends into
	// the heaviest subtree (right's) all the way to its tip, so the new head is
	// the follow block, not right itself.
	follow := addBlock(idx.blockCache, rootN(4), right.Root, 3)
	voteBlock(follow, 3, right.Root, []uint64{0, 1, 2})

	head2, canonical2, changed2 := idx.computeCanonicalChain()
	if head2 != follow.Root {
		t.Errorf("new head = %v, want follow (root 4)", head2)
	}
	if !changed2 {
		t.Errorf("changed = false after head moved")
	}
	if !canonical2[right.Root] || !canonical2[follow.Root] || canonical2[left.Root] {
		t.Errorf("canonical set wrong: right=%v follow=%v left=%v",
			canonical2[right.Root], canonical2[follow.Root], canonical2[left.Root])
	}

	// Reorg from old head (left) to new head (follow): common ancestor is
	// parent, rewind 1 (drop left), forward 2 (parent -> right -> follow).
	reorg := idx.processReorg(left, follow)
	if reorg == nil {
		t.Fatalf("processReorg returned nil, want a reorg")
	}
	if reorg.Depth != 1 {
		t.Errorf("reorg depth = %d, want 1", reorg.Depth)
	}
	if reorg.ForwardDistance != 2 {
		t.Errorf("reorg forward distance = %d, want 2", reorg.ForwardDistance)
	}
	if reorg.CommonAncestor != parent.Root {
		t.Errorf("common ancestor = %v, want parent (root 1)", reorg.CommonAncestor)
	}
}

func TestProcessReorgFastForwardAndRewind(t *testing.T) {
	idx := newTestIndexer()

	a := addBlock(idx.blockCache, rootN(1), leanapi.Root{}, 1)
	b := addBlock(idx.blockCache, rootN(2), rootN(1), 2)
	c := addBlock(idx.blockCache, rootN(3), rootN(2), 3)

	// Pure fast-forward: new head c builds on old head a's chain -> nil.
	if r := idx.processReorg(a, c); r != nil {
		t.Errorf("fast-forward should return nil, got %v", r)
	}
	// Pure rewind: old head c, new head a (a is ancestor of c) -> nil.
	if r := idx.processReorg(c, a); r != nil {
		t.Errorf("rewind should return nil, got %v", r)
	}
	_ = b
}

func TestComputeCanonicalChainTiebreak(t *testing.T) {
	idx := newTestIndexer()

	addBlock(idx.blockCache, rootN(1), leanapi.Root{}, 1)
	// Two siblings at the same slot with equal weight; tiebreak by greater root.
	low := addBlock(idx.blockCache, rootN(2), rootN(1), 2)
	high := addBlock(idx.blockCache, rootN(9), rootN(1), 2)

	voteBlock(low, 2, low.Root, []uint64{0})
	voteBlock(high, 2, high.Root, []uint64{1})

	head, _, _ := idx.computeCanonicalChain()
	// Equal weight (1 each), equal slot -> lexicographically greater root (9 > 2).
	if head != high.Root {
		t.Errorf("tiebreak head = %v, want high (root 9)", head)
	}
}
