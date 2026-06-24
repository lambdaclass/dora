package lean

import (
	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// setBits returns the validator indices whose bit is set in an SSZ bitlist.
//
// The lean aggregation_bits field is an SSZ Bitlist: bits are little-endian
// within each byte and the highest set bit is a length-delimiter sentinel that
// is NOT a validator. We therefore drop the most-significant set bit overall.
func setBits(bits leanapi.HexBytes) []uint64 {
	if len(bits) == 0 {
		return nil
	}
	// Find the sentinel: the highest set bit across the whole bitlist.
	sentinel := -1
	for byteIdx := len(bits) - 1; byteIdx >= 0 && sentinel < 0; byteIdx-- {
		b := bits[byteIdx]
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				sentinel = byteIdx*8 + bit
				break
			}
		}
	}
	if sentinel < 0 {
		return nil
	}
	out := make([]uint64, 0, sentinel)
	for i := 0; i < sentinel; i++ {
		if bits[i/8]&(1<<uint(i%8)) != 0 {
			out = append(out, uint64(i))
		}
	}
	return out
}

// resolveBackfillRoots derives a block root for each block returned by the
// range endpoint (which omits roots). Blocks arrive slot-ordered, so the root
// of block[i] equals block[i+1].parent_root (authoritative). The final block
// has no child to borrow a parent_root from, so it is resolved from fork choice
// by slot if present.
//
// It returns a parallel resolved[] flag. The last block's root is resolved=true
// only when fork choice supplies it; otherwise resolved[i]=false and the root
// falls back to the state root as a best-effort UNIQUE key. Callers MUST NOT
// place an unresolved (resolved=false) block into the in-memory cache: a
// state-root key would not match its real block root, breaking parent linkage
// and letting the SSE path create a duplicate node. Unresolved blocks below the
// finalized slot may still be written to the DB finalized tier (best effort).
func (idx *Indexer) resolveBackfillRoots(blocks []*leanapi.Block) (roots []leanapi.Root, resolved []bool) {
	roots = make([]leanapi.Root, len(blocks))
	resolved = make([]bool, len(blocks))
	for i := 0; i+1 < len(blocks); i++ {
		roots[i] = blocks[i+1].ParentRoot
		resolved[i] = true
	}
	if n := len(blocks); n > 0 {
		last := blocks[n-1]
		roots[n-1] = last.StateRoot // best-effort fallback key (not the real root)
		if fc, err := idx.client.GetForkChoice(idx.ctx); err == nil {
			for _, node := range fc.Nodes {
				if uint64(node.Slot) == uint64(last.Slot) {
					roots[n-1] = node.Root
					resolved[n-1] = true
					break
				}
			}
		}
	}
	return roots, resolved
}
