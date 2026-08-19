// Package layout implements the binary split-tree that arranges panes on
// screen, mirroring how tmux/screen lay out a window's panes.
package layout

// SplitType identifies how a node's two children are arranged.
type SplitType int

const (
	// NoSplit marks a leaf node (holds a pane, no children).
	NoSplit SplitType = iota
	// Vertical arranges children side by side (left/right), separated by
	// a single-column divider.
	Vertical
	// Horizontal stacks children top/bottom. No extra divider row is
	// reserved: the lower pane's title bar acts as the separator.
	Horizontal
)

// MinWidth/MinHeight are the smallest content area a pane may shrink to.
// Splits that would violate this are rejected.
const (
	MinWidth  = 4
	MinHeight = 2
)

// Rect is an axis-aligned screen region in terminal cells.
type Rect struct {
	X, Y, W, H int
}

// PaneHost is the minimal set of operations layout needs to perform on the
// thing living inside a leaf node. Kept as an interface so this package
// doesn't depend on the pty/vt10x plumbing.
type PaneHost interface {
	Resize(cols, rows int)
}

// Node is either a leaf (Pane != nil) or a split (First/Second != nil).
type Node struct {
	Parent *Node

	Pane PaneHost // non-nil for leaves
	ID   int      // pane id, meaningful for leaves only

	Split    SplitType
	Ratio    float64 // size fraction given to First
	First    *Node
	Second   *Node
	DividerX int // absolute column of the vertical divider (Vertical splits only)

	Rect Rect
}

// NewLeaf creates a standalone leaf node.
func NewLeaf(id int, pane PaneHost) *Node {
	return &Node{ID: id, Pane: pane}
}

// IsLeaf reports whether n holds a pane directly.
func (n *Node) IsLeaf() bool {
	return n.Split == NoSplit
}

// ContentRect returns the area available for the pane's terminal content,
// i.e. Rect minus the one-row title bar every leaf normally reserves at
// its top. When the pane is too short to spare a row for the title
// (Rect.H < 2), the whole rect goes to content instead and no title bar
// is drawn — the same spirit as tmux dropping pane borders/status lines
// it has no room for rather than corrupting the layout.
func (n *Node) ContentRect() Rect {
	if n.Rect.H < 2 {
		return n.Rect
	}
	r := n.Rect
	r.Y++
	r.H--
	return r
}

// Split turns leaf n into a split node with two new leaf children: the
// existing pane goes to First, newPane becomes Second. Returns the new leaf
// so the caller can make it active. Returns false if there isn't enough
// room left to split.
func Split(n *Node, st SplitType, newID int, newPane PaneHost) (*Node, bool) {
	if !n.IsLeaf() {
		return nil, false
	}
	if st == Vertical && n.Rect.W < MinWidth*2+1 {
		return nil, false
	}
	if st == Horizontal && n.Rect.H < MinHeight*2+1 {
		return nil, false
	}

	firstLeaf := NewLeaf(n.ID, n.Pane)
	secondLeaf := NewLeaf(newID, newPane)
	firstLeaf.Parent = n
	secondLeaf.Parent = n

	n.Pane = nil
	n.ID = 0
	n.Split = st
	n.Ratio = 0.5
	n.First = firstLeaf
	n.Second = secondLeaf

	return secondLeaf, true
}

// Remove detaches leaf n from the tree, promoting its sibling into the
// place of its former parent. Returns the new tree root (which changes only
// when n's parent was the root) and the leaf that should receive focus
// next, or nil if n was the last remaining pane.
func Remove(root, n *Node) (newRoot, nextFocus *Node) {
	parent := n.Parent
	if parent == nil {
		// n was the whole tree; nothing left.
		return nil, nil
	}

	var sibling *Node
	if parent.First == n {
		sibling = parent.Second
	} else {
		sibling = parent.First
	}

	// Splice sibling into parent's slot, keeping parent's own place in
	// the tree (its grandparent link) rather than sibling's stale one.
	grandparent := parent.Parent
	*parent = *sibling
	parent.Parent = grandparent
	if parent.First != nil {
		parent.First.Parent = parent
	}
	if parent.Second != nil {
		parent.Second.Parent = parent
	}

	next := FirstLeaf(root)
	return root, next
}

// FirstLeaf returns the leftmost/topmost leaf under n.
func FirstLeaf(n *Node) *Node {
	for !n.IsLeaf() {
		n = n.First
	}
	return n
}

// Leaves returns every leaf under n, in left-to-right / top-to-bottom order.
func Leaves(n *Node) []*Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return []*Node{n}
	}
	return append(Leaves(n.First), Leaves(n.Second)...)
}

// Compute assigns Rect (and DividerX) to every node in the tree rooted at n,
// recursively, and resizes every leaf's pane to match its new content area.
func Compute(n *Node, r Rect) {
	n.Rect = r
	if n.IsLeaf() {
		cr := n.ContentRect()
		if cr.W > 0 && cr.H > 0 && n.Pane != nil {
			n.Pane.Resize(cr.W, cr.H)
		}
		return
	}

	// Note: MinWidth/MinHeight are deliberately NOT enforced here. They
	// gate creating a *new* split (see Split, below) so users can't split
	// a pane down to something unusably small — but an *existing* tree
	// must still reflow when the terminal itself shrinks below that, the
	// same way tmux keeps shrinking panes rather than refusing to
	// redraw. Below, everything is clamped to [0, avail] so a tiny
	// terminal degrades to zero-sized (hidden) panes instead of
	// producing a negative size, which would panic downstream.
	switch n.Split {
	case Vertical:
		avail := maxInt(r.W-1, 0)
		firstW := clampInt(int(float64(avail)*n.Ratio), 0, avail)
		secondW := avail - firstW
		n.DividerX = r.X + firstW
		Compute(n.First, Rect{r.X, r.Y, firstW, r.H})
		Compute(n.Second, Rect{r.X + firstW + 1, r.Y, secondW, r.H})

	case Horizontal:
		avail := maxInt(r.H, 0)
		firstH := clampInt(int(float64(avail)*n.Ratio), 0, avail)
		secondH := avail - firstH
		Compute(n.First, Rect{r.X, r.Y, r.W, firstH})
		Compute(n.Second, Rect{r.X, r.Y + firstH, r.W, secondH})
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Resize nudges the divider nearest to leaf n along the given axis by delta
// cells (positive = move the divider right/down, growing the First child).
// It walks up from n to find the closest ancestor split of that axis. Used
// to implement interactive pane resizing (resize-mode, divider dragging).
func Resize(n *Node, axis SplitType, delta int) {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Split != axis {
			continue
		}
		var size int
		if axis == Vertical {
			size = p.Rect.W - 1
		} else {
			size = p.Rect.H
		}
		if size <= 0 {
			return
		}
		cur := int(p.Ratio * float64(size))
		next := clampInt(cur+delta, MinWidth, size-MinWidth)
		if axis == Horizontal {
			next = clampInt(cur+delta, MinHeight, size-MinHeight)
		}
		p.Ratio = float64(next) / float64(size)
		return
	}
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// HitDivider returns the vertical-split node whose divider line passes
// through screen column x at row y, or nil. Used for mouse-drag resizing:
// the caller sets the returned node's Ratio directly from the drag
// position.
func HitDivider(n *Node, x, y int) *Node {
	if n == nil || n.IsLeaf() {
		return nil
	}
	if n.Split == Vertical && x == n.DividerX && y >= n.Rect.Y && y < n.Rect.Y+n.Rect.H {
		return n
	}
	if h := HitDivider(n.First, x, y); h != nil {
		return h
	}
	return HitDivider(n.Second, x, y)
}

// SetRatioFromColumn sets a Vertical split node's Ratio so its divider
// lands as close as possible to absolute screen column x.
func SetRatioFromColumn(n *Node, x int) {
	size := n.Rect.W - 1
	if size <= 0 {
		return
	}
	next := clampInt(x-n.Rect.X, MinWidth, size-MinWidth)
	n.Ratio = float64(next) / float64(size)
}

// VerticalDividers returns the (x, yStart, yEnd) runs of every vertical
// divider in the tree, for drawing.
func VerticalDividers(n *Node) [][3]int {
	if n == nil || n.IsLeaf() {
		return nil
	}
	var out [][3]int
	if n.Split == Vertical {
		out = append(out, [3]int{n.DividerX, n.Rect.Y, n.Rect.Y + n.Rect.H - 1})
	}
	out = append(out, VerticalDividers(n.First)...)
	out = append(out, VerticalDividers(n.Second)...)
	return out
}
