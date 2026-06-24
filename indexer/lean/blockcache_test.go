package lean

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// newTestIndexer builds an Indexer with both caches wired and a quiet logger,
// suitable for cache/fork unit tests. No client or DB is attached.
func newTestIndexer() *Indexer {
	logger := logrus.New()
	logger.SetOutput(logTestWriter{})
	idx := &Indexer{
		ctx:    context.Background(),
		logger: logger,
	}
	idx.blockCache = newBlockCache(idx)
	idx.forkCache = newForkCache(idx)
	return idx
}

// logTestWriter discards log output during tests.
type logTestWriter struct{}

func (logTestWriter) Write(p []byte) (int, error) { return len(p), nil }

// addBlock creates a cache block, attaches a minimal body (so parent/state
// roots are populated), links it under its parent, and returns it.
func addBlock(cache *blockCache, root, parent leanapi.Root, slot leanapi.Slot) *Block {
	b, _ := cache.createOrGetBlock(root, slot)
	b.SetBlock(&leanapi.Block{
		Slot:       slot,
		ParentRoot: parent,
		StateRoot:  root, // arbitrary but unique
	})
	cache.addBlockToParentMap(b)
	return b
}

func TestBlockCacheCreateOrGet(t *testing.T) {
	cache := newBlockCache(newTestIndexer())

	b1, created := cache.createOrGetBlock(rootN(1), 1)
	if !created {
		t.Fatalf("first createOrGetBlock should report created")
	}
	b2, created := cache.createOrGetBlock(rootN(1), 1)
	if created {
		t.Errorf("second createOrGetBlock should not report created")
	}
	if b1 != b2 {
		t.Errorf("createOrGetBlock returned different instances for same root")
	}
}

func TestBlockCacheLookups(t *testing.T) {
	cache := newBlockCache(newTestIndexer())

	a := addBlock(cache, rootN(1), leanapi.Root{}, 1)
	b := addBlock(cache, rootN(2), rootN(1), 2)
	c := addBlock(cache, rootN(3), rootN(1), 2) // sibling of b at slot 2

	if got := cache.getBlockByRoot(rootN(2)); got != b {
		t.Errorf("getBlockByRoot(2) = %v, want b", got)
	}
	if got := cache.getBlockByRoot(rootN(99)); got != nil {
		t.Errorf("getBlockByRoot(unknown) = %v, want nil", got)
	}

	slot2 := cache.getBlocksBySlot(2)
	if len(slot2) != 2 {
		t.Errorf("getBlocksBySlot(2) len = %d, want 2", len(slot2))
	}

	children := cache.getBlocksByParentRoot(rootN(1))
	if len(children) != 2 {
		t.Fatalf("getBlocksByParentRoot(1) len = %d, want 2", len(children))
	}
	gotB, gotC := false, false
	for _, ch := range children {
		if ch == b {
			gotB = true
		}
		if ch == c {
			gotC = true
		}
	}
	if !gotB || !gotC {
		t.Errorf("children of root 1 = %v, want both b and c", children)
	}
	_ = a
}

func TestBlockCacheCanonicalDistance(t *testing.T) {
	cache := newBlockCache(newTestIndexer())

	// Build a linear chain a(1) -> b(2) -> c(3) -> head d(4), plus a fork
	// sibling x(2) off a.
	a := addBlock(cache, rootN(1), leanapi.Root{}, 1)
	addBlock(cache, rootN(2), rootN(1), 2)
	addBlock(cache, rootN(3), rootN(2), 3)
	head := addBlock(cache, rootN(4), rootN(3), 4)
	addBlock(cache, rootN(5), rootN(1), 2) // non-canonical sibling

	tests := []struct {
		name         string
		blockRoot    leanapi.Root
		head         leanapi.Root
		maxDistance  uint64
		wantCanon    bool
		wantDistance uint64
	}{
		{"equal", head.Root, head.Root, 0, true, 0},
		{"ancestor a from head", a.Root, head.Root, 0, true, 3},
		{"ancestor b from head", rootN(2), head.Root, 0, true, 2},
		{"non-canonical sibling", rootN(5), head.Root, 0, false, 0},
		{"within maxDistance", rootN(3), head.Root, 2, true, 1},
		{"exceeds maxDistance", a.Root, head.Root, 2, false, 0},
		{"unknown head", rootN(1), rootN(200), 0, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canon, dist := cache.getCanonicalDistance(tt.blockRoot, tt.head, tt.maxDistance)
			if canon != tt.wantCanon || dist != tt.wantDistance {
				t.Errorf("getCanonicalDistance(%v,%v,%d) = (%v,%d), want (%v,%d)",
					tt.blockRoot, tt.head, tt.maxDistance, canon, dist, tt.wantCanon, tt.wantDistance)
			}
		})
	}

	if !cache.isCanonicalBlock(a.Root, head.Root) {
		t.Errorf("isCanonicalBlock(a, head) = false, want true")
	}
	if cache.isCanonicalBlock(rootN(5), head.Root) {
		t.Errorf("isCanonicalBlock(sibling, head) = true, want false")
	}
}

func TestBlockCacheRemoveBlock(t *testing.T) {
	cache := newBlockCache(newTestIndexer())

	addBlock(cache, rootN(1), leanapi.Root{}, 1)
	b := addBlock(cache, rootN(2), rootN(1), 2)
	c := addBlock(cache, rootN(3), rootN(1), 2)

	cache.removeBlock(b)

	if cache.getBlockByRoot(rootN(2)) != nil {
		t.Errorf("removed block still in root map")
	}
	if got := cache.getBlocksBySlot(2); len(got) != 1 || got[0] != c {
		t.Errorf("slot map after remove = %v, want [c]", got)
	}
	children := cache.getBlocksByParentRoot(rootN(1))
	if len(children) != 1 || children[0] != c {
		t.Errorf("parent map after remove = %v, want [c]", children)
	}
}

func TestBlockCacheCleanupBlocks(t *testing.T) {
	cache := newBlockCache(newTestIndexer())
	addBlock(cache, rootN(1), leanapi.Root{}, 1)
	addBlock(cache, rootN(2), rootN(1), 2)
	addBlock(cache, rootN(3), rootN(2), 3)

	cleanup := cache.getCleanupBlocks(3)
	if len(cleanup) != 2 {
		t.Errorf("getCleanupBlocks(3) len = %d, want 2 (slots 1,2)", len(cleanup))
	}
}
