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
		{"slots", "/slots", &SlotsPageData{
			Slots: []*SlotsPageDataSlot{
				{Slot: 2, Epoch: 2, Status: uint8(dbtypes.Orphaned), Synchronized: true, BlockRoot: []byte{5, 6, 7, 8}},
			},
			SlotCount:           1,
			DisplayEpoch:        true,
			DisplaySlot:         true,
			DisplayStatus:       true,
			DisplayTime:         true,
			DisplayProposer:     true,
			DisplayAttestations: true,
			DisplayColCount:     6,
			IsDefaultPage:       true,
			TotalPages:          1,
			PageSize:            slotsPerPage,
			FirstPageLink:       "/slots",
		}, "Slots"},
		{"slot", "/slot/", &SlotPageData{
			Slot:   3,
			Epoch:  3,
			Status: uint16(dbtypes.Canonical),
			Block: &SlotPageBlockData{
				BlockRoot:            []byte{9, 9},
				ParentRoot:           []byte{8, 8},
				StateRoot:            []byte{7, 7},
				AttestationsCount:    1,
				SlotsPerEpoch:        1,
				TargetCommitteeSize:  4,
				MaxCommitteesPerSlot: 1,
				Attestations: []*SlotPageAttestation{
					{Slot: 3, CommitteeIndex: []uint64{0}, AggregationBits: []byte{0x01}, Validators: []uint64{0}, BeaconBlockSlot: 3},
				},
			},
			SlotBlocks: []*SlotPageSlotBlock{{BlockRoot: []byte{9, 9}, Status: uint16(dbtypes.Canonical), IsCurrent: true}},
		}, "Slot"},
		{"slotnotfound", "/slot/", struct{}{}, "Slot"},
		{"finality", "/finality", struct {
			JustifiedSlot, FinalizedSlot uint64
			Finalized, Justified         []*dbtypes.Checkpoint
		}{}, "Finality"},
		{"validators", "/validators", &ValidatorsPageData{
			Validators: []*ValidatorsPageDataValidator{
				{Index: 0, PublicKey: []byte{0xab, 0xcd}, State: "Active"},
				{Index: 1, PublicKey: []byte{0xef, 0x01}, State: "Active"},
			},
			ValidatorCount:   2,
			LastValidator:    2,
			FilterStatusOpts: []ValidatorsPageDataStatusOption{{Status: "Active", Count: 2}},
			FilterCredTypes:  map[uint8]bool{},
			Sorting:          "index",
			IsDefaultSorting: true,
			IsDefaultPage:    true,
			TotalPages:       1,
			PageSize:         2,
			CurrentPageIndex: 1,
			LastPageIndex:    1,
			FilteredPageLink: "/validators?f&c=2",
			UrlParams:        []urlParam{{Key: "c", Value: "2"}},
		}, "Validators"},
		{"forkchoice", "/forkchoice", struct{ NodeUIURL string }{NodeUIURL: "http://node/ui"}, "Fork Choice"},
		{"clients", "/clients", &ClientsCLPageData{
			Clients: []*ClientsCLPageDataClient{
				{Index: 0, Name: "ethlambda", Version: "ethlambda/v0.1.0", HeadSlot: 42, HeadRoot: []byte{0xde, 0xad}, Status: "online", PeerID: "ethlambda"},
			},
			ClientCount:      1,
			Nodes:            map[string]*clientsCLNode{},
			ShowPeerDASInfos: false,
			Sorting:          "index",
			IsDefaultSorting: true,
		}, "Consensus clients"},
		{"forks", "/forks", &ForksPageData{
			Forks: []*ForksPageDataFork{
				{HeadSlot: 42, HeadRoot: []byte{0xde, 0xad}, ClientCount: 1, Clients: []*ForksPageDataClient{
					{Index: 0, Name: "ethlambda", Version: "ethlambda/v0.1.0", Status: "online", HeadSlot: 42, Distance: 0},
				}},
			},
			ForkCount: 1,
		}, "Forks"},
		{"chain_forks", "/chain_forks", &ChainForksPageData{
			ChainSpecs: &ChainSpecs{SlotsPerEpoch: 1, SlotDurationMs: 4000, CurrentSlot: 42, EpochsFor12h: 10800},
		}, "Chain Forks"},
		{"epochs", "/epochs", &EpochsPageData{
			Epochs: nil, EpochCount: 0, IsDefaultPage: true, TotalPages: 1, PageSize: 25,
		}, "Epochs"},
		{"epoch", "/epoch/", &EpochPageData{
			Epoch: 1, PreviousEpoch: 0, NextEpoch: 0, Synchronized: false,
		}, "Epoch"},
		{"epochnotfound", "/epoch/", struct{}{}, "Epoch"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := &captureWriter{}
			r.render(rec, c.name, "lean-dora · "+c.title, c.path, c.data)
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
