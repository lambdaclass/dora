package lean

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Sample bodies mirror the exact JSON shapes emitted by ethlambda's axum
// handlers (crates/net/rpc/src/*.rs) and lean wire types
// (crates/common/types/src/*.rs).

const sampleGenesis = `{"genesis_time":1770407233,"validator_count":30}`

const sampleSpec = `{
  "MILLISECONDS_PER_SLOT":4000,
  "INTERVALS_PER_SLOT":5,
  "MILLISECONDS_PER_INTERVAL":800,
  "HISTORICAL_ROOTS_LIMIT":262144,
  "FORK_DIGEST":"12345678"
}`

const sampleSyncing = `{"is_syncing":false,"head_slot":42,"sync_distance":0}`

const sampleIdentity = `{"version":"0.1.0"}`

// Block JSON: proposer_index/slot are numbers, roots are 0x-hex,
// body.attestations[].aggregation_bits is a 0x-hex SSZ bitlist, and
// attestation data carries head/target/source checkpoints whose slot is a
// number.
const sampleBlock = `{
  "slot":7,
  "proposer_index":3,
  "parent_root":"0x1111111111111111111111111111111111111111111111111111111111111111",
  "state_root":"0x2222222222222222222222222222222222222222222222222222222222222222",
  "body":{
    "attestations":[
      {
        "aggregation_bits":"0x0f01",
        "data":{
          "slot":6,
          "head":{"root":"0x3333333333333333333333333333333333333333333333333333333333333333","slot":6},
          "target":{"root":"0x4444444444444444444444444444444444444444444444444444444444444444","slot":5},
          "source":{"root":"0x5555555555555555555555555555555555555555555555555555555555555555","slot":4}
        }
      }
    ]
  }
}`

const sampleHeader = `{
  "slot":7,
  "proposer_index":3,
  "parent_root":"0x1111111111111111111111111111111111111111111111111111111111111111",
  "state_root":"0x2222222222222222222222222222222222222222222222222222222222222222",
  "body_root":"0x6666666666666666666666666666666666666666666666666666666666666666"
}`

const sampleAttestations = `[
  {"validator_index":0,"slot":6,"source_slot":4,"target_slot":5},
  {"validator_index":1,"slot":6,"source_slot":4,"target_slot":5}
]`

const sampleForkChoice = `{
  "nodes":[
    {"root":"0xaaaa000000000000000000000000000000000000000000000000000000000000","slot":1,"parent_root":"0x0000000000000000000000000000000000000000000000000000000000000000","proposer_index":2,"weight":30}
  ],
  "head":"0xaaaa000000000000000000000000000000000000000000000000000000000000",
  "justified":{"root":"0xbbbb000000000000000000000000000000000000000000000000000000000000","slot":1},
  "finalized":{"root":"0x0000000000000000000000000000000000000000000000000000000000000000","slot":0},
  "safe_target":"0xaaaa000000000000000000000000000000000000000000000000000000000000",
  "validator_count":30
}`

const sampleJustified = `{"root":"0xbbbb000000000000000000000000000000000000000000000000000000000000","slot":1}`

// newTestClient spins up an httptest server that routes each /lean/v0 path to a
// fixed sample body, then returns a Client pointed at it.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/lean/v0/genesis", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleGenesis)) })
	mux.HandleFunc("/lean/v0/config/spec", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleSpec)) })
	mux.HandleFunc("/lean/v0/node/syncing", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleSyncing)) })
	mux.HandleFunc("/lean/v0/node/identity", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleIdentity)) })
	mux.HandleFunc("/lean/v0/blocks/7", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleBlock)) })
	mux.HandleFunc("/lean/v0/blocks/7/header", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleHeader)) })
	mux.HandleFunc("/lean/v0/blocks", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("[" + sampleBlock + "]")) })
	mux.HandleFunc("/lean/v0/attestations", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleAttestations)) })
	mux.HandleFunc("/lean/v0/fork_choice", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleForkChoice)) })
	mux.HandleFunc("/lean/v0/checkpoints/justified", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sampleJustified)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, nil)
}

func TestGenesisDecode(t *testing.T) {
	g, err := newTestClient(t).GetGenesis(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if g.GenesisTime != 1770407233 || g.ValidatorCount != 30 {
		t.Fatalf("bad genesis: %+v", g)
	}
}

func TestSpecAndChainSpec(t *testing.T) {
	c := newTestClient(t)
	g, err := c.GetGenesis(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.GetSpec(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.MillisecondsPerSlot != 4000 || s.IntervalsPerSlot != 5 || s.ForkDigest != "12345678" {
		t.Fatalf("bad spec: %+v", s)
	}
	cs := NewChainSpec(g, s)
	if cs.SlotDuration() != 4*time.Second {
		t.Fatalf("expected 4s slots, got %v", cs.SlotDuration())
	}
	// slot 10 starts at genesis + 40s.
	want := time.Unix(1770407233, 0).Add(40 * time.Second)
	if got := cs.SlotToTime(10); !got.Equal(want) {
		t.Fatalf("SlotToTime(10) = %v, want %v", got, want)
	}
	if got := cs.TimeToSlot(want.Add(time.Second)); got != 10 {
		t.Fatalf("TimeToSlot round-trip = %d, want 10", got)
	}
}

func TestSyncAndIdentity(t *testing.T) {
	c := newTestClient(t)
	sy, err := c.GetSyncState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sy.IsSyncing || sy.HeadSlot != 42 {
		t.Fatalf("bad sync: %+v", sy)
	}
	id, err := c.GetNodeIdentity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if id.Version != "0.1.0" {
		t.Fatalf("bad identity: %+v", id)
	}
}

func TestBlockDecode(t *testing.T) {
	b, err := newTestClient(t).GetBlockByID(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if b.Slot != 7 || b.ProposerIndex != 3 {
		t.Fatalf("bad block header fields: %+v", b)
	}
	if b.ParentRoot.String() != "0x1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("bad parent root: %s", b.ParentRoot)
	}
	if len(b.Body.Attestations) != 1 {
		t.Fatalf("expected 1 attestation, got %d", len(b.Body.Attestations))
	}
	att := b.Body.Attestations[0]
	if att.Data.Slot != 6 || att.Data.Target.Slot != 5 || att.Data.Source.Slot != 4 {
		t.Fatalf("bad attestation data: %+v", att.Data)
	}
	// aggregation_bits 0x0f01 decodes to two bytes.
	if len(att.AggregationBits) != 2 || att.AggregationBits[0] != 0x0f {
		t.Fatalf("bad aggregation bits: %x", att.AggregationBits)
	}
}

func TestBlockHeaderDecode(t *testing.T) {
	h, err := newTestClient(t).GetBlockHeaderByID(context.Background(), "7")
	if err != nil {
		t.Fatal(err)
	}
	if h.Slot != 7 || h.BodyRoot.IsZero() {
		t.Fatalf("bad header: %+v", h)
	}
}

func TestBlocksByRange(t *testing.T) {
	blocks, err := newTestClient(t).GetBlocksByRange(context.Background(), 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Slot != 7 {
		t.Fatalf("bad range: %+v", blocks)
	}
}

func TestAttestationsDecode(t *testing.T) {
	slot := uint64(6)
	atts, err := newTestClient(t).GetAttestations(context.Background(), &slot)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 || atts[1].ValidatorIndex != 1 || atts[0].SourceSlot != 4 {
		t.Fatalf("bad attestations: %+v", atts)
	}
}

func TestForkChoiceDecode(t *testing.T) {
	fc, err := newTestClient(t).GetForkChoice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fc.ValidatorCount != 30 || len(fc.Nodes) != 1 {
		t.Fatalf("bad fork choice: %+v", fc)
	}
	if fc.Nodes[0].Weight != 30 || fc.Nodes[0].Slot != 1 {
		t.Fatalf("bad fc node: %+v", fc.Nodes[0])
	}
	if fc.Justified.Slot != 1 {
		t.Fatalf("bad justified: %+v", fc.Justified)
	}
}

func TestJustifiedDecode(t *testing.T) {
	cp, err := newTestClient(t).GetJustifiedCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cp.Slot != 1 {
		t.Fatalf("bad justified checkpoint: %+v", cp)
	}
}

// Checkpoint.slot tolerates a decimal string (ethlambda's deser_dec_str).
func TestCheckpointSlotAcceptsString(t *testing.T) {
	var cp Checkpoint
	if err := json.Unmarshal([]byte(`{"root":"0x`+
		"0000000000000000000000000000000000000000000000000000000000000000"+
		`","slot":"123"}`), &cp); err != nil {
		t.Fatal(err)
	}
	if cp.Slot != 123 {
		t.Fatalf("expected slot 123 from string, got %d", cp.Slot)
	}
}

func TestStreamEvents(t *testing.T) {
	// Server emits one frame of each event type using ethlambda's SSE framing.
	sse := "event: head\ndata: {\"slot\":3,\"root\":\"0x" +
		"aa00000000000000000000000000000000000000000000000000000000000000" +
		"\",\"parent_root\":\"0x" +
		"bb00000000000000000000000000000000000000000000000000000000000000" +
		"\"}\n\n" +
		"event: block\ndata: {\"slot\":3,\"root\":\"0x" +
		"aa00000000000000000000000000000000000000000000000000000000000000" +
		"\"}\n\n" +
		": keep-alive\n\n" +
		"event: finalized_checkpoint\ndata: {\"slot\":2,\"root\":\"0x" +
		"cc00000000000000000000000000000000000000000000000000000000000000" +
		"\"}\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.URL, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := c.StreamEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var head *HeadEventData
	var block *BlockEventData
	var fin *FinalizedCheckpointEventData
	for ev := range ch {
		if ev.Err != nil {
			t.Fatalf("stream error: %v", ev.Err)
		}
		switch ev.Type {
		case StreamEventHead:
			head = ev.Head
		case StreamEventBlock:
			block = ev.Block
		case StreamEventFinalizedCheckpoint:
			fin = ev.Finalized
		}
	}
	if head == nil || head.Slot != 3 {
		t.Fatalf("missing/bad head event: %+v", head)
	}
	if block == nil || block.Slot != 3 {
		t.Fatalf("missing/bad block event: %+v", block)
	}
	if fin == nil || fin.Slot != 2 {
		t.Fatalf("missing/bad finalized event: %+v", fin)
	}
}
