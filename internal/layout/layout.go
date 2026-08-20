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
	// Horizontal stacks children top/bottom, separated by a single-row
	// divider (symmetric with Vertical).
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
	DividerX int // absolute column of the divider (Vertical splits only)
	DividerY int // absolute row of the divider (Horizontal splits only)

	// Rect is the pane's content area. A one-cell border is drawn around
	// every leaf by the client (see internal/client/render.go), entirely
	// outside Rect: the outermost border comes from the margin Compute's
	// caller reserves around the whole tree, and internal borders come
	// from the single row/column each split reserves between its two
	// children. Because that margin/gap is symmetric on every side, a
	// leaf's Rect is always exactly its terminal content — no title-row
	// bookkeeping needed here.
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

// Compute assigns Rect (and DividerX/DividerY) to every node in the tree
// rooted at n, recursively, and resizes every leaf's pane to match its new
// content area. The caller is expected to have already reserved a one-cell
// margin around r for the outer border, if any.
func Compute(n *Node, r Rect) {
	n.Rect = r
	if n.IsLeaf() {
		if r.W > 0 && r.H > 0 && n.Pane != nil {
			n.Pane.Resize(r.W, r.H)
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
		avail := maxInt(r.H-1, 0)
		firstH := clampInt(int(float64(avail)*n.Ratio), 0, avail)
		secondH := avail - firstH
		n.DividerY = r.Y + firstH
		Compute(n.First, Rect{r.X, r.Y, r.W, firstH})
		Compute(n.Second, Rect{r.X, r.Y + firstH + 1, r.W, secondH})
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
			size = p.Rect.H - 1
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

// HitDivider returns the split node whose divider line passes through
// screen column x at row y, or nil. Used for mouse-drag resizing: the
// caller checks the returned node's Split to know which axis it's
// dragging, and sets its Ratio directly from the drag position (via
// SetRatioFromColumn/SetRatioFromRow).
func HitDivider(n *Node, x, y int) *Node {
	if n == nil || n.IsLeaf() {
		return nil
	}
	if n.Split == Vertical && x == n.DividerX && y >= n.Rect.Y && y < n.Rect.Y+n.Rect.H {
		return n
	}
	if n.Split == Horizontal && y == n.DividerY && x >= n.Rect.X && x < n.Rect.X+n.Rect.W {
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

// SetRatioFromRow sets a Horizontal split node's Ratio so its divider
// lands as close as possible to absolute screen row y.
func SetRatioFromRow(n *Node, y int) {
	size := n.Rect.H - 1
	if size <= 0 {
		return
	}
	next := clampInt(y-n.Rect.Y, MinHeight, size-MinHeight)
	n.Ratio = float64(next) / float64(size)
}
