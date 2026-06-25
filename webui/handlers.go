package webui

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

const slotsPerPage = 50

// dashboardRows is how many recent slots/blocks the homepage panels show.
const dashboardRows = 10

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	data := s.buildIndexData(ctx)
	s.renderer.render(w, "dashboard", "lean-dora · Dashboard", "/", data)
}

// handleIndexData serves the homepage panel feed consumed by page-index.js
// ($.get("/index/data")). It returns the same IndexPageData handleDashboard
// renders, as JSON, plus the X-Server-Time header the JS reads to correct
// relative-time display.
func (s *Server) handleIndexData(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	data := s.buildIndexData(ctx)
	// Epoch milliseconds: page-index.js parseInt()s this header (same as the
	// server-time meta) to correct relative-time rendering. RFC3339 would parse
	// to the year only, breaking every delta.
	w.Header().Set("X-Server-Time", strconv.FormatInt(time.Now().UnixMilli(), 10))
	writeJSON(w, data)
}

// buildIndexData assembles the homepage IndexPageData (recent slots/blocks plus
// the head/finalized/justified summary). Shared by handleDashboard (HTML render)
// and handleIndexData (JSON feed) so both stay in lockstep.
func (s *Server) buildIndexData(ctx context.Context) *IndexPageData {
	head, justified, finalized := s.indexer.HeadState()
	vc := s.validatorCount(ctx)
	slots, err := db.GetSlotsByRange(ctx, sub(head, dashboardRows-1), head, dashboardRows)
	if err != nil {
		s.logger.WithError(err).Warn("dashboard: failed to load recent slots")
	}

	return &IndexPageData{
		// Lean has no epochs: map slot 1:1 onto the epoch chrome so the
		// "Epoch" / "Current Slot" fields both read the head slot.
		CurrentEpoch:          head,
		CurrentSlot:           head,
		CurrentFinalizedEpoch: int64(finalized),
		CurrentJustifiedEpoch: int64(justified),
		CurrentScheduledCount: 0,   // no scheduling view in lean
		CurrentEpochProgress:  100, // epoch == slot, always "complete"

		SlotsPerEpoch:   1, // lean: 1 slot per "epoch"
		SlotDurationMs:  s.slotSeconds() * 1000,
		EpochDurationMs: s.slotSeconds() * 1000,

		// Validator economics do not exist in lean: report count only, zero the rest.
		ActiveValidatorCount:    vc,
		EnteringValidatorCount:  0,
		ExitingValidatorCount:   0,
		TotalEligibleEther:      0,
		AverageValidatorBalance: 0,

		NetworkName:           s.networkName(),
		GenesisTime:           s.genesisTime(),
		GenesisValidatorsRoot: nil,

		NetworkForks:  s.networkForks(),
		ForkTreeWidth: 0,

		// Lean has no epochs: leave the recent-epochs panel empty. Use a non-nil
		// empty slice so it marshals to [] not null — page-index.js binds the
		// recent-epochs panel with `if: epochs().length == 0`, and `null.length`
		// throws a knockout exception that breaks the whole view (the slots/blocks
		// lists silently empty on the first /index/data poll).
		RecentEpochs:     []*IndexPageDataEpochs{},
		RecentEpochCount: 0,

		RecentSlots:      s.toIndexSlots(slots),
		RecentSlotCount:  uint64(len(slots)),
		RecentBlocks:     s.toIndexBlocks(slots),
		RecentBlockCount: uint64(len(slots)),
	}
}

// toIndexSlots maps canonical-ordered db slots to the homepage recent-slots
// rows. The fork graph is a single straight column (lean shows no competing
// forks here).
func (s *Server) toIndexSlots(slots []*dbtypes.Slot) []*IndexPageDataSlots {
	out := make([]*IndexPageDataSlots, 0, len(slots))
	for _, sl := range slots {
		out = append(out, &IndexPageDataSlots{
			Epoch:      sl.Slot, // epoch == slot in lean
			Slot:       sl.Slot,
			Ts:         s.slotTime(sl.Slot),
			Proposer:   sl.Proposer,
			Status:     uint64(sl.Status),
			BlockRoot:  sl.Root,
			ParentRoot: sl.ParentRoot,
			ForkGraph:  nil,
		})
	}
	return out
}

// toIndexBlocks maps the same canonical slots to recent-block rows. WithEthBlock
// is always false (no execution layer) so the eth-block column renders "-".
func (s *Server) toIndexBlocks(slots []*dbtypes.Slot) []*IndexPageDataBlocks {
	out := make([]*IndexPageDataBlocks, 0, len(slots))
	for _, sl := range slots {
		out = append(out, &IndexPageDataBlocks{
			Epoch:        sl.Slot,
			Slot:         sl.Slot,
			WithEthBlock: false,
			Ts:           s.slotTime(sl.Slot),
			Proposer:     sl.Proposer,
			Status:       uint64(sl.Status),
			BlockRoot:    sl.Root,
		})
	}
	return out
}

// networkForks returns the single lean fork derived from the chain spec's fork
// digest, always active. This replaces Dora's eth fork ladder.
func (s *Server) networkForks() []*IndexPageDataForks {
	fork := &IndexPageDataForks{
		Name:   "lean",
		Epoch:  0,
		Active: true,
		Type:   "consensus",
	}
	if spec := s.chainSpec(); spec != nil {
		fork.Time = spec.GenesisTime
		if b, err := hex.DecodeString(strings.TrimPrefix(spec.ForkDigest, "0x")); err == nil {
			fork.ForkDigest = b
		}
	}
	return []*IndexPageDataForks{fork}
}

func (s *Server) handleSlots(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	head, _, finalized := s.indexer.HeadState()

	// Pagination: the page is a max-slot window. Dora's page-jump form posts an
	// "s" (start slot) param; we also keep the legacy "max" param working.
	maxSlot := head
	if v := r.URL.Query().Get("s"); v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			maxSlot = parsed
		}
	} else if v := r.URL.Query().Get("max"); v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			maxSlot = parsed
		}
	}
	if maxSlot > head {
		maxSlot = head
	}
	minSlot := sub(maxSlot, slotsPerPage-1)
	slots, err := db.GetSlotsByRange(ctx, minSlot, maxSlot, slotsPerPage)
	if err != nil {
		s.logger.WithError(err).Warn("slots: failed to load slot range")
	}

	rows := make([]*SlotsPageDataSlot, 0, len(slots))
	var firstSlot, lastSlot uint64
	for i, sl := range slots {
		if i == 0 {
			firstSlot = sl.Slot
		}
		lastSlot = sl.Slot
		rows = append(rows, &SlotsPageDataSlot{
			Slot:             sl.Slot,
			Epoch:            sl.Slot, // epoch == slot in lean
			Ts:               s.slotTime(sl.Slot),
			Finalized:        sl.Slot <= finalized,
			Scheduled:        false,
			Status:           uint8(sl.Status),
			Synchronized:     true,
			Proposer:         sl.Proposer,
			AttestationCount: sl.AttestationCount,
			BlockRoot:        sl.Root,
			ParentRoot:       sl.ParentRoot,
			BlockSize:        sl.BlockSize,
			RecvDelay:        sl.RecvDelay,
		})
	}

	// Page navigation links. Page 1 (most recent) drops the "s" param.
	totalPages := head/slotsPerPage + 1
	currentPage := (head-maxSlot)/slotsPerPage + 1
	newerMax := minVal(head, maxSlot+slotsPerPage)
	olderMax := sub(minSlot, 1)

	data := &SlotsPageData{
		Slots:     rows,
		SlotCount: uint64(len(rows)),
		FirstSlot: firstSlot,
		LastSlot:  lastSlot,

		// Lean-visible columns only. The eth-only display columns stay off so the
		// template never renders them (Tx/Blobs, Gas, MEV, Builder, block size,
		// exec time, graffiti, extra data, deposits, slashings, sync agg, chain).
		DisplayEpoch:        true,
		DisplaySlot:         true,
		DisplayStatus:       true,
		DisplayTime:         true,
		DisplayProposer:     true,
		DisplayAttestations: true,
		DisplayColCount:     6,

		IsDefaultPage:    maxSlot >= head,
		TotalPages:       totalPages,
		PageSize:         slotsPerPage,
		CurrentPageIndex: currentPage,
		CurrentPageSlot:  maxSlot,
		MaxSlot:          head,
		LastPageSlot:     0,

		FirstPageLink: "/slots",
		PrevPageLink:  slotsPageLink(newerMax, head),
		NextPageLink:  slotsPageLink(olderMax, head),
		LastPageLink:  "/slots?s=" + strconv.FormatUint(slotsPerPage-1, 10),
	}
	if maxSlot < head {
		data.PrevPageIndex = currentPage - 1
	}
	if minSlot > 0 {
		data.NextPageIndex = currentPage + 1
		data.NextPageSlot = olderMax
	}
	s.renderer.render(w, "slots", "lean-dora · Slots", "/slots", data)
}

// slotsPageLink builds a /slots page link for the given window-max slot. The
// most-recent page (max >= head) drops the "s" query param.
func slotsPageLink(maxSlot, head uint64) string {
	if maxSlot >= head {
		return "/slots"
	}
	return "/slots?s=" + strconv.FormatUint(maxSlot, 10)
}

func (s *Server) handleSlotDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	head, _, finalized := s.indexer.HeadState()

	id := strings.TrimPrefix(r.URL.Path, "/slot/")
	// Trim any trailing sub-path (e.g. /slot/N/duties is handled elsewhere).
	if i := strings.IndexByte(id, '/'); i >= 0 {
		id = id[:i]
	}
	var slot *dbtypes.Slot
	if strings.HasPrefix(id, "0x") {
		if b, err := hexToBytes(id); err == nil {
			var gerr error
			if slot, gerr = db.GetSlotByRoot(ctx, b); gerr != nil {
				s.logger.WithError(gerr).Warn("slot detail: failed to load slot by root")
			}
		}
	} else if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		slots, gerr := db.GetSlotsByRange(ctx, n, n, 2)
		if gerr != nil {
			s.logger.WithError(gerr).Warn("slot detail: failed to load slot by number")
		}
		if len(slots) > 0 {
			// Prefer the canonical row if multiple exist at this slot.
			slot = slots[0]
			for _, sr := range slots {
				if sr.Status == dbtypes.Canonical {
					slot = sr
					break
				}
			}
		}
	}

	if slot == nil {
		s.renderer.render(w, "slotnotfound", "lean-dora · Slot not found", "/slot/", struct{}{})
		return
	}

	votes, err := db.GetVotesForSlot(ctx, slot.Slot)
	if err != nil {
		s.logger.WithError(err).Warn("slot detail: failed to load votes")
	}
	data := s.toSlotPage(ctx, slot, votes, head, finalized)
	s.renderer.render(w, "slot", "lean-dora · Slot "+strconv.FormatUint(slot.Slot, 10), "/slot/", data)
}

// toSlotPage maps a db slot plus its votes onto Dora's slot-detail model.
//
// Status mapping: dbtypes Missing(0)/Canonical(1)/Orphaned(2) lines up with the
// template's Missed/Proposed/Orphaned. A Missing slot carries no block, so
// Block stays nil and only the overview header (slot/status/time/proposer)
// renders. Each vote becomes one single-bit attestation row.
func (s *Server) toSlotPage(ctx context.Context, slot *dbtypes.Slot, votes []*dbtypes.Vote, head, finalized uint64) *SlotPageData {
	data := &SlotPageData{
		Slot:           slot.Slot,
		Epoch:          slot.Slot, // epoch == slot in lean
		EpochFinalized: slot.Slot <= finalized,
		Ts:             s.slotTime(slot.Slot),
		PreviousSlot:   sub(slot.Slot, 1),
		Status:         uint16(slot.Status),
		Proposer:       slot.Proposer,
		SlotBlocks: []*SlotPageSlotBlock{{
			BlockRoot: slot.Root,
			Status:    uint16(slot.Status),
			IsCurrent: true,
		}},
	}
	if slot.Slot < head {
		data.NextSlot = slot.Slot + 1
	}

	// A canonical or orphaned slot has a block; a missed slot does not.
	if slot.Status != dbtypes.Missing {
		atts := s.votesToAttestations(slot, votes, s.validatorCount(ctx))
		block := &SlotPageBlockData{
			BlockRoot:  slot.Root,
			ParentRoot: slot.ParentRoot,
			StateRoot:  slot.StateRoot,
			// Count aggregated attestations (cards), not raw votes: normally one
			// card covering all validators, matching the block's attestation count.
			AttestationsCount: uint64(len(atts)),
			// One committee of size ValidatorCount (no shuffling in lean).
			SlotsPerEpoch:        1,
			TargetCommitteeSize:  s.validatorCount(ctx),
			MaxCommitteesPerSlot: 1,
			Attestations:         atts,
			// Non-nil so includeJSON renders [] not null: attestations.html does
			// JSON.parse(.ValidatorNames).forEach(...), and null.forEach throws a
			// JS exception that aborts the attestations script (no rows render).
			ValidatorNames: []SlotPageValidatorName{},
		}
		data.Block = block
	}
	return data
}

// votesToAttestations aggregates lean per-validator votes into attestation rows.
// Lean stores one vote per validator; aggregators combine the votes that share
// the same attestation data (head/source/target). We regroup by that data so
// each card represents one aggregated attestation over the FULL validator
// committee: Validators lists every validator (0..N-1) in index order and one
// aggregation bit is set per validator that cast this vote. The attestations.html
// summary then reads "K validators attesting, N-K not (pct%)" instead of the
// misleading per-vote "1 of 1". A single committee (index 0) holds all validators
// since lean has no committee shuffling.
func (s *Server) votesToAttestations(slot *dbtypes.Slot, votes []*dbtypes.Vote, validatorCount uint64) []*SlotPageAttestation {
	// Committee size = validator count, but never smaller than the highest voter
	// index + 1, so the committee always covers every voter even if the count is
	// momentarily stale (and Validators stays non-empty, which keeps the JS from
	// firing the lazy committee-duties fetch).
	vc := validatorCount
	for _, v := range votes {
		if v.ValidatorIndex+1 > vc {
			vc = v.ValidatorIndex + 1
		}
	}
	committee := make([]uint64, 0, vc)
	for i := uint64(0); i < vc; i++ {
		committee = append(committee, i)
	}
	bitlen := (int(vc) + 7) / 8

	type dataKey struct {
		head                   string
		sourceSlot, targetSlot uint64
		sourceRoot, targetRoot string
	}
	groups := map[dataKey]*SlotPageAttestation{}
	order := make([]dataKey, 0, len(votes))
	for _, v := range votes {
		k := dataKey{string(v.HeadRoot), v.SourceSlot, v.TargetSlot, string(v.SourceRoot), string(v.TargetRoot)}
		att, ok := groups[k]
		if !ok {
			att = &SlotPageAttestation{
				Slot:            v.Slot,
				CommitteeIndex:  []uint64{0},
				TotalActive:     vc,
				AggregationBits: make([]byte, bitlen),
				Validators:      committee,
				Signature:       nil,
				BeaconBlockRoot: v.HeadRoot,
				BeaconBlockSlot: slot.Slot,
				SourceEpoch:     v.SourceSlot, // epoch == slot in lean
				SourceRoot:      v.SourceRoot,
				TargetEpoch:     v.TargetSlot,
				TargetRoot:      v.TargetRoot,
			}
			groups[k] = att
			order = append(order, k)
		}
		// Bit position == validator index (no committee shuffling in lean).
		att.AggregationBits[v.ValidatorIndex/8] |= 1 << (v.ValidatorIndex % 8)
	}

	atts := make([]*SlotPageAttestation, 0, len(order))
	for _, k := range order {
		atts = append(atts, groups[k])
	}
	return atts
}

func (s *Server) handleFinality(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	_, justified, finalized := s.indexer.HeadState()
	fcps, err := db.GetCheckpoints(ctx, dbtypes.CheckpointFinalized, 50)
	if err != nil {
		s.logger.WithError(err).Warn("finality: failed to load finalized checkpoints")
	}
	jcps, err := db.GetCheckpoints(ctx, dbtypes.CheckpointJustified, 50)
	if err != nil {
		s.logger.WithError(err).Warn("finality: failed to load justified checkpoints")
	}

	data := struct {
		JustifiedSlot uint64
		FinalizedSlot uint64
		Finalized     []*dbtypes.Checkpoint
		Justified     []*dbtypes.Checkpoint
	}{
		JustifiedSlot: justified,
		FinalizedSlot: finalized,
		Finalized:     fcps,
		Justified:     jcps,
	}
	s.renderer.render(w, "finality", "lean-dora · Finality", "/finality", data)
}

func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	validators, err := db.GetValidators(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("validators: failed to load validator registry")
	}

	// The lean validator registry is small and static (no activation queue, no
	// exits, no balances), so we serve the whole set on a single page and ignore
	// the filter/sort query params Dora's template exposes.
	rows := make([]*ValidatorsPageDataValidator, 0, len(validators))
	for _, v := range validators {
		rows = append(rows, &ValidatorsPageDataValidator{
			Index:     v.Index,
			PublicKey: v.AttestationPubkey,
			// Every registry member is permanently active in lean. No economics
			// (Balance/EffectiveBalance stay 0 → "0 (0 ETH)"), no epochs
			// (ShowActivation/ShowExit false → "-"), no withdrawal credentials
			// (ShowWithdrawAddress false → "-"), no liveness up-check.
			State: "Active",
		})
	}

	count := uint64(len(rows))
	data := &ValidatorsPageData{
		Validators:     rows,
		ValidatorCount: count,
		FirstValidator: 0,
		LastValidator:  count,

		// A single inert "Active" status option; no credential-type filtering.
		FilterStatusOpts: []ValidatorsPageDataStatusOption{{Status: "Active", Count: count}},
		FilterCredTypes:  map[uint8]bool{},

		Sorting:          "index",
		IsDefaultSorting: true,
		IsDefaultPage:    true,

		// One page holds the whole registry, so pagination controls stay hidden
		// (template gates them on TotalPages > 1).
		TotalPages:       1,
		PageSize:         count,
		CurrentPageIndex: 1,
		LastPageIndex:    1,

		FilteredPageLink: "/validators?f&c=" + strconv.FormatUint(count, 10),
		UrlParams: []urlParam{{
			Key:   "c",
			Value: strconv.FormatUint(count, 10),
		}},
	}
	s.renderer.render(w, "validators", "lean-dora · Validators", "/validators", data)
}

// handleValidatorDetail renders a minimal validator-detail page. The lean
// indexer has no rich validator registry (no balances, epochs, or withdrawal
// data), so the page shows only the index plus the validator's XMSS public keys
// when the index is in the registry; the eth-only fields render as "—". This
// exists so proposer links on the slot-detail page (/validator/{index}) resolve
// to a styled page instead of a 404.
func (s *Server) handleValidatorDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	id := strings.TrimPrefix(r.URL.Path, "/validator/")
	if i := strings.IndexByte(id, '/'); i >= 0 {
		id = id[:i]
	}

	data := struct {
		Index             uint64
		Found             bool
		AttestationPubkey []byte
		ProposalPubkey    []byte
	}{}

	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		// Non-numeric index: render the page with Found=false rather than 404.
		s.renderer.render(w, "validator", "lean-dora · Validator", "/validators", data)
		return
	}
	data.Index = n

	validators, err := db.GetValidators(ctx)
	if err != nil {
		s.logger.WithError(err).Warn("validator detail: failed to load validators")
	}
	for _, v := range validators {
		if v.Index == n {
			data.Found = true
			data.AttestationPubkey = v.AttestationPubkey
			data.ProposalPubkey = v.ProposalPubkey
			break
		}
	}

	s.renderer.render(w, "validator", "lean-dora · Validator "+strconv.FormatUint(n, 10), "/validators", data)
}

func (s *Server) handleForkChoice(w http.ResponseWriter, r *http.Request) {
	data := struct {
		NodeUIURL string
	}{
		NodeUIURL: strings.TrimRight(s.nodeEndpoint, "/") + "/lean/v0/fork_choice/ui",
	}
	s.renderer.render(w, "forkchoice", "lean-dora · Fork Choice", "/forkchoice", data)
}

// handleForkChoiceJSON proxies the node's fork-choice tree as JSON for any UI
// that wants to fetch it from the explorer origin.
func (s *Server) handleForkChoiceJSON(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()
	fc, err := s.client.GetForkChoice(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, fc)
}

// chainSpec resolves the chain spec, preferring the snapshot captured at
// construction but falling back to the indexer's live spec. The indexer fetches
// the spec asynchronously, so the snapshot handed to NewServer can be nil if the
// server is built before initSpec completes (a startup race); resolving lazily
// here ensures timestamps and counts use genesis once it is available rather
// than falling back to unix-0 forever.
func (s *Server) chainSpec() *leanapi.ChainSpec {
	if s.spec != nil {
		return s.spec
	}
	if s.indexer != nil {
		return s.indexer.Spec()
	}
	return nil
}

func (s *Server) validatorCount(ctx context.Context) uint64 {
	if c, err := db.GetValidatorCount(ctx); err == nil && c > 0 {
		return c
	}
	if spec := s.chainSpec(); spec != nil {
		return spec.ValidatorCount
	}
	return 0
}

func (s *Server) slotSeconds() uint64 {
	if spec := s.chainSpec(); spec != nil && spec.MillisecondsPerSlot > 0 {
		return spec.MillisecondsPerSlot / 1000
	}
	return 4
}

// slotTime returns the wall-clock time for a slot from the chain spec, falling
// back to genesis + slot*slotSeconds when SlotToTime is unavailable.
func (s *Server) slotTime(slot uint64) time.Time {
	if spec := s.chainSpec(); spec != nil {
		return spec.SlotToTime(slot)
	}
	return time.Unix(int64(slot*s.slotSeconds()), 0).UTC()
}

// genesisTime returns the chain genesis time, or the zero time if unknown.
func (s *Server) genesisTime() time.Time {
	if spec := s.chainSpec(); spec != nil {
		return spec.GenesisTimestamp()
	}
	return time.Time{}
}

// networkName derives a display name for the lean network from the fork digest.
func (s *Server) networkName() string {
	if spec := s.chainSpec(); spec != nil && spec.ForkDigest != "" {
		return "lean-" + strings.TrimPrefix(spec.ForkDigest, "0x")
	}
	return "lean"
}
