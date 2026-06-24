package lean

import (
	"testing"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// importBlock mirrors the eventual ingestion sequence: create the cache node,
// attach its body, link it under its parent, then run fork detection.
func importBlock(t *testing.T, idx *Indexer, root, parent leanapi.Root, slot leanapi.Slot) *Block {
	t.Helper()
	b := addBlock(idx.blockCache, root, parent, slot)
	if err := idx.forkCache.processBlock(b); err != nil {
		t.Fatalf("processBlock(slot %d) failed: %v", slot, err)
	}
	return b
}

// TestForkDetectionLinearChain: a linear chain produces no extra forks; every
// block stays on the single canonical fork id.
func TestForkDetectionLinearChain(t *testing.T) {
	idx := newTestIndexer()

	a := importBlock(t, idx, rootN(1), leanapi.Root{}, 1)
	b := importBlock(t, idx, rootN(2), rootN(1), 2)
	c := importBlock(t, idx, rootN(3), rootN(2), 3)

	if a.forkId != b.forkId || b.forkId != c.forkId {
		t.Errorf("linear chain split fork ids: a=%d b=%d c=%d", a.forkId, b.forkId, c.forkId)
	}
	// A linear chain never spawns a Fork object: every block stays on the
	// inherited parent fork id and forkMap remains empty until a real fork or
	// genesis/finalized seeding (a later task) creates one.
	if len(idx.forkCache.forkMap) != 0 {
		t.Errorf("forkMap len = %d, want 0 for a linear chain", len(idx.forkCache.forkMap))
	}
}

// TestForkDetectionSibling: a second block at the same parent spawns a second
// fork. With one prior child (scenario 1, non-finalized parent) BOTH children
// become their own forks with the correct base = parent.
func TestForkDetectionSibling(t *testing.T) {
	idx := newTestIndexer()

	// a(1) -> b(2). Then c(2) is a sibling of b building on a.
	importBlock(t, idx, rootN(1), leanapi.Root{}, 1)
	b := importBlock(t, idx, rootN(2), rootN(1), 2)
	c := importBlock(t, idx, rootN(3), rootN(1), 2)

	// c introduced a new fork.
	cFork := idx.forkCache.getForkByLeaf(c.Root)
	if cFork == nil {
		t.Fatalf("no fork created for sibling c")
	}
	baseSlot, baseRoot := cFork.GetBase()
	if baseRoot != rootN(1) || baseSlot != 1 {
		t.Errorf("c fork base = (%d,%v), want (1, root 1)", baseSlot, baseRoot)
	}
	if cFork.leafRoot != c.Root || cFork.leafSlot != 2 {
		t.Errorf("c fork leaf = (%d,%v), want (2, root 3)", cFork.leafSlot, cFork.leafRoot)
	}

	// The first child b also got its own fork (scenario 1, single prior child).
	bFork := idx.forkCache.getForkByLeaf(b.Root)
	if bFork == nil {
		t.Fatalf("no fork created for original child b")
	}
	if _, br := bFork.GetBase(); br != rootN(1) {
		t.Errorf("b fork base root = %v, want root 1", br)
	}

	if b.forkId == c.forkId {
		t.Errorf("siblings b and c share fork id %d, want distinct", b.forkId)
	}

	// The split produced two Fork objects (one per sibling branch).
	if len(idx.forkCache.forkMap) != 2 {
		t.Errorf("forkMap len = %d, want 2 after sibling fork", len(idx.forkCache.forkMap))
	}
}

// TestForkDetectionDescendantRewalk: scenario 2. When a parent gains a second
// child after its first child already has descendants, the extra children
// spawn forks and the descendant walk reassigns fork ids down the chain.
func TestForkDetectionDescendantRewalk(t *testing.T) {
	idx := newTestIndexer()

	// Build a(1) -> b(2) -> d(3) first (linear).
	importBlock(t, idx, rootN(1), leanapi.Root{}, 1)
	b := importBlock(t, idx, rootN(2), rootN(1), 2)
	d := importBlock(t, idx, rootN(4), rootN(2), 3)

	// b and d share one fork at this point.
	if b.forkId != d.forkId {
		t.Fatalf("precondition: b and d should share fork id, got %d and %d", b.forkId, d.forkId)
	}

	// Now add sibling c(2) off a -> triggers scenario 1, both b and c get forks,
	// and b's fork id propagates down to d via updateForkBlocks.
	c := importBlock(t, idx, rootN(3), rootN(1), 2)

	if c.forkId == b.forkId {
		t.Errorf("c should be on a different fork than b")
	}
	if d.forkId != b.forkId {
		t.Errorf("descendant d fork id = %d, want b's fork id %d", d.forkId, b.forkId)
	}
}
