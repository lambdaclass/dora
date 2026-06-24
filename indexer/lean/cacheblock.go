package lean

import (
	"sync"
	"time"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// Block is the in-memory cache node wrapping a lean consensus block.
//
// It is the lean analogue of Dora's beacon Block, with all Ethereum coupling
// removed: there is no execution payload, no fork-version branching, no SSZ
// (dynssz) marshalling and no lazily-loaded signed header. The lean read API
// hands us the decoded block body directly, so the body is held in memory as a
// plain *leanapi.Block. Keys are leanapi.Root / leanapi.Slot.
type Block struct {
	Root leanapi.Root
	Slot leanapi.Slot

	parentRoot    leanapi.Root
	stateRoot     leanapi.Root
	proposerIndex uint64

	// body is the decoded lean block body (attestations). It may be nil for
	// blocks that are only known by header/root (e.g. a placeholder created for
	// a not-yet-seen parent).
	body *leanapi.Block

	forkId      ForkKey
	forkChecked bool

	recvDelay int32 // ms between slot start and first sighting

	seenMutex sync.RWMutex
	seenTime  time.Time

	isInUnfinalizedDb bool // block is present in the unfinalized DB table
	isDisposed        bool // block has been removed from the cache
}

// newBlock creates a new cache Block for the given root and slot.
func newBlock(root leanapi.Root, slot leanapi.Slot) *Block {
	return &Block{
		Root: root,
		Slot: slot,
	}
}

// Dispose marks the block as removed and releases its body reference.
func (block *Block) Dispose() {
	block.isDisposed = true
	block.body = nil
}

// GetParentRoot returns the parent root of this block.
//
// Unlike Dora this never returns nil: the parent root is a value type set at
// block creation (or zero for the genesis/anchor block). Callers that need to
// distinguish "no parent" should compare against the zero root.
func (block *Block) GetParentRoot() leanapi.Root {
	return block.parentRoot
}

// GetStateRoot returns the state root of this block.
func (block *Block) GetStateRoot() leanapi.Root {
	return block.stateRoot
}

// GetProposerIndex returns the proposer index of this block.
func (block *Block) GetProposerIndex() uint64 {
	return block.proposerIndex
}

// GetBody returns the decoded lean block body, or nil if only the header/root
// is known.
func (block *Block) GetBody() *leanapi.Block {
	if block.isDisposed {
		return nil
	}
	return block.body
}

// SetBlock attaches the decoded lean block body and derives the cached header
// fields (parent root, state root, proposer index) from it.
func (block *Block) SetBlock(body *leanapi.Block) {
	if block.isDisposed || body == nil {
		return
	}
	block.body = body
	block.parentRoot = body.ParentRoot
	block.stateRoot = body.StateRoot
	block.proposerIndex = body.ProposerIndex
}

// GetForkId returns the fork ID of this block.
func (block *Block) GetForkId() ForkKey {
	return block.forkId
}

// SetSeen records that this block was observed, tracking the earliest receive
// delay (ms after the slot start) and first sighting time.
func (block *Block) SetSeen(seenTime time.Time, recvDelay int32) {
	if block.isDisposed {
		return
	}
	block.seenMutex.Lock()
	defer block.seenMutex.Unlock()

	if block.seenTime.IsZero() || seenTime.Before(block.seenTime) {
		block.seenTime = seenTime
	}
	if block.recvDelay == 0 || recvDelay < block.recvDelay {
		block.recvDelay = recvDelay
	}
}

// GetSeenTime returns the earliest recorded sighting time of this block.
func (block *Block) GetSeenTime() time.Time {
	block.seenMutex.RLock()
	defer block.seenMutex.RUnlock()
	return block.seenTime
}

// GetRecvDelay returns the earliest recorded receive delay (ms) of this block.
func (block *Block) GetRecvDelay() int32 {
	block.seenMutex.RLock()
	defer block.seenMutex.RUnlock()
	return block.recvDelay
}
