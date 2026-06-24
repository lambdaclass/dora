package lean

import (
	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// ForkKey is a unique identifier for a fork in the lean chain.
type ForkKey uint64

// Fork represents a fork in the lean chain: a chain segment rooted at a base
// block (the divergence point) and extending to a leaf/head block.
//
// This is a faithful, domain-agnostic port of Dora's beacon Fork, retyped to
// lean Root/Slot. DB (de)serialization is intentionally omitted; persistence is
// handled by a later task.
type Fork struct {
	forkId     ForkKey      // Unique identifier for the fork.
	baseSlot   leanapi.Slot // Slot of the base block.
	baseRoot   leanapi.Root // Root of the base block.
	leafSlot   leanapi.Slot // Slot of the leaf block.
	leafRoot   leanapi.Root // Root of the leaf block.
	parentFork ForkKey      // Parent fork.

	headBlock *Block // Block at the head of the fork (when known).
}

// newFork creates a new Fork instance.
func newFork(forkId ForkKey, baseSlot leanapi.Slot, baseRoot leanapi.Root, leafBlock *Block, parentFork ForkKey) *Fork {
	return &Fork{
		forkId:     forkId,
		baseSlot:   baseSlot,
		baseRoot:   baseRoot,
		leafSlot:   leafBlock.Slot,
		leafRoot:   leafBlock.Root,
		parentFork: parentFork,
		headBlock:  leafBlock,
	}
}

// GetBase returns the base slot and root of the fork.
func (fork *Fork) GetBase() (leanapi.Slot, leanapi.Root) {
	return fork.baseSlot, fork.baseRoot
}

// GetLeaf returns the leaf slot and root of the fork.
func (fork *Fork) GetLeaf() (leanapi.Slot, leanapi.Root) {
	return fork.leafSlot, fork.leafRoot
}

// GetParent returns the parent fork id.
func (fork *Fork) GetParent() ForkKey {
	return fork.parentFork
}
