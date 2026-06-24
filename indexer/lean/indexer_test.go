package lean

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
	"github.com/ethpandaops/dora/types"
)

// mockClient is an in-memory ConsensusRPCClient for indexer tests.
type mockClient struct {
	mu          sync.Mutex
	genesis     *leanapi.Genesis
	spec        *leanapi.Spec
	sync        *leanapi.SyncState
	blocks      map[string]*leanapi.Block // keyed by id (slot string or root hex)
	rangeBlocks []*leanapi.Block
	forkChoice  *leanapi.ForkChoice
	justified   *leanapi.JustifiedCheckpoint
	events      chan leanapi.StreamEvent
}

func (m *mockClient) GetGenesis(context.Context) (*leanapi.Genesis, error) { return m.genesis, nil }
func (m *mockClient) GetSpec(context.Context) (*leanapi.Spec, error)       { return m.spec, nil }
func (m *mockClient) GetSyncState(context.Context) (*leanapi.SyncState, error) {
	return m.sync, nil
}
func (m *mockClient) GetNodeIdentity(context.Context) (*leanapi.NodeIdentity, error) {
	return &leanapi.NodeIdentity{Version: "test"}, nil
}
func (m *mockClient) GetBlockByID(_ context.Context, id string) (*leanapi.Block, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blocks[id], nil
}
func (m *mockClient) GetBlockHeaderByID(context.Context, string) (*leanapi.BlockHeader, error) {
	return nil, nil
}
func (m *mockClient) GetBlocksByRange(_ context.Context, start, count uint64) ([]*leanapi.Block, error) {
	out := []*leanapi.Block{}
	for _, b := range m.rangeBlocks {
		if uint64(b.Slot) >= start && uint64(b.Slot) < start+count {
			out = append(out, b)
		}
	}
	return out, nil
}
func (m *mockClient) GetAttestations(context.Context, *uint64) ([]*leanapi.Attestation, error) {
	return nil, nil
}
func (m *mockClient) GetForkChoice(context.Context) (*leanapi.ForkChoice, error) {
	return m.forkChoice, nil
}
func (m *mockClient) GetJustifiedCheckpoint(context.Context) (*leanapi.JustifiedCheckpoint, error) {
	return m.justified, nil
}
func (m *mockClient) StreamEvents(context.Context) (<-chan leanapi.StreamEvent, error) {
	return m.events, nil
}

func rootOf(b byte) leanapi.Root {
	var r leanapi.Root
	for i := range r {
		r[i] = b
	}
	return r
}

func initTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbcfg := &types.DatabaseConfig{
		Engine: "sqlite",
		Sqlite: &types.SqliteDatabaseConfig{
			File:         filepath.Join(dir, "test.sqlite"),
			MaxOpenConns: 10,
			MaxIdleConns: 5,
		},
	}
	db.MustInitDB(dbcfg)
	if err := db.ApplyEmbeddedDbSchema(-2); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	t.Cleanup(db.MustCloseDB)
}

// bitlistWith builds an SSZ bitlist (1 byte) with the given validator indices
// set plus the length-delimiter sentinel.
func bitlistWith(indices ...int) leanapi.HexBytes {
	// Use 8 validators -> 2 bytes (data byte + sentinel byte at bit 8).
	bits := []byte{0, 0}
	for _, i := range indices {
		bits[i/8] |= 1 << uint(i%8)
	}
	bits[1] |= 1 << 0 // sentinel at bit 8 marks length=8
	return bits
}

func TestIndexerBackfillAndEvents(t *testing.T) {
	initTestDB(t)

	block1 := &leanapi.Block{
		Slot: 1, ProposerIndex: 5,
		ParentRoot: rootOf(0x00), StateRoot: rootOf(0x11),
		Body: leanapi.BlockBody{Attestations: []leanapi.AggregatedAttestation{
			{
				AggregationBits: bitlistWith(0, 2, 3),
				Data: leanapi.AttestationData{
					Slot:   1,
					Head:   leanapi.Checkpoint{Root: rootOf(0xaa), Slot: 1},
					Target: leanapi.Checkpoint{Root: rootOf(0xbb), Slot: 1},
					Source: leanapi.Checkpoint{Root: rootOf(0xcc), Slot: 0},
				},
			},
		}},
	}
	block2 := &leanapi.Block{
		Slot: 2, ProposerIndex: 7,
		ParentRoot: rootOf(0xa1), StateRoot: rootOf(0x22),
	}

	events := make(chan leanapi.StreamEvent, 8)
	mc := &mockClient{
		genesis: &leanapi.Genesis{GenesisTime: 1000, ValidatorCount: 8},
		spec:    &leanapi.Spec{MillisecondsPerSlot: 4000, IntervalsPerSlot: 5, MillisecondsPerInterval: 800},
		sync:    &leanapi.SyncState{HeadSlot: 1},
		blocks: map[string]*leanapi.Block{
			rootOf(0xb2).String(): block2,
		},
		rangeBlocks: []*leanapi.Block{block1},
		forkChoice: &leanapi.ForkChoice{
			Nodes: []leanapi.ForkChoiceNode{
				{Root: rootOf(0xa1), Slot: 1, ParentRoot: rootOf(0x00), ProposerIndex: 5, Weight: 8},
				{Root: rootOf(0xb2), Slot: 2, ParentRoot: rootOf(0xa1), ProposerIndex: 7, Weight: 8},
			},
			Head:           rootOf(0xb2),
			Justified:      leanapi.Checkpoint{Root: rootOf(0xa1), Slot: 1},
			Finalized:      leanapi.Checkpoint{Root: rootOf(0x00), Slot: 0},
			ValidatorCount: 8,
		},
		justified: &leanapi.JustifiedCheckpoint{Root: rootOf(0xa1), Slot: 1},
		events:    events,
	}

	// Feed a block event (slot 2) and a finalized checkpoint, then close.
	events <- leanapi.StreamEvent{Type: leanapi.StreamEventBlock, Block: &leanapi.BlockEventData{Slot: 2, Root: rootOf(0xb2)}}
	events <- leanapi.StreamEvent{Type: leanapi.StreamEventHead, Head: &leanapi.HeadEventData{Slot: 2, Root: rootOf(0xb2), ParentRoot: rootOf(0xa1)}}
	events <- leanapi.StreamEvent{Type: leanapi.StreamEventFinalizedCheckpoint, Finalized: &leanapi.FinalizedCheckpointEventData{Slot: 1, Root: rootOf(0xa1)}}
	close(events)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	idx := NewIndexer(ctx, logrus.New().WithField("test", true), mc)
	// Run Start in a goroutine; it returns after the stream channel closes and
	// it tries to reconnect (which we stop by cancelling once data is present).
	done := make(chan struct{})
	go func() {
		_ = idx.Start()
		close(done)
	}()

	// Wait until the finalized checkpoint has been recorded, then cancel.
	deadline := time.After(8 * time.Second)
	for {
		cps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointFinalized, 10)
		if len(cps) > 0 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatal("timed out waiting for finalized checkpoint")
		case <-time.After(50 * time.Millisecond):
		}
	}
	cancel()
	<-done

	// Use a fresh context for assertions (ctx is now cancelled).
	rctx := context.Background()

	// Slot 1 (backfill) persisted.
	s1, err := db.GetSlotByRoot(rctx, rootOf(0xa1).Bytes())
	if err != nil || s1 == nil {
		t.Fatalf("slot 1 not persisted: %v", err)
	}
	if s1.Proposer != 5 || s1.AttestationCount != 1 {
		t.Fatalf("bad slot1 row: %+v", s1)
	}

	// Slot 2 (block event) persisted.
	s2, err := db.GetSlotByRoot(rctx, rootOf(0xb2).Bytes())
	if err != nil || s2 == nil {
		t.Fatalf("slot 2 not persisted: %v", err)
	}
	if s2.Proposer != 7 {
		t.Fatalf("bad slot2 row: %+v", s2)
	}

	// Votes from slot1's attestation: validators 0,2,3.
	count, err := db.CountVotesForSlot(rctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected 3 votes for slot 1, got %d", count)
	}

	// Finalized checkpoint recorded and slot1 marked finalized.
	fcps, _ := db.GetCheckpoints(rctx, dbtypes.CheckpointFinalized, 10)
	if len(fcps) != 1 || fcps[0].Slot != 1 {
		t.Fatalf("bad finalized checkpoints: %+v", fcps)
	}
}

// attBody builds a block body with a single attestation: validators vote for
// headRoot at attSlot.
func attBody(attSlot leanapi.Slot, headRoot leanapi.Root, validators ...int) leanapi.BlockBody {
	return leanapi.BlockBody{Attestations: []leanapi.AggregatedAttestation{{
		AggregationBits: bitlistWith(validators...),
		Data: leanapi.AttestationData{
			Slot: attSlot,
			Head: leanapi.Checkpoint{Root: headRoot, Slot: attSlot},
		},
	}}}
}

// TestCacheDrivenIngestion exercises the rewired two-tier flow end to end:
// genesis seed -> linear chain via block events -> competing sibling that wins
// votes -> reorg + status flips -> finalize -> flush + prune.
func TestCacheDrivenIngestion(t *testing.T) {
	initTestDB(t)

	genesisRoot := rootOf(0x00) // zero root; seed anchors here at slot 0
	// Use small, distinct roots so lexicographic tiebreaks are predictable.
	rA := rootOf(0x10) // slot 1
	rB := rootOf(0x20) // slot 2 (left branch)
	rC := rootOf(0x30) // slot 2 (right branch, the eventual winner)
	rD := rootOf(0x40) // slot 3 on right branch

	blkA := &leanapi.Block{Slot: 1, ProposerIndex: 1, ParentRoot: genesisRoot, StateRoot: rootOf(0xa0)}
	blkB := &leanapi.Block{Slot: 2, ProposerIndex: 2, ParentRoot: rA, StateRoot: rootOf(0xb0),
		Body: attBody(2, rB, 0)}
	blkC := &leanapi.Block{Slot: 2, ProposerIndex: 3, ParentRoot: rA, StateRoot: rootOf(0xc0),
		Body: attBody(2, rC, 1)}
	blkD := &leanapi.Block{Slot: 3, ProposerIndex: 4, ParentRoot: rC, StateRoot: rootOf(0xd0),
		Body: attBody(3, rC, 0, 1, 2)}

	mc := &mockClient{
		genesis: &leanapi.Genesis{GenesisTime: 1000, ValidatorCount: 8},
		spec:    &leanapi.Spec{MillisecondsPerSlot: 4000, IntervalsPerSlot: 5, MillisecondsPerInterval: 800},
		blocks: map[string]*leanapi.Block{
			rA.String(): blkA, rB.String(): blkB, rC.String(): blkC, rD.String(): blkD,
		},
		forkChoice: &leanapi.ForkChoice{Head: rA, Finalized: leanapi.Checkpoint{Root: genesisRoot, Slot: 0}},
		justified:  &leanapi.JustifiedCheckpoint{Root: genesisRoot, Slot: 0},
	}

	ctx := context.Background()
	idx := NewIndexer(ctx, logrus.New().WithField("test", true), mc)

	// Seed: finalized anchor at genesis (slot 0). The seed caches no block (zero
	// root), so create the genesis cache node explicitly as the anchor.
	idx.spec = leanapi.NewChainSpec(mc.genesis, mc.spec)
	gen, _ := idx.blockCache.createOrGetBlock(genesisRoot, 0)
	gen.SetBlock(&leanapi.Block{Slot: 0, ParentRoot: leanapi.Root{}, StateRoot: rootOf(0x99)})
	gen.forkId = idx.forkCache.finalizedForkId
	gen.forkChecked = true
	idx.blockCache.addBlockToParentMap(gen)

	// Ingest the linear chain A -> B via block events.
	idx.onBlockEvent(&leanapi.BlockEventData{Slot: 1, Root: rA})
	idx.onBlockEvent(&leanapi.BlockEventData{Slot: 2, Root: rB})
	idx.onHeadEvent(&leanapi.HeadEventData{Slot: 2, Root: rB, ParentRoot: rA})

	// B is canonical (only branch with a vote).
	if sb, _ := db.GetSlotByRoot(ctx, rB.Bytes()); sb == nil || sb.Status != dbtypes.Canonical {
		t.Fatalf("expected B canonical, got %+v", sb)
	}

	// Competing sibling C, then D on C with 3 votes -> right branch wins.
	idx.onBlockEvent(&leanapi.BlockEventData{Slot: 2, Root: rC})
	idx.onBlockEvent(&leanapi.BlockEventData{Slot: 3, Root: rD})
	idx.onHeadEvent(&leanapi.HeadEventData{Slot: 3, Root: rD, ParentRoot: rC})

	// After the head moves to D's branch: D and C canonical, B orphaned.
	sd, _ := db.GetSlotByRoot(ctx, rD.Bytes())
	if sd == nil || sd.Status != dbtypes.Canonical {
		t.Fatalf("expected D canonical, got %+v", sd)
	}
	sc, _ := db.GetSlotByRoot(ctx, rC.Bytes())
	if sc == nil || sc.Status != dbtypes.Canonical {
		t.Fatalf("expected C canonical, got %+v", sc)
	}
	sb, _ := db.GetSlotByRoot(ctx, rB.Bytes())
	if sb == nil || sb.Status != dbtypes.Orphaned {
		t.Fatalf("expected B orphaned after reorg, got %+v", sb)
	}

	// Finalize at slot 2 (root C): flush slots <= 2 to DB, prune from cache.
	idx.onFinalizedEvent(&leanapi.FinalizedCheckpointEventData{Slot: 2, Root: rC})

	// Cache pruned: no blocks at slot <= 2 remain in the cache.
	if got := idx.blockCache.getBlocksBySlot(2); len(got) != 0 {
		t.Errorf("expected slot 2 pruned from cache, got %d blocks", len(got))
	}
	if got := idx.blockCache.getBlocksBySlot(1); len(got) != 0 {
		t.Errorf("expected slot 1 pruned from cache, got %d blocks", len(got))
	}
	// D (slot 3 > finalized 2) stays in the cache.
	if idx.blockCache.getBlockByRoot(rD) == nil {
		t.Errorf("expected D (slot 3) to remain cached")
	}

	// DB: C finalized canonical, B finalized orphaned.
	sc2, _ := db.GetSlotByRoot(ctx, rC.Bytes())
	if sc2 == nil || !sc2.Finalized || sc2.Status != dbtypes.Canonical {
		t.Errorf("expected C finalized+canonical in DB, got %+v", sc2)
	}
	sb2, _ := db.GetSlotByRoot(ctx, rB.Bytes())
	if sb2 == nil || sb2.Status != dbtypes.Orphaned {
		t.Errorf("expected B orphaned in DB after finalize, got %+v", sb2)
	}

	// Finalized checkpoint recorded.
	fcps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointFinalized, 10)
	found := false
	for _, cp := range fcps {
		if cp.Slot == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finalized checkpoint at slot 2, got %+v", fcps)
	}
}

func TestSetBitsDropsSentinel(t *testing.T) {
	// 0,2,3 set with sentinel at bit 8.
	got := setBits(bitlistWith(0, 2, 3))
	want := []uint64{0, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
