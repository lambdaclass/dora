package lean

import (
	"fmt"
	"strings"

	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// processBlock processes a block and detects new forks if any. It sets the
// forkId of the supplied block and updates the forkId of all blocks affected by
// newly detected forks.
//
// This is the lean port of Dora's beacon forkCache.processBlock. The fork
// detection logic is domain-agnostic and ported faithfully; what changes:
//   - all epoch / finalization-epoch references become a raw finalized slot,
//   - DB lookups for finalized parents/children are dropped (the lean cache is
//     in-memory; DB-backed fork persistence is a later task),
//   - GetParentRoot returns a value (zero root == no parent) rather than a
//     pointer.
func (cache *forkCache) processBlock(block *Block) error {
	cache.forkProcessLock.Lock()
	defer cache.forkProcessLock.Unlock()

	parentRoot := block.GetParentRoot()

	finalizedSlot, finalizedRoot := cache.indexer.finalizedCheckpoint()

	// get fork id from parent block
	parentForkId := ForkKey(1)
	parentSlot := leanapi.Slot(0)
	parentIsProcessed := false
	parentIsFinalized := false

	if block.Slot == 0 {
		// genesis block
		parentForkId = 0
		parentSlot = 0
		parentIsProcessed = false
		parentIsFinalized = true
	} else if parentBlock := cache.indexer.blockCache.getBlockByRoot(parentRoot); parentBlock != nil && parentBlock.forkChecked {
		parentForkId = parentBlock.forkId
		parentSlot = parentBlock.Slot
		parentIsProcessed = true
		parentIsFinalized = parentBlock.Slot < finalizedSlot
	}

	if block.Root == finalizedRoot && parentForkId == 1 {
		// this is the finalization checkpoint, but we don't have a fork id for
		// it. Just use the finalized forkId 0.
		parentForkId = 0
		parentSlot = 0
		parentIsProcessed = false
		parentIsFinalized = true
		cache.finalizedForkId = parentForkId
	}

	// check if this block (c) introduces a new fork, it does so if:
	// 1. the parent (p) is known & processed and has 1 or more child blocks besides this one (c1, c2, ...)
	//  c  c1 c2
	//   \ | /
	//     p
	// 2. the current block (c) has 2 or more child blocks, multiple forks possible (c1, c2, ...)
	//  c1 c2 c3
	//   \ | /
	//     c

	newForks := []*Fork{}
	currentForkId := parentForkId // default to parent fork id

	// check scenario 1
	if parentIsProcessed {
		otherChildren := []*Block{}
		for _, child := range cache.indexer.blockCache.getBlocksByParentRoot(parentRoot) {
			if child == block {
				continue
			}

			otherChildren = append(otherChildren, child)
		}

		if len(otherChildren) > 0 {
			logbuf := strings.Builder{}

			// parent already has a child, so this block introduces a new fork
			if cache.getForkByLeaf(block.Root) != nil {
				cache.indexer.logger.Warnf("fork already exists for leaf %v [%v] (processing %v, scenario 1)", block.Slot, block.Root.String(), block.Slot)
			} else {
				cache.lastForkId++
				fork := newFork(cache.lastForkId, parentSlot, parentRoot, block, parentForkId)
				cache.addFork(fork)

				currentForkId = fork.forkId
				cache.parentIdCache.Add(fork.forkId, fork.parentFork)
				newForks = append(newForks, fork)

				fmt.Fprintf(&logbuf, ", head1(%v): %v [%v]", fork.forkId, block.Slot, block.Root.String())
			}

			if !parentIsFinalized && len(otherChildren) == 1 {
				// parent (a) is not finalized and our new detected fork is the first fork based on this parent (c)
				// we need to create another fork for the other chain that starts from our fork base (b1, b2, )
				// and update the blocks building on top of it
				// we don't need to care about this if there are other forks already based on the parent
				//   b2
				//   |
				//   b1  c
				//   | /
				//   a

				if cache.getForkByLeaf(otherChildren[0].Root) != nil {
					cache.indexer.logger.Warnf("fork already exists for leaf %v [%v] (processing %v, scenario 1)", otherChildren[0].Slot, otherChildren[0].Root.String(), block.Slot)
				} else {
					cache.lastForkId++
					otherFork := newFork(cache.lastForkId, parentSlot, parentRoot, otherChildren[0], parentForkId)
					cache.addFork(otherFork)

					_, _, headBlock := cache.updateForkBlocks(otherChildren[0], otherFork.forkId, false)
					otherFork.headBlock = headBlock
					cache.parentIdCache.Add(otherFork.forkId, otherFork.parentFork)
					newForks = append(newForks, otherFork)

					fmt.Fprintf(&logbuf, ", head2(%v): %v [%v]", otherFork.forkId, otherFork.leafSlot, otherFork.leafRoot.String())
				}
			}

			if logbuf.Len() > 0 {
				cache.indexer.logger.Infof("new fork leaf detected (base(%v) %v [%v]%v)", parentForkId, parentSlot, parentRoot.String(), logbuf.String())
			}
		}
	}

	// avoid using forkid 0 for unfinalized blocks, add a new temporary forkid if needed
	if currentForkId == 0 && parentIsFinalized {
		cache.lastForkId++
		fork := newFork(cache.lastForkId, parentSlot, parentRoot, block, parentForkId)
		cache.addFork(fork)
		cache.parentIdCache.Add(fork.forkId, fork.parentFork)
		newForks = append(newForks, fork)

		currentForkId = cache.lastForkId
		cache.indexer.logger.Infof("new fork for canonical chain (base(%v) %v [%v], head(%v) %v [%v])", parentForkId, parentSlot, parentRoot.String(), currentForkId, block.Slot, block.Root.String())
	}

	// check scenario 2
	childBlocks := make([]*Block, 0)
	for _, child := range cache.indexer.blockCache.getBlocksByParentRoot(block.Root) {
		if !child.forkChecked {
			continue
		}

		childBlocks = append(childBlocks, child)
	}

	if len(childBlocks) > 1 {
		// multiple blocks building on top of the current one, create a fork for each
		logbuf := strings.Builder{}
		for idx, child := range childBlocks {
			if cache.getForkByLeaf(child.Root) != nil {
				cache.indexer.logger.Warnf("fork already exists for leaf %v [%v] (processing %v, scenario 2)", child.Slot, child.Root.String(), block.Slot)
			} else {
				cache.lastForkId++
				fork := newFork(cache.lastForkId, block.Slot, block.Root, child, currentForkId)
				cache.addFork(fork)

				_, _, headBlock := cache.updateForkBlocks(child, fork.forkId, false)
				fork.headBlock = headBlock
				cache.parentIdCache.Add(fork.forkId, fork.parentFork)
				newForks = append(newForks, fork)

				fmt.Fprintf(&logbuf, ", head%v: %v [%v]", idx+1, fork.leafSlot, fork.leafRoot.String())
			}
		}

		if logbuf.Len() > 0 {
			cache.indexer.logger.Infof("new child forks detected (base %v [%v]%v)", block.Slot, block.Root.String(), logbuf.String())
		}
	}

	// update fork ids of all blocks building on top of the current block
	_, _, headBlock := cache.updateForkBlocks(block, currentForkId, true)

	// set detected fork id to the block
	block.forkId = currentForkId
	block.forkChecked = true

	// update fork head block if needed
	fork := cache.getForkById(currentForkId)
	if fork != nil {
		lastBlock := block
		if headBlock != nil && headBlock.Slot > lastBlock.Slot {
			lastBlock = headBlock
		}
		if fork.headBlock == nil || lastBlock.Slot > fork.headBlock.Slot {
			fork.headBlock = lastBlock
		}
	}

	_ = newForks

	return nil
}

// updateForkBlocks walks the chain of blocks building on top of startBlock,
// assigning them forkId, and returns the updated block roots and the highest
// block reached (the head of the contiguous segment).
//
// updatedFork is set when a downstream fork's parent must be re-pointed to
// forkId because this segment now sits between it and its old parent.
func (cache *forkCache) updateForkBlocks(startBlock *Block, forkId ForkKey, skipStartBlock bool) (blockRoots []leanapi.Root, updatedFork *Fork, headBlock *Block) {
	blockRoots = []leanapi.Root{}

	if !skipStartBlock {
		blockRoots = append(blockRoots, startBlock.Root)
		startBlock.forkId = forkId
		headBlock = startBlock
	}

	for {
		nextBlocks := cache.indexer.blockCache.getBlocksByParentRoot(startBlock.Root)
		if len(nextBlocks) == 0 {
			break
		}

		if len(nextBlocks) > 1 {
			// potential fork ahead, check if the fork is already processed and has correct parent fork id
			if forks := cache.getForkByBase(startBlock.Root); len(forks) > 0 && forks[0].parentFork != forkId {
				for _, fork := range forks {
					fork.parentFork = forkId
					cache.parentIdCache.Add(fork.forkId, fork.parentFork)
				}

				updatedFork = forks[0]
			}
			break
		}

		nextBlock := nextBlocks[0]
		if !nextBlock.forkChecked {
			break
		}

		if nextBlock.forkId == forkId {
			break
		}

		nextBlock.forkId = forkId
		blockRoots = append(blockRoots, nextBlock.Root)
		headBlock = nextBlock

		startBlock = nextBlock
	}

	return
}
