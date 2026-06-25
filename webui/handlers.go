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
		MinSlot  uint64
		MaxSlot  uint64
		Slots    []*dbtypes.Slot
		HasNewer bool
		HasOlder bool
		NewerMax uint64
		OlderMax uint64
	}{
		MinSlot:  minSlot,
		MaxSlot:  maxSlot,
		Slots:    slots,
		HasNewer: maxSlot < head,
		HasOlder: minSlot > 0,
		NewerMax: minVal(head, maxSlot+slotsPerPage),
		OlderMax: sub(minSlot, 1),
	}
	s.renderer.render(w, "slots", "lean-dora · Slots", "/slots", data)
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
		Slot      *dbtypes.Slot
		Votes     []*dbtypes.Vote
		VoteCount int
	}{
		Slot:      slot,
		Votes:     votes,
		VoteCount: len(votes),
	}
	s.renderer.render(w, "slot", "lean-dora · Slot", "/slot/", data)
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
