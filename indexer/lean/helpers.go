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
// of block[i] equals block[i+1].parent_root. The final block's root is
// resolved via a header lookup keyed by slot. Unresolved roots fall back to the
// state root so the row still has a unique-ish key (best effort for backfill;
// the SSE path supplies authoritative roots near head).
func (idx *Indexer) resolveBackfillRoots(blocks []*leanapi.Block) []leanapi.Root {
	roots := make([]leanapi.Root, len(blocks))
	for i := 0; i+1 < len(blocks); i++ {
		roots[i] = blocks[i+1].ParentRoot
	}
	if n := len(blocks); n > 0 {
		// Resolve the last block's root from fork choice if it is present there;
		// otherwise fall back to its state root.
		last := blocks[n-1]
		roots[n-1] = last.StateRoot
		if fc, err := idx.client.GetForkChoice(idx.ctx); err == nil {
			for _, node := range fc.Nodes {
				if uint64(node.Slot) == uint64(last.Slot) {
					roots[n-1] = node.Root
					break
				}
			}
		}
	}
	return roots
}
