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
