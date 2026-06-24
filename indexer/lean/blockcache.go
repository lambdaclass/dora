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

	return cacheBlock, true
}

// addBlockToParentMap links the given block under its parent root so it can be
// found as a child during fork detection.
func (cache *blockCache) addBlockToParentMap(block *Block) {
	cache.cacheMutex.Lock()
	defer cache.cacheMutex.Unlock()

	parentRoot := block.GetParentRoot()

	for _, parentBlock := range cache.parentMap[parentRoot] {
		if parentBlock == block {
			return
		}
	}

	cache.parentMap[parentRoot] = append(cache.parentMap[parentRoot], block)
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

// getForkBlocks returns the cached blocks that belong to the specified forkId.
func (cache *blockCache) getForkBlocks(forkId ForkKey) []*Block {
	cache.cacheMutex.RLock()
	defer cache.cacheMutex.RUnlock()

	blocks := []*Block{}
	for _, slotBlocks := range cache.slotMap {
		for _, block := range slotBlocks {
			if block.forkId != forkId {
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
	if head == blockRoot {
		return true, 0
	}

	canonicalBlock := cache.getBlockByRoot(head)
	if canonicalBlock == nil {
		return false, 0
	}

	block := cache.getBlockByRoot(blockRoot)

	var distance uint64 = 0

	for canonicalBlock != nil {
		if block != nil && canonicalBlock.Slot < block.Slot {
			return false, 0
		}

		parentRoot := canonicalBlock.GetParentRoot()

		distance++
		if maxDistance > 0 && distance > maxDistance {
			return false, 0
		}

		if parentRoot == blockRoot {
			return true, distance
		}

		// Reached the anchor (self-parent or zero parent) without a match.
		if parentRoot.IsZero() || parentRoot == canonicalBlock.Root {
			return false, 0
		}

		canonicalBlock = cache.getBlockByRoot(parentRoot)
		if canonicalBlock == nil {
			return false, 0
		}
	}

	return false, 0
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

	block.Dispose()
}
