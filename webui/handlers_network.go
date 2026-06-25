package webui

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// epochsNotApplicable is the muted note shown on the epochs pages: lean consensus
// has no epoch concept (one slot per "epoch"), so these pages exist only for
// navbar/chrome parity and render an empty/zeroed body.
const epochsNotApplicable = "Epochs do not apply to lean consensus (1 slot per epoch). See Slots instead."

// handleClients renders the consensus-clients page populated with the single
// connected lean node. Identity/version come from the node's /node/identity,
// head slot + sync status from /node/syncing, and the head root from the
// fork-choice tree. Peer counts and ENR/DAS columns stay empty (the lean read
// API exposes none of that).
func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	client := &ClientsCLPageDataClient{
		Index:       0,
		Name:        s.nodeName(),
		Version:     "unknown",
		Status:      "offline",
		LastRefresh: time.Now().UTC(),
		PeerID:      s.nodeName(),
	}

	if id, err := s.client.GetNodeIdentity(ctx); err == nil && id.Version != "" {
		client.Version = id.Version
	}

	// Sync state gives head slot + whether the node is still syncing.
	if sync, err := s.client.GetSyncState(ctx); err == nil {
		client.HeadSlot = sync.HeadSlot
		if sync.IsSyncing {
			client.Status = "synchronizing"
		} else {
			client.Status = "online"
		}
	} else {
		client.LastError = err.Error()
	}

	// Head root from the fork-choice tree; also a liveness signal.
	if fc, err := s.client.GetForkChoice(ctx); err == nil {
		client.HeadRoot = fc.Head.Bytes()
		if client.Status == "offline" {
			client.Status = "online"
		}
		if fc.Head.IsZero() {
			client.HeadRoot = nil
		}
	}

	data := &ClientsCLPageData{
		Clients:     []*ClientsCLPageDataClient{client},
		ClientCount: 1,
		// Peer graph / PeerDAS are off in lean: empty (non-nil) node map so
		// `range`/`len` are safe; PeerDASInfos stays nil (only read behind
		// ShowPeerDASInfos, which is false).
		Nodes:            map[string]*clientsCLNode{},
		ShowPeerDASInfos: false,
		Sorting:          "index",
		IsDefaultSorting: true,
	}
	s.renderer.render(w, "clients", "lean-dora · Consensus clients", "/clients", data)
}

// handleForks renders the forks page from the node's fork-choice tree. Each leaf
// (a node that is no other node's parent) is a fork head; the canonical fork is
// the one whose head matches the fork-choice head. In the common case there is a
// single canonical fork. Each fork lists one client row: the connected node.
func (s *Server) handleForks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	data := &ForksPageData{}

	version := "unknown"
	if id, err := s.client.GetNodeIdentity(ctx); err == nil && id.Version != "" {
		version = id.Version
	}

	fc, err := s.client.GetForkChoice(ctx)
	if err == nil && fc != nil && len(fc.Nodes) > 0 {
		data.Forks = s.forksFromForkChoice(fc, version)
		data.ForkCount = uint64(len(data.Forks))
	}
	s.renderer.render(w, "forks", "lean-dora · Forks", "/forks", data)
}

// forksFromForkChoice derives the fork list from the fork-choice tree. A leaf is
// any node that is not referenced as a parent by another node. The canonical
// fork (the one matching the fork-choice head) is placed first so the template
// labels it "Canonical".
func (s *Server) forksFromForkChoice(fc *leanapi.ForkChoice, clientVersion string) []*ForksPageDataFork {
	// Index nodes by root and collect the set of roots that are someone's parent.
	bySlot := func(n leanapi.ForkChoiceNode) uint64 { return uint64(n.Slot) }
	isParent := make(map[leanapi.Root]bool, len(fc.Nodes))
	for _, n := range fc.Nodes {
		isParent[n.ParentRoot] = true
	}

	headHeadSlot := uint64(0)
	// canonicalFound tracks whether a leaf actually matched fc.Head. If the head
	// is not itself a leaf (e.g. fc.Head sits mid-tree), no fork is canonical;
	// without this guard the loops below would mislabel forks[0] as "Canonical"
	// and compute every distance against a bogus headHeadSlot of 0.
	canonicalFound := false

	var forks []*ForksPageDataFork
	for _, n := range fc.Nodes {
		if isParent[n.Root] {
			continue // not a leaf
		}
		isCanonical := n.Root == fc.Head
		fork := &ForksPageDataFork{
			HeadSlot:    bySlot(n),
			HeadRoot:    n.Root.Bytes(),
			ClientCount: 1,
		}
		if isCanonical {
			headHeadSlot = bySlot(n)
			canonicalFound = true
		}
		forks = append(forks, fork)
		// Reorder canonical fork to front.
		if isCanonical && len(forks) > 1 {
			forks[0], forks[len(forks)-1] = forks[len(forks)-1], forks[0]
		}
	}
	// Mark the front fork canonical only when a canonical leaf was actually
	// found; otherwise no fork is canonical (head sits mid-tree).
	if canonicalFound && len(forks) > 0 {
		forks[0].Canonical = true
	}

	// Attach the single client (the connected node) to each fork. When a canonical
	// leaf was found, distance is the slot gap from the canonical head (0 for the
	// canonical fork at index 0, which the node follows). When no canonical leaf
	// was found, there is no reference head: leave every distance at 0 and do not
	// single out forks[0], since none is genuinely canonical.
	for i, fork := range forks {
		distance := uint64(0)
		if canonicalFound && i != 0 {
			// Non-canonical forks: the node does not follow them.
			distance = absDiff(headHeadSlot, fork.HeadSlot)
		}
		fork.Clients = []*ForksPageDataClient{{
			Index:       0,
			Name:        s.nodeName(),
			Version:     clientVersion,
			Status:      "online",
			HeadSlot:    fork.HeadSlot,
			Distance:    distance,
			LastRefresh: time.Now().UTC(),
		}}
	}
	return forks
}

// handleChainForks renders Dora's real chain-forks (cytoscape) page shell. The
// diagram is drawn by static/js/chain-forks-diagram.js, which AJAX-fetches a
// Dora epoch/slot data endpoint that lean does not serve; the diagram area
// therefore stays empty. The page is mounted for chrome parity so the navbar
// entry resolves to a styled Dora page rather than a 404.
func (s *Server) handleChainForks(w http.ResponseWriter, r *http.Request) {
	head, _, _ := s.indexer.HeadState()
	slotMs := s.slotSeconds() * 1000
	// Epochs == slots in lean (1 slot/epoch), so the time-window selectors map a
	// duration directly onto a slot count.
	slotsIn := func(d time.Duration) uint64 {
		if slotMs == 0 {
			return 0
		}
		return uint64(d.Milliseconds()) / slotMs
	}
	data := &ChainForksPageData{
		ChainSpecs: &ChainSpecs{
			SlotsPerEpoch:  1,
			SlotDurationMs: slotMs,
			GenesisTime:    uint64(s.genesisTime().Unix()),
			CurrentSlot:    head,
			EpochsFor12h:   slotsIn(12 * time.Hour),
			EpochsFor1d:    slotsIn(24 * time.Hour),
			EpochsFor7d:    slotsIn(7 * 24 * time.Hour),
			EpochsFor14d:   slotsIn(14 * 24 * time.Hour),
		},
	}
	s.renderer.render(w, "chain_forks", "lean-dora · Chain Forks", "/chain_forks", data)
}

// handleEpochs renders Dora's real epochs page with an empty body: lean has no
// epoch concept, so the list is always empty (the template draws its empty-state
// graphic) and a muted note explains why.
func (s *Server) handleEpochs(w http.ResponseWriter, r *http.Request) {
	data := &EpochsPageData{
		Epochs:        nil,
		EpochCount:    0,
		IsDefaultPage: true,
		TotalPages:    1,
		PageSize:      25,
	}
	s.renderer.render(w, "epochs", "lean-dora · Epochs", "/epochs", data)
}

// handleEpochDetail renders Dora's real epoch-detail page with a zeroed body for
// any requested epoch id. Lean has no epochs; the page exists for chrome parity.
// A non-numeric id renders the epoch "not found" page.
func (s *Server) handleEpochDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/epoch/")
	if i := strings.IndexByte(id, '/'); i >= 0 {
		id = id[:i]
	}
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		s.renderer.render(w, "epochnotfound", "lean-dora · Epoch not found", "/epoch/", struct{}{})
		return
	}

	data := &EpochPageData{
		Epoch:         n,
		PreviousEpoch: sub(n, 1),
		NextEpoch:     0, // no "next" in lean; chevron stays inert
		Ts:            s.slotTime(n),
		Synchronized:  false, // not indexed: shows "Not indexed yet" cells
		Slots:         nil,
	}
	s.renderer.render(w, "epoch", "lean-dora · Epoch "+strconv.FormatUint(n, 10), "/epoch/", data)
}

// nodeName returns a display name for the connected lean node.
func (s *Server) nodeName() string {
	return "ethlambda"
}

// absDiff returns |a-b| for unsigned values.
func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
