package lean

import "testing"

// ethlambda wraps every SSE payload in an envelope:
//
//	data: {"event":"block","data":{"slot":34,"root":"0x44..."}}
//
// so the typed payload is nested one level under "data". decodeEvent must
// unwrap it; a regression here silently yields zero slot/root and stalls
// ingestion (every block fetch 404s on the zero root).
func TestDecodeEventUnwrapsEnvelope(t *testing.T) {
	root := "0x4421394320fbcc618926ea60d098239026d73ad4d46465332560da58d512b278"

	t.Run("block", func(t *testing.T) {
		data := `{"event":"block","data":{"slot":34,"root":"` + root + `"}}`
		ev := decodeEvent(StreamEventBlock, data)
		if ev == nil || ev.Err != nil {
			t.Fatalf("decode failed: %+v", ev)
		}
		if ev.Block == nil {
			t.Fatal("Block payload nil")
		}
		if ev.Block.Slot != 34 {
			t.Errorf("slot = %d, want 34", ev.Block.Slot)
		}
		if ev.Block.Root.IsZero() || ev.Block.Root.String() != root {
			t.Errorf("root = %s, want %s", ev.Block.Root.String(), root)
		}
	})

	t.Run("head", func(t *testing.T) {
		parent := "0x0e37cdef99a34332821290739c4d8bf29519f99d43bf39792cd965721a94007e"
		data := `{"event":"head","data":{"slot":35,"root":"` + root + `","parent_root":"` + parent + `"}}`
		ev := decodeEvent(StreamEventHead, data)
		if ev == nil || ev.Err != nil || ev.Head == nil {
			t.Fatalf("decode failed: %+v", ev)
		}
		if ev.Head.Slot != 35 || ev.Head.Root.String() != root || ev.Head.ParentRoot.String() != parent {
			t.Errorf("head mismatch: %+v", ev.Head)
		}
	})

	t.Run("finalized_checkpoint", func(t *testing.T) {
		data := `{"event":"finalized_checkpoint","data":{"slot":48,"root":"` + root + `"}}`
		ev := decodeEvent(StreamEventFinalizedCheckpoint, data)
		if ev == nil || ev.Err != nil || ev.Finalized == nil {
			t.Fatalf("decode failed: %+v", ev)
		}
		if ev.Finalized.Slot != 48 || ev.Finalized.Root.String() != root {
			t.Errorf("finalized mismatch: %+v", ev.Finalized)
		}
	})

	// Defensive: a bare (un-enveloped) payload must still decode, so the parser
	// tolerates either shape.
	t.Run("bare payload fallback", func(t *testing.T) {
		data := `{"slot":7,"root":"` + root + `"}`
		ev := decodeEvent(StreamEventBlock, data)
		if ev == nil || ev.Err != nil || ev.Block == nil {
			t.Fatalf("decode failed: %+v", ev)
		}
		if ev.Block.Slot != 7 || ev.Block.Root.String() != root {
			t.Errorf("bare payload mismatch: %+v", ev.Block)
		}
	})
}
