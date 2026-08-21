package core

import (
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

func (c *Core) handleMouse(m proto.ClientMsg) Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.windows) == 0 {
		return Result{} // session mid-shutdown; a lingering connection raced us here
	}
	buttons := tcell.ButtonMask(m.MouseButtons)
	x, y := m.MouseX, m.MouseY

	switch c.mode {
	case ModeConfirm, ModePicker, ModeRegisters, ModeSessions, ModeSearch, ModeOpener, ModeQuickJump:
		// These are keyboard-only type-ahead/prompt modes — a stray
		// click shouldn't be able to act on whatever's underneath while
		// any of them are up.
		return Result{}
	case ModeHelp, ModeSettings:
		// Same rule for clicks, but both of these are long scrollable
		// lists and the wheel is the first thing anyone reaches for on
		// one — especially on a short terminal, which is exactly where
		// the list doesn't fit and scrolling matters most.
		delta := 0
		switch {
		case buttons&tcell.WheelUp != 0:
			delta = -3
		case buttons&tcell.WheelDown != 0:
			delta = 3
		default:
			return Result{}
		}
		if c.mode == ModeHelp {
			c.scrollHelp(delta)
		} else {
			c.scrollSettings(delta)
		}
		c.markDirty()
		return Result{}
	}

	// Wheel-scrolling only makes sense over the normal pane layout or
	// while already scrolled back in copy-mode; with the popup or
	// overview covering the screen, there's no window underneath for it
	// to sensibly apply to.
	if (buttons&tcell.WheelUp != 0 || buttons&tcell.WheelDown != 0) && (c.mode == ModeNormal || c.mode == ModeCopy) {
		if buttons&tcell.WheelUp != 0 {
			c.wheelUp(x, y)
		} else {
			c.wheelDown()
		}
		c.markDirty()
		return Result{}
	}

	primary := buttons&tcell.Button1 != 0
	released := buttons == tcell.ButtonNone

	var res Result
	switch c.mode {
	case ModeCopy:
		res = c.handleCopyMouse(primary, released, x, y)
	case ModeOverview:
		c.handleOverviewMouse(primary, released, x, y)
	case ModePopup:
		c.handlePopupMouse(primary, released, x, y)
	default:
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
		if c.titleDrag != nil {
			c.endTitleDrag(x, y)
			return
		}
		if c.contentPress != nil {
			// Never escalated into a text-selection drag (see below) —
			// a plain click, so give that pane focus same as always.
			cp := c.contentPress
			c.contentPress = nil
			c.setActive(cp.leaf)
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
	if c.titleDrag != nil {
		return // no live feedback while dragging a pane's title (yet); resolved entirely on release
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
	if c.contentPress != nil {
		if x != c.contentPress.x || y != c.contentPress.y {
			c.startContentDragSelect(x, y)
		}
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
	if leaf := c.leafTitleAt(x, y); leaf != nil {
		c.clickTitle(leaf)
		c.titleDrag = &titleDragState{leaf: leaf, win: c.win()}
		c.dragDownX, c.dragDownY = x, y
		return
	}
	// A press on ordinary pane content is deferred exactly like a
	// divider or tab press: it might be a plain click (focus, handled
	// on release above) or the start of a text-selection drag — see
	// startContentDragSelect, triggered by the *next* event once it
	// shows real movement.
	if leaf := c.leafAt(x, y); leaf != nil {
		c.contentPress = &contentPressState{leaf: leaf, x: x, y: y}
		return
	}
	c.focusAt(x, y)
}

// startContentDragSelect turns a content-area press that's just started
// moving into a copy-mode text selection anchored at the *original*
// press point (cp.x/cp.y), with the cursor already tracking the current
// drag position (x,y) rather than sitting on top of the anchor until the
// next move event — a single quick drag (press, one move, release) is
// the common case for a deliberate selection, not a rare edge case, and
// leaving the cursor pinned to the anchor until "the next event" would
// make that common case select nothing. Matches how any ordinary
// terminal lets you click-drag to select without first having to
// explicitly enter a "selection mode." From here, c.mode is ModeCopy, so
// every further event for this same gesture (more moves, then the
// release) is naturally picked up by handleCopyMouse instead — this only
// handles the transition itself.
func (c *Core) startContentDragSelect(x, y int) {
	cp := c.contentPress
	c.contentPress = nil
	c.setActive(cp.leaf)
	c.enterCopyMode() // sets c.copy.top to the live bottom of the buffer
	cr := cp.leaf.Rect
	cols, rows, total, ok := c.copyPaneDims()
	if !ok {
		c.exitCopyMode()
		return
	}
	toAbs := func(px, py int) (int, int) {
		localX := clampi(px-cr.X, 0, maxi(0, cols-1))
		localY := clampi(py-cr.Y, 0, maxi(0, rows-1))
		return localX, clampi(c.copy.top+localY, 0, maxi(0, total-1))
	}
	c.copy.selecting = true
	c.copy.anchorX, c.copy.anchorY = toAbs(cp.x, cp.y)
	c.copy.curX, c.copy.curY = toAbs(x, y)
	c.mouseDown = true
	c.mouseDownX, c.mouseDownY = cp.x, cp.y
}

// clickAt handles a divider press-and-release with no movement in
// between: the one row/column where a title-bar click (see clickTitle)
// couldn't be resolved immediately on press, because that exact spot was
// also a divider and might instead have been the start of a resize drag
// (see handleNormalMouse). Everywhere else a title click resolves
// straight away; this is just that one edge case's release-time fallback.
func (c *Core) clickAt(x, y int) {
	if leaf := c.leafTitleAt(x, y); leaf != nil {
		c.clickTitle(leaf)
		return
	}
	c.focusAt(x, y)
}

// clickTitle focuses leaf, or on a second quick click zooms/unzooms it —
// the same double-click convention a window manager uses.
func (c *Core) clickTitle(leaf *layout.Node) {
	if c.registerTitleClick(leaf) {
		c.toggleZoomOn(leaf)
	} else {
		c.setActive(leaf)
	}
}

// endTitleDrag resolves a title-bar drag on release: dropping it on a
// *different* window's tab in the status bar moves that pane there (see
// movePaneToWindow); anything else — no movement at all (the click was
// already handled on press by clickTitle), or a drop that missed the tab
// strip — is a no-op.
func (c *Core) endTitleDrag(x, y int) {
	drag := c.titleDrag
	c.titleDrag = nil
	if drag == nil || (x == c.dragDownX && y == c.dragDownY) {
		return
	}
	if c.statusRows() == 0 || y != c.rows-1 {
		return
	}
	wi, ok := c.tabAt(x)
	if !ok || c.windows[wi] == drag.win {
		return
	}
	c.movePaneToWindow(drag.leaf, drag.win, c.windows[wi])
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
				// Releasing the drag ends copy-mode, exactly like the
				// keyboard's y/Enter (and like tmux, whose default
				// MouseDragEnd1Pane binding is copy-pipe-and-*cancel*).
				// Staying in copy-mode instead left the pane frozen at
				// whatever row the view was scrolled to, with typing going
				// to the copy cursor rather than the shell — so the first
				// mouse copy appeared to work and everything after it
				// looked broken until you happened to press q or Esc.
				c.exitCopyMode()
				if ok {
					c.statusMsg = "copied " + copiedSummary(text)
				}
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
		c.copy.lineWise = false // a drag is always character-wise, even if a prior keyboard 'V' left this set
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
