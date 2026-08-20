package core

import (
	"math"

	"github.com/padovanl/termdock/internal/layout"
)

// layoutPreset names the arrangements Ctrl-B Space cycles through —
// termdock's answer to tmux's next-layout, adapted to a binary
// split-tree instead of tmux's flat pane list: cycling rebuilds the
// active window's tree from scratch in the panes' current
// left-to-right, top-to-bottom order (see layout.Leaves), so it never
// depends on how the split was originally built by hand, only on how
// many panes there are and which order they're in right now.
type layoutPreset int

const (
	layoutTiled layoutPreset = iota
	layoutEvenColumns
	layoutEvenRows
	numLayoutPresets
)

func (lp layoutPreset) String() string {
	switch lp {
	case layoutEvenColumns:
		return "even-columns"
	case layoutEvenRows:
		return "even-rows"
	default:
		return "tiled"
	}
}

// cycleLayout rebuilds the active window's split tree into the next
// preset shape. Only the tree wiring and each split's Ratio change —
// every pane's process is left completely untouched, the same
// process-survives-the-move guarantee breakPaneToNewWindow and
// movePaneToWindow already give at the single-pane level.
func (c *Core) cycleLayout() {
	w := c.win()
	if w.zoomed != nil {
		c.statusMsg = "exit zoom (prefix z) before changing layout"
		return
	}
	leaves := layout.Leaves(w.root)
	if len(leaves) < 2 {
		c.statusMsg = "only one pane in this window"
		return
	}

	refs := make([]leafRef, len(leaves))
	for i, l := range leaves {
		refs[i] = leafRef{l.ID, l.Pane}
	}
	activeID := w.active.ID
	// Every *Node in the old tree is discarded below, so anything else
	// holding one has to be re-resolved by pane ID afterwards — including
	// Ctrl-B ;'s target. Left alone, it kept pointing into the old tree,
	// and pressing ; made a node that belongs to no tree the window's
	// active pane: nothing rendered as focused and the arrow keys had
	// nothing to move from.
	lastActiveID := -1
	if w.lastActive != nil {
		lastActiveID = w.lastActive.ID
	}

	w.layoutPreset = (w.layoutPreset + 1) % numLayoutPresets
	var root *layout.Node
	switch w.layoutPreset {
	case layoutEvenColumns:
		root = buildLeafChain(refs, layout.Vertical)
	case layoutEvenRows:
		root = buildLeafChain(refs, layout.Horizontal)
	default:
		root = buildTiled(refs)
	}

	w.root = root
	w.active = findLeafByID(root, activeID)
	if w.active == nil {
		w.active = layout.FirstLeaf(root)
	}
	w.lastActive = nil
	if lastActiveID >= 0 {
		w.lastActive = findLeafByID(root, lastActiveID)
	}
	c.relayoutLocked()
	c.persistStateLocked()
	c.statusMsg = "layout: " + w.layoutPreset.String()
}

// leafRef is what buildLeafChain/buildTiled need from an existing leaf —
// its pane identity, not the leaf Node itself, since every preset
// discards the old tree and builds fresh Nodes around the same panes.
type leafRef struct {
	id int
	p  layout.PaneHost
}

// buildLeafChain arranges refs as a chain of splits along axis, each
// pane getting an equal share: the first split gives its First child a
// 1/n share and recurses into the remaining n-1 the same way, so by
// induction every leaf ends up exactly 1/n of the total — n equal
// columns (Vertical) or n equal rows (Horizontal).
func buildLeafChain(refs []leafRef, axis layout.SplitType) *layout.Node {
	nodes := make([]*layout.Node, len(refs))
	for i, r := range refs {
		nodes[i] = layout.NewLeaf(r.id, r.p)
	}
	return buildChainNodes(nodes, axis)
}

// buildChainNodes is buildLeafChain generalized to already-built
// subtrees, so buildTiled can chain whole rows together the same way
// buildLeafChain chains individual leaves.
func buildChainNodes(nodes []*layout.Node, axis layout.SplitType) *layout.Node {
	if len(nodes) == 1 {
		return nodes[0]
	}
	first := nodes[0]
	rest := buildChainNodes(nodes[1:], axis)
	n := &layout.Node{Split: axis, Ratio: 1.0 / float64(len(nodes)), First: first, Second: rest}
	first.Parent = n
	rest.Parent = n
	return n
}

// buildTiled arranges refs into a roughly-square grid — tmux's own
// "tiled" layout — by splitting them into ceil(sqrt(n))-wide rows (the
// last row may be shorter) and chaining those rows top to bottom.
func buildTiled(refs []leafRef) *layout.Node {
	n := len(refs)
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	var rows []*layout.Node
	for i := 0; i < n; i += cols {
		end := i + cols
		if end > n {
			end = n
		}
		rows = append(rows, buildLeafChain(refs[i:end], layout.Vertical))
	}
	return buildChainNodes(rows, layout.Horizontal)
}
