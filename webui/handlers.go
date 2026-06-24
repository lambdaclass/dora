package webui

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/ethpandaops/dora/db"
	"github.com/ethpandaops/dora/dbtypes"
)

const slotsPerPage = 50

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	head, justified, finalized := s.indexer.HeadState()
	slots, _ := db.GetSlotsByRange(ctx, sub(head, slotsPerPage), head, slotsPerPage)
	vc := s.validatorCount(ctx)

	data := struct {
		Title          string
		HeadSlot       uint64
		JustifiedSlot  uint64
		FinalizedSlot  uint64
		ValidatorCount uint64
		SlotSeconds    uint64
		Slots          []*dbtypes.Slot
	}{
		Title:          "Dashboard",
		HeadSlot:       head,
		JustifiedSlot:  justified,
		FinalizedSlot:  finalized,
		ValidatorCount: vc,
		SlotSeconds:    s.slotSeconds(),
		Slots:          slots,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplDashboard.Execute(w, data)
}

func (s *Server) handleSlots(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	head, _, _ := s.indexer.HeadState()
	maxSlot := head
	if v := r.URL.Query().Get("max"); v != "" {
		if parsed, err := strconv.ParseUint(v, 10, 64); err == nil {
			maxSlot = parsed
		}
	}
	minSlot := sub(maxSlot, slotsPerPage-1)
	slots, _ := db.GetSlotsByRange(ctx, minSlot, maxSlot, slotsPerPage)

	data := struct {
		Title    string
		MinSlot  uint64
		MaxSlot  uint64
		Slots    []*dbtypes.Slot
		HasNewer bool
		HasOlder bool
		NewerMax uint64
		OlderMax uint64
	}{
		Title:    "Slots",
		MinSlot:  minSlot,
		MaxSlot:  maxSlot,
		Slots:    slots,
		HasNewer: maxSlot < head,
		HasOlder: minSlot > 0,
		NewerMax: minVal(head, maxSlot+slotsPerPage),
		OlderMax: sub(minSlot, 1),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplSlots.Execute(w, data)
}

func (s *Server) handleSlotDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	id := strings.TrimPrefix(r.URL.Path, "/slot/")
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

	var votes []*dbtypes.Vote
	if slot != nil {
		votes, _ = db.GetVotesForSlot(ctx, slot.Slot)
	}

	data := struct {
		Title     string
		Slot      *dbtypes.Slot
		Votes     []*dbtypes.Vote
		VoteCount int
	}{
		Title:     "Slot",
		Slot:      slot,
		Votes:     votes,
		VoteCount: len(votes),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplSlotDetail.Execute(w, data)
}

func (s *Server) handleFinality(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	_, justified, finalized := s.indexer.HeadState()
	fcps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointFinalized, 50)
	jcps, _ := db.GetCheckpoints(ctx, dbtypes.CheckpointJustified, 50)

	data := struct {
		Title         string
		JustifiedSlot uint64
		FinalizedSlot uint64
		Finalized     []*dbtypes.Checkpoint
		Justified     []*dbtypes.Checkpoint
	}{
		Title:         "Finality",
		JustifiedSlot: justified,
		FinalizedSlot: finalized,
		Finalized:     fcps,
		Justified:     jcps,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplFinality.Execute(w, data)
}

func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.reqCtx(r)
	defer cancel()

	validators, _ := db.GetValidators(ctx)
	data := struct {
		Title          string
		ValidatorCount uint64
		Validators     []*dbtypes.Validator
	}{
		Title:          "Validators",
		ValidatorCount: s.validatorCount(ctx),
		Validators:     validators,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplValidators.Execute(w, data)
}

func (s *Server) handleForkChoice(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title     string
		NodeUIURL string
	}{
		Title:     "Fork Choice",
		NodeUIURL: strings.TrimRight(s.nodeEndpoint, "/") + "/lean/v0/fork_choice/ui",
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmplForkChoice.Execute(w, data)
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
