package lean

import (
	"sync"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// blockCache is an in-memory, reorg-aware cache of lean blocks.
//
// It is the lean port of Dora's beacon blockCache. The execution-block index
// (execBlockMap) is dropped: lean blocks carry no execution payload. Blocks are
// keyed by root, indexed by slot, and linked child→parent for fork detection.
type blockCache struct {
	indexer     *Indexer
	cacheMutex  sync.RWMutex
	highestSlot int64
	lowestSlot  int64
	slotMap     map[leanapi.Slot][]*Block
	rootMap     map[leanapi.Root]*Block
	parentMap   map[leanapi.Root][]*Block
	latestBlock *Block // latest added block (a marker for cache changes, not necessarily the head)
	// version is a monotonic counter bumped on every mutation that can affect
	// canonical selection (new node, body attach, parent link, removal). It is
	// the short-circuit key for computeCanonicalChain: unlike the latestBlock
	// pointer it advances even when a body/parent edge changes on an existing
	// node (so vote-only changes still trigger recomputation).
	version uint64
}

// newBlockCache creates a new instance of blockCache.
func newBlockCache(indexer *Indexer) *blockCache {
	return &blockCache{
		indexer:    indexer,
		lowestSlot: -1,
		slotMap:    map[leanapi.Slot][]*Block{},
		rootMap:    map[leanapi.Root]*Block{},
		parentMap:  map[leanapi.Root][]*Block{},
	}
}

// createOrGetBlock creates a new block with the given root and slot, or returns
// the existing block if one is already cached. The boolean reports whether the
// block was newly created.
func (cache *blockCache) createOrGetBlock(root leanapi.Root, slot leanapi.Slot) (*Block, bool) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	if cache.rootMap[root] != nil {
		return cache.rootMap[root], false
	}

	cacheBlock := newBlock(root, slot)
	cache.rootMap[root] = cacheBlock
	cache.slotMap[slot] = append(cache.slotMap[slot], cacheBlock)

	if int64(slot) > cache.highestSlot {
		cache.highestSlot = int64(slot)
	}
	if cache.lowestSlot < 0 || int64(slot) < cache.lowestSlot {
		cache.lowestSlot = int64(slot)
	}

	cache.latestBlock = cacheBlock
	cache.version++

	return cacheBlock, true
}

// addBlockToParentMap links the given block under its parent root so it can be
// found as a child during fork detection.
//
// Self-parent edges are never filed: the genesis/anchor block is created with
// root == parentRoot == zero, and filing it under parentMap[zero] would make it
// its own child, causing findHead to loop forever. Such a block simply has no
// parent edge in the cache.
func (cache *blockCache) addBlockToParentMap(block *Block) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	parentRoot := block.GetParentRoot()
	if parentRoot == block.Root {
		return
	}

	for _, parentBlock := range cache.parentMap[parentRoot] {
		if parentBlock == block {
			return
		}
	}

	cache.parentMap[parentRoot] = append(cache.parentMap[parentRoot], block)
	cache.version++
}

// markChanged bumps the cache version. Callers use this after mutating a cached
// block's body/parent (via Block.SetBlock) so canonical recomputation is not
// short-circuited away. It is separate from the block mutation itself because
// Block has its own (finer-grained) locking.
func (cache *blockCache) markChanged() {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()
	cache.version++
}

// snapshotBodies returns a copy of every cached block's decoded body, read
// while holding the cache read lock. This serializes body reads against
// removeBlock/Dispose (which take the write lock), avoiding a data race on
// Block.body / Block.isDisposed in callers like collectLatestVotes.
func (cache *blockCache) snapshotBodies() []*leanapi.Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	bodies := make([]*leanapi.Block, 0, len(cache.rootMap))
	for _, block := range cache.rootMap {
		if body := block.GetBody(); body != nil {
			bodies = append(bodies, body)
		}
	}
	return bodies
}

// getLatestBlock returns the most recently added block under the read lock.
func (cache *blockCache) getLatestBlock() *Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()
	return cache.latestBlock
}

// getVersion returns the current cache version under the read lock.
func (cache *blockCache) getVersion() uint64 {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()
	return cache.version
}

// size returns the number of blocks currently held in the cache.
func (cache *blockCache) size() int {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()
	return len(cache.rootMap)
}

// getBlockByRoot returns the cached block with the given root, or nil.
func (cache *blockCache) getBlockByRoot(root leanapi.Root) *Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	return cache.rootMap[root]
}

// getBlocksBySlot returns a copy of the cached blocks at the given slot.
func (cache *blockCache) getBlocksBySlot(slot leanapi.Slot) []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	blocks := make([]*Block, len(cache.slotMap[slot]))
	if len(blocks) > 0 {
		copy(blocks, cache.slotMap[slot])
	}

	return blocks
}

// getBlocksByParentRoot returns a copy of the blocks whose parent is parentRoot
// (i.e. its children).
func (cache *blockCache) getBlocksByParentRoot(parentRoot leanapi.Root) []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	cachedBlocks := cache.parentMap[parentRoot]
	blocks := make([]*Block, len(cachedBlocks))
	if len(blocks) > 0 {
		copy(blocks, cachedBlocks)
	}

	return blocks
}

// getCleanupBlocks returns all cached blocks below finalizedSlot. These are
// candidates for cache eviction once finalization has advanced past them.
func (cache *blockCache) getCleanupBlocks(finalizedSlot leanapi.Slot) []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	blocks := []*Block{}
	for slot, slotBlocks := range cache.slotMap {
		if slot >= finalizedSlot {
			continue
		}

		blocks = append(blocks, slotBlocks...)
	}

	return blocks
}

// getAllBlocks returns a snapshot of every block currently in the cache.
func (cache *blockCache) getAllBlocks() []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	blocks := make([]*Block, 0, len(cache.rootMap))
	for _, block := range cache.rootMap {
		blocks = append(blocks, block)
	}

	return blocks
}

// getForkBlocks returns the cached blocks that belong to the specified forkId.
func (cache *blockCache) getForkBlocks(forkId ForkKey) []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	blocks := []*Block{}
	for _, slotBlocks := range cache.slotMap {
		for _, block := range slotBlocks {
			if block.GetForkId() != forkId {
				continue
			}

			blocks = append(blocks, block)
		}
	}

	return blocks
}

// isCanonicalBlock reports whether blockRoot is an ancestor of (or equal to)
// head, walking the cached parent chain.
func (cache *blockCache) isCanonicalBlock(blockRoot leanapi.Root, head leanapi.Root) bool {
	res, _ := cache.getCanonicalDistance(blockRoot, head, 0)
	return res
}

// getCanonicalDistance reports whether blockRoot lies on the chain ending at
// head, and the number of hops from head down to blockRoot. maxDistance bounds
// the walk (0 = unbounded).
func (cache *blockCache) getCanonicalDistance(blockRoot leanapi.Root, head leanapi.Root, maxDistance uint64) (bool, uint64) {
	canonical, distance, _ := cache.getCanonicalDistanceEx(blockRoot, head, maxDistance)
	return canonical, distance
}

// getCanonicalDistanceEx is getCanonicalDistance with an extra uncached flag
// that disambiguates the two false cases:
//   - (false, 0, false): blockRoot is provably NOT on head's chain (the walk
//     reached head's cached anchor without matching).
//   - (false, 0, true):  the walk hit an ancestor that is not in the cache, so
//     canonicality could not be determined (e.g. the common ancestor lies below
//     the finalized boundary and was pruned). Callers must not treat this as
//     "definitely not canonical".
func (cache *blockCache) getCanonicalDistanceEx(blockRoot leanapi.Root, head leanapi.Root, maxDistance uint64) (canonical bool, distance uint64, uncached bool) {
	if head == blockRoot {
		return true, 0, false
	}

	canonicalBlock := cache.getBlockByRoot(head)
	if canonicalBlock == nil {
		// head itself isn't cached: undetermined.
		return false, 0, true
	}

	block := cache.getBlockByRoot(blockRoot)

	for canonicalBlock != nil {
		if block != nil && canonicalBlock.Slot < block.Slot {
			return false, 0, false
		}

		parentRoot := canonicalBlock.GetParentRoot()

		distance++
		if maxDistance > 0 && distance > maxDistance {
			return false, 0, false
		}

		if parentRoot == blockRoot {
			return true, distance, false
		}

		// Reached the anchor (self-parent or zero parent) without a match: the
		// chain is fully walked and blockRoot is not on it.
		if parentRoot.IsZero() || parentRoot == canonicalBlock.Root {
			return false, 0, false
		}

		canonicalBlock = cache.getBlockByRoot(parentRoot)
		if canonicalBlock == nil {
			// the parent (an ancestor of head) isn't cached: undetermined.
			return false, 0, true
		}
	}

	return false, 0, false
}

// removeBlock removes the given block from all cache indexes and disposes it.
func (cache *blockCache) removeBlock(block *Block) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	// remove from the root map.
	delete(cache.rootMap, block.Root)

	// remove from the slot map.
	slotBlocks := cache.slotMap[block.Slot]
	if len(slotBlocks) == 1 && slotBlocks[0] == block {
		delete(cache.slotMap, block.Slot)
	} else if len(slotBlocks) > 1 {
		for i, slotBlock := range slotBlocks {
			if slotBlock == block {
				cache.slotMap[block.Slot] = append(slotBlocks[:i], slotBlocks[i+1:]...)
				break
			}
		}
	}

	// remove from the parent map.
	parentRoot := block.GetParentRoot()
	parentBlocks := cache.parentMap[parentRoot]
	if len(parentBlocks) == 1 && parentBlocks[0] == block {
		delete(cache.parentMap, parentRoot)
	} else if len(parentBlocks) > 1 {
		for i, parentBlock := range parentBlocks {
			if parentBlock == block {
				cache.parentMap[parentRoot] = append(parentBlocks[:i], parentBlocks[i+1:]...)
				break
			}
		}
	}

	cache.version++

	block.Dispose()
}
