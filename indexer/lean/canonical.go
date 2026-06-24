package lean

import (
	leanapi "github.com/ethpandaops/dora/clients/consensus/lean"
)

// canonical.go implements canonical head selection for lean consensus.
//
// Dora's beacon canonical.go aggregates attestation votes over epochs to pick
// the heaviest fork head. Lean is 3SF-mini, so this is a pure LMD-GHOST:
//   - one vote per validator (no balances; every validator contributes weight 1),
//   - the latest (highest-slot) head vote per validator is the one that counts,
//   - subtree weights are accumulated by walking each vote's head root up to the
//     finalized anchor,
//   - the head is found by greedily descending into the heaviest child from the
//     finalized anchor.
//
// We compute the head locally from cached attestations. The lean node also
// serves an authoritative head via GET /lean/v0/fork_choice.
//
// TODO(cross-check): reconcile computed head against /lean/v0/fork_choice head
// (the node is authoritative; the wiring task should add a sanity assertion or
// divergence metric here).

// latestVote is a validator's most recent head vote.
type latestVote struct {
	slot     leanapi.Slot
	headRoot leanapi.Root
}

// ReorgInfo describes a detected chain reorganization. Dora only logs reorgs;
// we return a struct so the wiring task can persist or emit it.
type ReorgInfo struct {
	// Depth is the rewind distance: number of blocks dropped from the old head
	// down to the common ancestor.
	Depth uint64
	// ForwardDistance is the number of blocks from the common ancestor up to
	// the new head.
	ForwardDistance uint64
	OldHead         leanapi.Root
	NewHead         leanapi.Root
	CommonAncestor  leanapi.Root
}

// collectLatestVotes scans all cached block bodies and returns, per validator,
// its latest head vote (highest attestation slot wins). The aggregation bitlist
// decode reuses setBits (the same helper the votes-table path uses), so the
// SSZ bitlist sentinel handling stays in one place.
func (idx *Indexer) collectLatestVotes() map[uint64]latestVote {
	votes := map[uint64]latestVote{}

	// Snapshot bodies under the cache lock so disposal cannot race the reads.
	for _, body := range idx.blockCache.snapshotBodies() {
		for _, att := range body.Body.Attestations {
			headRoot := att.Data.Head.Root
			attSlot := att.Data.Slot

			for _, vi := range setBits(att.AggregationBits) {
				if prev, ok := votes[vi]; ok && prev.slot >= attSlot {
					continue
				}
				votes[vi] = latestVote{slot: attSlot, headRoot: headRoot}
			}
		}
	}

	return votes
}

// computeSubtreeWeights converts the latest votes into per-block subtree
// weights: each vote adds weight 1 to its head block and every ancestor up to
// (and including) the finalized anchor. A block's weight is therefore the
// number of latest votes landing on it or any of its descendants.
func (idx *Indexer) computeSubtreeWeights(votes map[uint64]latestVote) map[leanapi.Root]uint64 {
	weights := map[leanapi.Root]uint64{}

	for _, vote := range votes {
		root := vote.headRoot
		for {
			block := idx.blockCache.getBlockByRoot(root)
			if block == nil {
				break
			}

			weights[root]++

			parentRoot := block.GetParentRoot()
			if parentRoot.IsZero() || parentRoot == root {
				break
			}
			root = parentRoot
		}
	}

	return weights
}

// findHead descends from the anchor root, at each step picking the child with
// the greatest subtree weight. Ties break by higher slot, then by
// lexicographically-greater root, matching Dora's deterministic-tiebreak
// intent. It stops at a block with no children: the canonical head.
func (idx *Indexer) findHead(anchor leanapi.Root, weights map[leanapi.Root]uint64) leanapi.Root {
	head := anchor
	// visited guards against cycles / self-parent edges (e.g. the zero-root
	// genesis anchor filed as its own child); without it findHead can loop
	// forever holding canonicalHeadMutex and wedge the indexer.
	visited := map[leanapi.Root]bool{head: true}

	for {
		children := idx.blockCache.getBlocksByParentRoot(head)
		if len(children) == 0 {
			break
		}

		var best *Block
		var bestWeight uint64
		for _, child := range children {
			if child.Root == head || visited[child.Root] {
				// self-parent or already-visited edge: not a real descendant.
				continue
			}
			w := weights[child.Root]
			if best == nil || isBetterChild(child, w, best, bestWeight) {
				best = child
				bestWeight = w
			}
		}

		if best == nil || best.Root == head {
			break
		}
		head = best.Root
		visited[head] = true
	}

	return head
}

// isBetterChild reports whether candidate (with weight cw) should beat the
// current best (with weight bw): greater weight wins; ties go to the higher
// slot; further ties go to the lexicographically-greater root.
func isBetterChild(candidate *Block, cw uint64, best *Block, bw uint64) bool {
	if cw != bw {
		return cw > bw
	}
	if candidate.Slot != best.Slot {
		return candidate.Slot > best.Slot
	}
	return rootGreater(candidate.Root, best.Root)
}

// rootGreater reports whether root a is lexicographically greater than b.
func rootGreater(a, b leanapi.Root) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// computeCanonicalChain runs LMD-GHOST over the cached attestations and returns
// the canonical head, the set of canonical roots (head back to the finalized
// anchor), and whether the head changed since the last computation.
//
// It short-circuits when the block cache has not changed since the last call
// (tracked by the latest-block marker, mirroring Dora's canonicalComputation),
// returning the cached result with changed=false.
//
// No DB writes happen here: the canonical set is returned so the wiring task
// can persist canonical/orphaned diffs.
//
// TODO(cross-check): reconcile the returned headRoot against the node's
// authoritative GET /lean/v0/fork_choice head.
func (idx *Indexer) computeCanonicalChain() (headRoot leanapi.Root, canonical map[leanapi.Root]bool, changed bool) {
	idx.canonicalHeadMutex.Lock()
	defer idx.canonicalHeadMutex.Unlock()

	// Short-circuit on the cache version: it advances on every mutation that can
	// affect selection (new node, body attach, parent edge, removal), so unlike
	// the old latestBlock-root marker it also catches vote-only changes (a body
	// attached to an existing node) and back-to-back compute calls with new
	// blocks in between.
	version := idx.blockCache.getVersion()
	if idx.canonicalComputed && version == idx.canonicalComputedVersion {
		return idx.canonicalHead, idx.canonicalSet(idx.canonicalHead), false
	}

	_, finalizedRoot := idx.finalizedCheckpoint()

	// Anchor on the finalized root when it is cached; otherwise fall back to the
	// lowest cached block on the latest block's chain so we still produce a head
	// before finalized seeding is wired.
	anchor := finalizedRoot
	if idx.blockCache.getBlockByRoot(anchor) == nil {
		anchor = idx.fallbackAnchor()
	}

	votes := idx.collectLatestVotes()
	weights := idx.computeSubtreeWeights(votes)
	headRoot = idx.findHead(anchor, weights)

	canonical = idx.canonicalSet(headRoot)

	changed = !idx.canonicalComputed || headRoot != idx.canonicalHead
	idx.canonicalHead = headRoot
	idx.canonicalComputed = true
	idx.canonicalComputedVersion = version

	return headRoot, canonical, changed
}

// canonicalSet returns the set of roots on the canonical chain from headRoot
// down to (and including) the finalized anchor / genesis.
func (idx *Indexer) canonicalSet(headRoot leanapi.Root) map[leanapi.Root]bool {
	canonical := map[leanapi.Root]bool{}
	root := headRoot
	for {
		block := idx.blockCache.getBlockByRoot(root)
		if block == nil {
			break
		}
		canonical[root] = true

		parentRoot := block.GetParentRoot()
		if parentRoot.IsZero() || parentRoot == root {
			break
		}
		root = parentRoot
	}
	return canonical
}

// fallbackAnchor returns an anchor root to use before finalized seeding is
// wired: the lowest-slot ancestor reachable from the latest cached block.
func (idx *Indexer) fallbackAnchor() leanapi.Root {
	latest := idx.blockCache.getLatestBlock()
	if latest == nil {
		return leanapi.Root{}
	}

	root := latest.Root
	for {
		block := idx.blockCache.getBlockByRoot(root)
		if block == nil {
			return root
		}
		parentRoot := block.GetParentRoot()
		if parentRoot.IsZero() || parentRoot == root || idx.blockCache.getBlockByRoot(parentRoot) == nil {
			return root
		}
		root = parentRoot
	}
}

// processReorg computes the reorg between an old and new head. It walks the old
// head's chain back until it meets the new head's canonical chain (the common
// ancestor), measuring the rewind distance (blocks dropped) and forward
// distance (blocks added). It returns nil for a pure fast-forward or pure
// rewind (no actual fork divergence).
func (idx *Indexer) processReorg(oldHead, newHead *Block) *ReorgInfo {
	if oldHead == nil || newHead == nil {
		return nil
	}

	reorgBase := oldHead
	forwardDistance := uint64(0)
	rewindDistance := uint64(0)
	var commonAncestor leanapi.Root

	for {
		res, dist, uncached := idx.blockCache.getCanonicalDistanceEx(reorgBase.Root, newHead.Root, 0)
		if res {
			forwardDistance = dist
			commonAncestor = reorgBase.Root
			break
		}
		if uncached {
			// The common ancestor lies below what the cache holds (e.g. across
			// the finalized boundary). We cannot measure the depth, but a reorg
			// almost certainly happened: the old head is not provably on the new
			// head's chain. Report it as unknown-depth rather than silently nil.
			idx.logger.Warnf("possible reorg with unknown depth: common ancestor not cached (old: %v, new: %v)", oldHead.Root.String(), newHead.Root.String())
			return &ReorgInfo{
				Depth:           0,
				ForwardDistance: 0,
				OldHead:         oldHead.Root,
				NewHead:         newHead.Root,
				CommonAncestor:  leanapi.Root{},
			}
		}

		parentRoot := reorgBase.GetParentRoot()
		if parentRoot.IsZero() || parentRoot == reorgBase.Root {
			return nil
		}

		reorgBase = idx.blockCache.getBlockByRoot(parentRoot)
		if reorgBase == nil {
			// Walked off the cached chain without resolving: unknown-depth reorg.
			idx.logger.Warnf("possible reorg with unknown depth: ancestor chain not cached (old: %v, new: %v)", oldHead.Root.String(), newHead.Root.String())
			return &ReorgInfo{
				Depth:          0,
				OldHead:        oldHead.Root,
				NewHead:        newHead.Root,
				CommonAncestor: leanapi.Root{},
			}
		}

		rewindDistance++
	}

	if rewindDistance == 0 {
		// pure fast-forward: new head builds directly on old head's chain
		idx.logger.Debugf("chain fast forward! +%v slots (old: %v, new: %v)", forwardDistance, oldHead.Root.String(), newHead.Root.String())
		return nil
	}
	if forwardDistance == 0 {
		// pure rewind: old head builds on the new head's chain
		idx.logger.Debugf("chain rewind! -%v slots (old: %v, new: %v)", rewindDistance, oldHead.Root.String(), newHead.Root.String())
		return nil
	}

	idx.logger.Infof("chain reorg! depth: -%v / +%v (old: %v, new: %v)", rewindDistance, forwardDistance, oldHead.Root.String(), newHead.Root.String())

	return &ReorgInfo{
		Depth:           rewindDistance,
		ForwardDistance: forwardDistance,
		OldHead:         oldHead.Root,
		NewHead:         newHead.Root,
		CommonAncestor:  commonAncestor,
	}
}
