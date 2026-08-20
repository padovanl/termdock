package core

import (
	"time"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/layout"
	"termdock/internal/proto"
)

func (c *Core) handleMouse(m proto.ClientMsg) Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.windows) == 0 {
		return Result{} // session mid-shutdown; a lingering connection raced us here
	}
	if c.mode == ModeConfirm || c.mode == ModePicker {
		// A pending "kill this window?" prompt, or the jump picker, only
		// listen for keyboard input — a stray click shouldn't be able to
		// act on whatever's underneath while either is up.
		return Result{}
	}

	buttons := tcell.ButtonMask(m.MouseButtons)
	x, y := m.MouseX, m.MouseY

	if buttons&tcell.WheelUp != 0 {
		c.wheelUp(x, y)
		c.markDirty()
		return Result{}
	}
	if buttons&tcell.WheelDown != 0 {
		c.wheelDown()
		c.markDirty()
		return Result{}
	}

	primary := buttons&tcell.Button1 != 0
	released := buttons == tcell.ButtonNone

	var res Result
	if c.mode == ModeCopy {
		res = c.handleCopyMouse(primary, released, x, y)
	} else {
		c.handleNormalMouse(primary, released, x, y)
	}
	c.markDirty()
	return res
}

func (c *Core) handleNormalMouse(primary, released bool, x, y int) {
	if released {
		if c.tabDrag != nil {
			c.endTabDrag()
			return
		}
		drag := c.drag
		c.drag = nil
		// A divider is also where a "second child" pane's title bar
		// lives (see internal/layout.Compute), so a press there is
		// ambiguous: it might be the start of a resize drag, or just a
		// click on that title bar. Committing to "drag" on every press
		// would make the title unclickable; deciding on press whether
		// it's a click would make the divider undraggable. Waiting
		// until release and checking whether the drag actually moved
		// resolves it without sacrificing either.
		if drag != nil && x == c.dragDownX && y == c.dragDownY {
			c.clickAt(x, y)
		}
		return
	}
	if !primary {
		return
	}
	if c.tabDrag != nil {
		c.updateTabDrag(x)
		return
	}
	if c.drag != nil {
		if c.drag.axis == layout.Vertical {
			layout.SetRatioFromColumn(c.drag.node, x)
		} else {
			layout.SetRatioFromRow(c.drag.node, y)
		}
		c.relayoutLocked()
		return
	}
	// A tab press is deferred the same way a divider press is (see
	// below): whether it ends up a click (select) or a drag (reorder)
	// is only known once it either moves or releases without moving.
	if c.statusRows() > 0 && y == c.rows-1 {
		if wi, ok := c.tabAt(x); ok {
			c.tabDrag = &tabDragState{win: c.windows[wi]}
			return
		}
	}
	if c.win().zoomed == nil {
		if node := layout.HitDivider(c.win().root, x, y); node != nil {
			c.drag = &dragState{node: node, axis: node.Split}
			c.dragDownX, c.dragDownY = x, y
			return
		}
	}
	c.clickAt(x, y)
}

// clickAt handles a plain click (a press with no divider or tab under it,
// or a press-and-release on a divider with no movement in between): a
// pane's title bar — which focuses it, or on a second quick click
// zooms/unzooms it, the same double-click convention a window manager
// uses — or otherwise just gives the clicked pane focus.
func (c *Core) clickAt(x, y int) {
	if leaf := c.leafTitleAt(x, y); leaf != nil {
		if c.registerTitleClick(leaf) {
			c.toggleZoomOn(leaf)
		} else {
			c.setActive(leaf)
		}
		return
	}
	c.focusAt(x, y)
}

// tabAt returns the index of the window tab strip entry column x falls
// under, if any.
func (c *Core) tabAt(x int) (int, bool) {
	for _, t := range c.windowTabs() {
		if x >= t.X && x < t.X+t.W {
			return t.Index, true
		}
	}
	return 0, false
}

// updateTabDrag live-reorders the dragged window to wherever column x
// currently sits among the tab strip — the same feel as dragging a
// browser tab: it snaps into its new slot as soon as you cross the
// midpoint of a neighboring tab, not only on release.
func (c *Core) updateTabDrag(x int) {
	from := c.windowIndex(c.tabDrag.win)
	if from < 0 {
		c.tabDrag = nil
		return
	}
	to := c.tabIndexNear(x)
	if to != from {
		c.moveWindow(from, to)
		c.tabDrag.moved = true
	}
}

// tabIndexNear returns the tab strip index whose midpoint x has just
// crossed, clamped to the first/last tab if x is beyond either end —
// used both to resolve a live drag and, implicitly, its final resting
// place on release.
func (c *Core) tabIndexNear(x int) int {
	tabs := c.windowTabs()
	for _, t := range tabs {
		if x < t.X+t.W/2 {
			return t.Index
		}
	}
	return len(tabs) - 1
}

// endTabDrag resolves a tab press on release: if it never actually moved
// to a different slot, it's read as a plain click (select that window);
// otherwise the reorder that already happened live during the drag is
// left as-is.
func (c *Core) endTabDrag() {
	drag := c.tabDrag
	c.tabDrag = nil
	if drag == nil || drag.moved {
		return
	}
	if i := c.windowIndex(drag.win); i >= 0 {
		c.selectWindowIndex(i)
	}
}

// leafTitleAt returns the leaf whose title-bar row (its border's top
// edge, one row above its content — see internal/layout.Node.Rect) x,y
// falls on, or nil.
func (c *Core) leafTitleAt(x, y int) *layout.Node {
	w := c.win()
	leaves := layout.Leaves(w.root)
	if w.zoomed != nil {
		leaves = []*layout.Node{w.zoomed}
	}
	for _, l := range leaves {
		r := l.Rect
		if y == r.Y-1 && x >= r.X-1 && x <= r.X+r.W {
			return l
		}
	}
	return nil
}

// registerTitleClick reports whether this title-bar click on n is the
// second half of a double-click (same pane, within doubleClickWindow of
// the last one) — and, either way, records it as "the last click" for the
// next call to compare against.
func (c *Core) registerTitleClick(n *layout.Node) bool {
	now := time.Now()
	isDouble := c.lastTitleClickID == n.ID && now.Sub(c.lastTitleClickAt) < doubleClickWindow
	if isDouble {
		// Consumed: a third click right after starts a fresh pair
		// rather than being read as yet another double-click.
		c.lastTitleClickID = 0
		c.lastTitleClickAt = time.Time{}
	} else {
		c.lastTitleClickID = n.ID
		c.lastTitleClickAt = now
	}
	return isDouble
}

func (c *Core) handleCopyMouse(primary, released bool, x, y int) Result {
	leaf := findLeafByID(c.win().root, c.copy.paneID)
	if leaf == nil {
		c.exitCopyMode()
		return Result{}
	}

	if released {
		if c.mouseDown {
			c.mouseDown = false
			moved := x != c.mouseDownX || y != c.mouseDownY
			if moved {
				text, ok := c.yank()
				return Result{Clipboard: text, HasClipboard: ok}
			}
			c.copy.selecting = false
		}
		return Result{}
	}
	if !primary {
		return Result{}
	}

	cr := leaf.Rect
	cols, rows, total, ok := c.copyPaneDims()
	if !ok {
		return Result{}
	}
	localX := clampi(x-cr.X, 0, maxi(0, cols-1))
	localY := clampi(y-cr.Y, 0, maxi(0, rows-1))
	absY := clampi(c.copy.top+localY, 0, maxi(0, total-1))

	if !c.mouseDown {
		c.mouseDown = true
		c.mouseDownX, c.mouseDownY = x, y
		c.copy.selecting = true
		c.copy.anchorX, c.copy.anchorY = localX, absY
	}
	c.copy.curX, c.copy.curY = localX, absY
	c.clampTop(rows, total)
	return Result{}
}

func (c *Core) wheelUp(x, y int) {
	leaf := c.leafAt(x, y)
	if leaf == nil {
		return
	}
	if c.mode != ModeCopy || c.copy.paneID != leaf.ID {
		p, ok := c.panes[leaf.ID]
		if !ok {
			return
		}
		t := p.Term()
		t.Lock()
		hl := t.HistoryLen()
		t.Unlock()
		if hl == 0 {
			return
		}
		c.setActive(leaf)
		c.enterCopyMode()
	}
	c.scrollView(-3)
}

func (c *Core) wheelDown() {
	if c.mode != ModeCopy {
		return
	}
	if c.scrollView(3) {
		c.exitCopyMode()
	}
}
