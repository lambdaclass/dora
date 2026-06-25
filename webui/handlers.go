package webui

import (
	"context"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

const slotsPerPage = 50

// dashboardRows is how many recent slots/blocks the homepage panels show.
const dashboardRows = 10

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	head, justified, finalized := s.indexer.HeadState()
	vc := s.validatorCount(ctx)
	slots, _ := db.GetSlotsByRange(ctx, sub(head, dashboardRows-1), head, dashboardRows)

	data := &IndexPageData{
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

		// Lean has no epochs: leave the recent-epochs panel empty (renders greyed).
		RecentEpochs:     nil,
		RecentEpochCount: 0,

		RecentSlots:      s.toIndexSlots(slots),
		RecentSlotCount:  uint64(len(slots)),
		RecentBlocks:     s.toIndexBlocks(slots),
		RecentBlockCount: uint64(len(slots)),
	}
	s.renderer.render(w, "dashboard", "lean-dora · Dashboard", "/", data)
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
	if s.spec != nil {
		fork.Time = s.spec.GenesisTime
		if b, err := hex.DecodeString(strings.TrimPrefix(s.spec.ForkDigest, "0x")); err == nil {
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
	slots, _ := db.GetSlotsByRange(ctx, minSlot, maxSlot, slotsPerPage)

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
			slot, _ = db.GetSlotByRoot(ctx, b)
		}
	} else if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		if slots, _ := db.GetSlotsByRange(ctx, n, n, 2); len(slots) > 0 {
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

	votes, _ := db.GetVotesForSlot(ctx, slot.Slot)
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
		block := &SlotPageBlockData{
			BlockRoot:         slot.Root,
			ParentRoot:        slot.ParentRoot,
			StateRoot:         slot.StateRoot,
			AttestationsCount: uint64(len(votes)),
			// One vote per validator: a single committee of size ValidatorCount.
			SlotsPerEpoch:        1,
			TargetCommitteeSize:  s.validatorCount(ctx),
			MaxCommitteesPerSlot: 1,
			Attestations:         s.votesToAttestations(slot, votes),
		}
		data.Block = block
	}
	return data
}

// votesToAttestations turns lean votes into single-validator attestation rows.
// Each lean validator casts one vote, so every row has exactly one aggregation
// bit set (committee index 0) and lists that one validator. The JS in
// attestations.html base64-decodes these fields to render the bitfield + links.
func (s *Server) votesToAttestations(slot *dbtypes.Slot, votes []*dbtypes.Vote) []*SlotPageAttestation {
	atts := make([]*SlotPageAttestation, 0, len(votes))
	for _, v := range votes {
		atts = append(atts, &SlotPageAttestation{
			Slot:            v.Slot,
			CommitteeIndex:  []uint64{0},
			TotalActive:     1,
			AggregationBits: []byte{0x01}, // single attesting validator
			Validators:      []uint64{v.ValidatorIndex},
			Signature:       nil,
			BeaconBlockRoot: v.HeadRoot,
			BeaconBlockSlot: slot.Slot,
			SourceEpoch:     v.SourceSlot, // epoch == slot in lean
			SourceRoot:      v.SourceRoot,
			TargetEpoch:     v.TargetSlot,
			TargetRoot:      v.TargetRoot,
		})
	}
	return atts
}

func (s *Server) handleFinality(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	_, justified, finalized := s.indexer.HeadState()
	fcps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointFinalized, 50)
	jcps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointJustified, 50)

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

	validators, _ := db.GetValidators(ctx)
	data := struct {
		ValidatorCount uint64
		Validators     []*dbtypes.Validator
	}{
		ValidatorCount: s.validatorCount(ctx),
		Validators:     validators,
	}
	s.renderer.render(w, "validators", "lean-dora · Validators", "/validators", data)
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

func (s *Server) validatorCount(ctx context.Context) uint64 {
	if c, err := db.GetValidatorCount(ctx); err == nil && c > 0 {
		return c
	}
	if s.spec != nil {
		return s.spec.ValidatorCount
	}
	return 0
}

func (s *Server) slotSeconds() uint64 {
	if s.spec != nil && s.spec.MillisecondsPerSlot > 0 {
		return s.spec.MillisecondsPerSlot / 1000
	}
	return 4
}

// slotTime returns the wall-clock time for a slot from the chain spec, falling
// back to genesis + slot*slotSeconds when SlotToTime is unavailable.
func (s *Server) slotTime(slot uint64) time.Time {
	if s.spec != nil {
		return s.spec.SlotToTime(slot)
	}
	return time.Unix(int64(slot*s.slotSeconds()), 0).UTC()
}

// genesisTime returns the chain genesis time, or the zero time if unknown.
func (s *Server) genesisTime() time.Time {
	if s.spec != nil {
		return s.spec.GenesisTimestamp()
	}
	return time.Time{}
}

// networkName derives a display name for the lean network from the fork digest.
func (s *Server) networkName() string {
	if s.spec != nil && s.spec.ForkDigest != "" {
		return "lean-" + strings.TrimPrefix(s.spec.ForkDigest, "0x")
	}
	return "lean"
}
