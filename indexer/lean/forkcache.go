package lean

import (
	"sort"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common/lru"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// forkCache tracks the set of known forks in the lean chain.
//
// This is the lean port of Dora's beacon forkCache. All epoch / SlotsPerEpoch
// arithmetic is dropped (lean has no epochs); finalization works on a raw
// finalized slot. DB load/persist of the fork state is intentionally omitted
// and handled by a later task, so this cache is purely in-memory.
type forkCache struct {
	indexer *Indexer
	// cacheMutex guards forkMap and the Fork struct fields (parentFork,
	// headBlock) that fork detection mutates while readers (getForkHeads,
	// setFinalizedSlot) iterate.
	cacheMutex sync.RWMutex
	forkMap    map[ForkKey]*Fork
	// finalizedForkId and lastForkId are atomics: fork detection writes them
	// while holding only forkProcessLock (which excludes other writers but not
	// readers), so the reads/writes must be lock-free-safe.
	finalizedForkId atomic.Uint64
	lastForkId      atomic.Uint64
	parentIdCache   *lru.Cache[ForkKey, ForkKey]
	forkProcessLock sync.Mutex
}

// newForkCache creates a new instance of forkCache.
func newForkCache(indexer *Indexer) *forkCache {
	return &forkCache{
		indexer:       indexer,
		forkMap:       make(map[ForkKey]*Fork),
		parentIdCache: lru.NewCache[ForkKey, ForkKey](1000),
	}
}

// nextForkId atomically increments and returns the next fork id.
func (cache *forkCache) nextForkId() ForkKey {
	return ForkKey(cache.lastForkId.Add(1))
}

// getLastForkId returns the current highest assigned fork id.
func (cache *forkCache) getLastForkId() ForkKey {
	return ForkKey(cache.lastForkId.Load())
}

// getFinalizedForkId returns the finalized fork id.
func (cache *forkCache) getFinalizedForkId() ForkKey {
	return ForkKey(cache.finalizedForkId.Load())
}

// setForkHead sets a fork's head block under the cache lock.
func (cache *forkCache) setForkHead(fork *Fork, head *Block) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()
	fork.headBlock = head
}

// setForkParent sets a fork's parent fork id under the cache lock.
func (cache *forkCache) setForkParent(fork *Fork, parent ForkKey) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()
	fork.parentFork = parent
}

// updateForkHeadIfNewer advances a fork's head block to candidate if candidate
// is at a higher slot (or no head is set), under the cache lock.
func (cache *forkCache) updateForkHeadIfNewer(fork *Fork, candidate *Block) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()
	if fork.headBlock == nil || candidate.Slot > fork.headBlock.Slot {
		fork.headBlock = candidate
	}
}

// getForkById retrieves a fork from the cache by its ID.
func (cache *forkCache) getForkById(forkId ForkKey) *Fork {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	return cache.forkMap[forkId]
}

// addFork adds a fork to the cache.
func (cache *forkCache) addFork(fork *Fork) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	cache.forkMap[fork.forkId] = fork
}

// getForkByLeaf retrieves a fork from the cache by its leaf root.
func (cache *forkCache) getForkByLeaf(leafRoot leanapi.Root) *Fork {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	for _, fork := range cache.forkMap {
		if fork.leafRoot == leafRoot {
			return fork
		}
	}

	return nil
}

// getForkByBase retrieves forks from the cache by their base root.
func (cache *forkCache) getForkByBase(baseRoot leanapi.Root) []*Fork {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	forks := []*Fork{}
	for _, fork := range cache.forkMap {
		if fork.baseRoot == baseRoot {
			forks = append(forks, fork)
		}
	}

	return forks
}

// removeFork removes a fork from the cache.
func (cache *forkCache) removeFork(forkId ForkKey) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	delete(cache.forkMap, forkId)
}

// ForkHead represents a fork head with its ID, fork, and head block.
type ForkHead struct {
	ForkId ForkKey
	Fork   *Fork
	Block  *Block
}

// getForkHeads returns the fork heads in the cache.
// A head fork is a fork that no other fork is building on top of, so it
// contains the head of its chain segment.
func (cache *forkCache) getForkHeads() []*ForkHead {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	forkParents := map[ForkKey]bool{}
	for _, fork := range cache.forkMap {
		if fork.parentFork != 0 {
			forkParents[fork.parentFork] = true
		}
	}

	finalizedForkId := cache.getFinalizedForkId()
	forkHeads := []*ForkHead{}
	if !forkParents[finalizedForkId] {
		canonicalBlocks := cache.indexer.blockCache.getForkBlocks(finalizedForkId)
		sort.Slice(canonicalBlocks, func(i, j int) bool {
			return canonicalBlocks[i].Slot > canonicalBlocks[j].Slot
		})
		if len(canonicalBlocks) > 0 {
			forkHeads = append(forkHeads, &ForkHead{
				ForkId: finalizedForkId,
				// Fork is intentionally nil for the finalized fork (it has no
				// Fork object); consumers must nil-check ForkHead.Fork.
				Block: canonicalBlocks[0],
			})
		}
	}

	for forkId, fork := range cache.forkMap {
		if !forkParents[forkId] {
			forkHeads = append(forkHeads, &ForkHead{
				ForkId: forkId,
				Fork:   cache.forkMap[forkId],
				Block:  fork.headBlock,
			})
		}
	}

	return forkHeads
}

// setFinalizedSlot advances finalization to the given finalized slot.
//
// It drops all forks whose leaf lies below the finalized slot and recomputes
// the finalized fork id by walking down from the justified/finalized block at
// justifiedRoot until reaching the finalized slot. This is the lean equivalent
// of Dora's setFinalizedEpoch with all epoch math removed (it operates on the
// raw finalized slot directly). DB persistence is handled by a later task.
func (cache *forkCache) setFinalizedSlot(finalizedSlot leanapi.Slot, justifiedRoot leanapi.Root) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	for _, fork := range cache.forkMap {
		if fork.leafSlot >= finalizedSlot {
			continue
		}

		cache.parentIdCache.Add(fork.forkId, fork.parentFork)
		delete(cache.forkMap, fork.forkId)
	}

	latestFinalizedBlock := cache.indexer.blockCache.getBlockByRoot(justifiedRoot)
	finalizedForkId := ForkKey(0)
	for {
		if latestFinalizedBlock == nil {
			break
		}

		finalizedForkId = latestFinalizedBlock.GetForkId()

		if latestFinalizedBlock.Slot <= finalizedSlot {
			break
		}

		parentRoot := latestFinalizedBlock.GetParentRoot()
		if parentRoot.IsZero() {
			break
		}

		latestFinalizedBlock = cache.indexer.blockCache.getBlockByRoot(parentRoot)
	}

	cache.finalizedForkId.Store(uint64(finalizedForkId))
}
