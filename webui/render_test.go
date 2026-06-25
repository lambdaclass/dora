package webui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ethpandaops/dora/dbtypes"
)

// TestRenderPages renders every lean page with a representative (mostly
// zero-value) data struct and asserts the template executes without error and
// emits the Dora Bootstrap chrome.
func TestRenderPages(t *testing.T) {
	r, err := newRenderer()
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	cases := []struct {
		name  string
		path  string
		data  any
		title string
	}{
		{"dashboard", "/", &IndexPageData{
			CurrentEpoch:         1,
			CurrentSlot:          1,
			SlotsPerEpoch:        1,
			CurrentEpochProgress: 100,
			NetworkName:          "lean-test",
			NetworkForks: []*IndexPageDataForks{
				{Name: "lean", Active: true, Type: "consensus", ForkDigest: []byte{0x12, 0x34, 0x56, 0x78}},
			},
			RecentSlots: []*IndexPageDataSlots{
				{Epoch: 1, Slot: 1, Status: 1, BlockRoot: []byte{1, 2, 3, 4}},
			},
			RecentSlotCount: 1,
			RecentBlocks: []*IndexPageDataBlocks{
				{Epoch: 1, Slot: 1, Status: 1, BlockRoot: []byte{1, 2, 3, 4}},
			},
			RecentBlockCount: 1,
		}, "Dashboard"},
		{"slots", "/slots", struct {
			MinSlot, MaxSlot   uint64
			Slots              []*dbtypes.Slot
			HasNewer, HasOlder bool
			NewerMax, OlderMax uint64
		}{Slots: []*dbtypes.Slot{{Slot: 2, Status: dbtypes.Orphaned}}}, "Slots"},
		{"slot", "/slot/", struct {
			Slot      *dbtypes.Slot
			Votes     []*dbtypes.Vote
			VoteCount int
		}{Slot: &dbtypes.Slot{Slot: 3, Status: dbtypes.Canonical, Root: []byte{9, 9}}}, "Slot"},
		{"slot-notfound", "/slot/", struct {
			Slot      *dbtypes.Slot
			Votes     []*dbtypes.Vote
			VoteCount int
		}{}, "Slot"},
		{"finality", "/finality", struct {
			JustifiedSlot, FinalizedSlot uint64
			Finalized, Justified         []*dbtypes.Checkpoint
		}{}, "Finality"},
		{"validators", "/validators", struct {
			ValidatorCount uint64
			Validators     []*dbtypes.Validator
		}{}, "Validators"},
		{"forkchoice", "/forkchoice", struct{ NodeUIURL string }{NodeUIURL: "http://node/ui"}, "Fork Choice"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Map the test case "name" to the actual page template name.
			page := c.name
			if page == "slot-notfound" {
				page = "slot"
			}
			rec := &captureWriter{}
			r.render(rec, page, "lean-dora · "+c.title, c.path, c.data)
			if rec.status != 0 && rec.status != 200 {
				t.Fatalf("render %s wrote error status %d: %s", c.name, rec.status, rec.body.String())
			}
			out := rec.body.String()
			for _, marker := range []string{"bootstrap.min.css", "navbar", "explorer.js"} {
				if !strings.Contains(out, marker) {
					t.Errorf("render %s: missing chrome marker %q", c.name, marker)
				}
			}
		})
	}
}

// captureWriter is a minimal http.ResponseWriter for tests.
type captureWriter struct {
	body   strings.Builder
	header http.Header
	status int
}

func (c *captureWriter) Header() http.Header {
	if c.header == nil {
		c.header = make(http.Header)
	}
	return c.header
}
func (c *captureWriter) Write(p []byte) (int, error) { return c.body.Write(p) }
func (c *captureWriter) WriteHeader(status int)      { c.status = status }

var _ http.ResponseWriter = (*captureWriter)(nil)
