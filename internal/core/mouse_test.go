package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func TestClickWindowTabSwitchesWindow(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	c.newWindow()
	tabs := c.windowTabs()
	tab0 := tabs[0]
	c.mu.Unlock()

	clickX := tab0.X + tab0.W/2
	c.mu.Lock()
	wi, ok := c.tabAt(clickX)
	c.mu.Unlock()
	if !ok || wi != 0 {
		t.Fatalf("click at x=%d (inside tab0 range [%d,%d)) should hit tab 0, got wi=%d ok=%v", clickX, tab0.X, tab0.X+tab0.W, wi, ok)
	}

	c.handleNormalMouse(true, false, tab0.X+1, c.rows-1)
	c.handleNormalMouse(false, true, tab0.X+1, c.rows-1)
	c.mu.Lock()
	active := c.activeWindow
	c.mu.Unlock()
	if active != 0 {
		t.Fatalf("clicking tab 0 should select window 0, active=%d", active)
	}
}

func TestDividerDragResizes(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	root := c.win().root
	dividerX := root.DividerX
	midY := root.Rect.Y + root.Rect.H/2
	ratioBefore := root.Ratio
	c.mu.Unlock()

	c.mu.Lock()
	c.handleNormalMouse(true, false, dividerX, midY) // press on the divider (not a title row)
	dragArmed := c.drag != nil
	c.handleNormalMouse(true, false, dividerX+10, midY) // drag it
	c.handleNormalMouse(false, true, dividerX+10, midY) // release, moved
	ratioAfter := root.Ratio
	dragAfter := c.drag
	c.mu.Unlock()

	if !dragArmed {
		t.Fatal("pressing on a divider should arm a drag")
	}
	if dragAfter != nil {
		t.Fatal("drag should be cleared on release")
	}
	if ratioAfter == ratioBefore {
		t.Fatalf("dragging the divider should have changed its ratio (before=%v after=%v)", ratioBefore, ratioAfter)
	}
}

func TestDoubleClickTitleTogglesZoom(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical) // two side-by-side panes, right one active
	leftLeaf := c.win().root.First
	titleRow := leftLeaf.Rect.Y - 1
	titleCol := leftLeaf.Rect.X
	c.mu.Unlock()

	// Single click on the left pane's title: focuses it, no zoom.
	c.mu.Lock()
	c.handleNormalMouse(true, false, titleCol, titleRow)
	c.handleNormalMouse(false, true, titleCol, titleRow)
	activeAfterSingle := c.win().active
	zoomedAfterSingle := c.win().zoomed
	c.mu.Unlock()
	if activeAfterSingle != leftLeaf {
		t.Fatal("single click on title should focus that pane")
	}
	if zoomedAfterSingle != nil {
		t.Fatal("single click on title should NOT zoom")
	}

	// Second click right after, same spot: should zoom it.
	c.mu.Lock()
	c.handleNormalMouse(true, false, titleCol, titleRow)
	c.handleNormalMouse(false, true, titleCol, titleRow)
	zoomedAfterDouble := c.win().zoomed
	c.mu.Unlock()
	if zoomedAfterDouble != leftLeaf {
		t.Fatalf("double click on title should zoom that pane, zoomed=%v want=%v", zoomedAfterDouble, leftLeaf)
	}

	// Double-clicking it again (two more clicks — the double that just
	// fired consumes itself, so a lone third click is just a new single
	// click) un-zooms it.
	c.mu.Lock()
	tRow, tCol := leftLeaf.Rect.Y-1, leftLeaf.Rect.X
	c.handleNormalMouse(true, false, tCol, tRow)
	c.handleNormalMouse(false, true, tCol, tRow)
	c.handleNormalMouse(true, false, tCol, tRow)
	c.handleNormalMouse(false, true, tCol, tRow)
	stillZoomed := c.win().zoomed
	c.mu.Unlock()
	if stillZoomed != nil {
		t.Fatalf("double click on the zoomed pane's title should un-zoom, got zoomed=%v", stillZoomed)
	}
}

func TestContentDragSelectsWithoutPriorCopyMode(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	leaf := c.win().root // single pane, full content area
	pressX, pressY := leaf.Rect.X+2, leaf.Rect.Y+1
	dragX, dragY := leaf.Rect.X+5, leaf.Rect.Y+2

	modeBeforeMove := c.mode
	c.handleNormalMouse(true, false, pressX, pressY) // press: arms contentPress, no mode change yet
	modeAfterPress := c.mode
	c.handleNormalMouse(true, false, dragX, dragY) // move: should escalate into copy-mode selection
	modeAfterMove := c.mode
	selecting := c.copy.selecting
	anchorX, anchorY := c.copy.anchorX, c.copy.anchorY
	curX, curY := c.copy.curX, c.copy.curY
	wantAnchorX, wantAnchorY := pressX-leaf.Rect.X, c.copy.top+(pressY-leaf.Rect.Y)
	c.mu.Unlock()

	if modeBeforeMove != ModeNormal || modeAfterPress != ModeNormal {
		t.Fatalf("a bare press must not itself enter copy-mode: before=%v afterPress=%v", modeBeforeMove, modeAfterPress)
	}
	if modeAfterMove != ModeCopy {
		t.Fatalf("dragging on pane content should enter copy-mode, got mode=%v", modeAfterMove)
	}
	if !selecting {
		t.Fatal("expected a selection to be active after the drag")
	}
	if anchorX != wantAnchorX || anchorY != wantAnchorY {
		t.Errorf("selection anchor = (%d,%d), want the *press* point (%d,%d), not the drag-to point", anchorX, anchorY, wantAnchorX, wantAnchorY)
	}
	if curX == anchorX && curY == anchorY {
		t.Error("selection cursor should have moved away from the anchor to track the drag (a fast single-move drag must not select nothing)")
	}
}

func TestContentClickWithNoMovementJustFocuses(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	leaf := c.win().root
	x, y := leaf.Rect.X+2, leaf.Rect.Y+1
	c.handleNormalMouse(true, false, x, y) // press
	c.handleNormalMouse(false, true, x, y) // release, no movement
	mode := c.mode
	active := c.win().active
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("a stationary click should not enter copy-mode, got %v", mode)
	}
	if active != leaf {
		t.Fatal("a stationary click should still focus the pane, same as before this feature existed")
	}
}

func TestMouseIgnoredDuringConfirm(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	before := c.win().active
	c.confirmKillWindow()
	c.mu.Unlock()

	// A click that would normally focus the other pane must be a no-op
	// while a kill-window confirmation is pending. handleMouse locks
	// internally, so it's called with c.mu free.
	c.mu.Lock()
	target := c.win().root.First
	tx, ty := target.Rect.X, target.Rect.Y
	c.mu.Unlock()
	c.handleMouse(mouseMsg(tx, ty))
	c.mu.Lock()
	afterClickActive := c.win().active
	afterClickMode := c.mode
	c.mu.Unlock()
	if afterClickActive != before {
		t.Fatal("mouse click should be ignored while ModeConfirm is pending")
	}
	if afterClickMode != ModeConfirm {
		t.Fatal("mode should still be ModeConfirm after the ignored click")
	}
}

func TestTabDragReordersLiveAndSelectsOnPlainClick(t *testing.T) {
	c := setupNamedWindows(t, "A", "B", "C")

	c.mu.Lock()
	tabs := c.windowTabs()
	bTab := tabs[1]
	cTab := tabs[2]
	// Press on B's tab, then move past C's midpoint: should live-reorder
	// to [A, C, B] without selecting anything yet.
	c.handleNormalMouse(true, false, bTab.X+1, c.rows-1)
	if c.tabDrag == nil {
		t.Fatal("pressing a tab should arm a tab drag")
	}
	c.handleNormalMouse(true, false, cTab.X+cTab.W-1, c.rows-1)
	orderMidDrag := namesOf(c)
	movedFlag := c.tabDrag.moved
	c.handleNormalMouse(false, true, cTab.X+cTab.W-1, c.rows-1) // release
	orderAfterRelease := namesOf(c)
	dragCleared := c.tabDrag == nil
	c.mu.Unlock()

	if !movedFlag {
		t.Fatal("dragging past another tab's midpoint should mark the drag as moved")
	}
	if !eqStrings(orderMidDrag, []string{"A", "C", "B"}) {
		t.Fatalf("mid-drag order = %v, want [A C B]", orderMidDrag)
	}
	if !eqStrings(orderAfterRelease, orderMidDrag) {
		t.Fatalf("release after a real drag should not change order further: had %v, now %v", orderMidDrag, orderAfterRelease)
	}
	if !dragCleared {
		t.Fatal("tabDrag should be cleared on release")
	}

	// A fresh press-and-release with NO movement on a tab should just
	// select it, same as the old click behavior.
	c.mu.Lock()
	tabs2 := c.windowTabs()
	target := tabs2[0] // "A"
	c.handleNormalMouse(true, false, target.X+1, c.rows-1)
	c.handleNormalMouse(false, true, target.X+1, c.rows-1)
	activeName := c.windowDisplayName(c.windows[c.activeWindow])
	c.mu.Unlock()

	if activeName != "A" {
		t.Fatalf("stationary click on A's tab should select it, got active=%q", activeName)
	}
}

func TestDragTitleOntoTabMovesTheWholePane(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindowOpts("target", "")
	c.selectWindowIndex(0)
	src := c.win()
	leaf := src.root
	titleRow, titleCol := leaf.Rect.Y-1, leaf.Rect.X

	tabs := c.windowTabs()
	targetTab := tabs[1]
	dropX, dropY := targetTab.X+1, c.rows-1

	c.handleNormalMouse(true, false, titleCol, titleRow) // press the title
	armed := c.titleDrag != nil
	c.handleNormalMouse(true, false, dropX, dropY) // drag onto window 1's tab
	c.handleNormalMouse(false, true, dropX, dropY) // drop

	windowCount := len(c.windows)
	var dst *Window
	for _, w := range c.windows {
		if c.windowDisplayName(w) == "target" {
			dst = w
		}
	}
	c.mu.Unlock()

	if !armed {
		t.Fatal("pressing a pane's title should arm a title drag")
	}
	// src had only this one pane (never split), so moving it out should
	// remove the whole window rather than leaving an empty one behind.
	if windowCount != 1 {
		t.Errorf("expected the source window to be gone (its only pane moved out), got %d windows", windowCount)
	}
	if dst == nil {
		t.Fatal("'target' window should still exist")
	}
	// target already had its own one pane; +1 moved in.
	if got := len(layout.Leaves(dst.root)); got != 2 {
		t.Errorf("expected 2 panes in the target window (its own + the moved one), got %d", got)
	}
}

func TestDragTitleOntoOwnWindowTabIsNoop(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	w := c.win()
	leaf := w.root.Second
	titleRow, titleCol := leaf.Rect.Y-1, leaf.Rect.X
	tabs := c.windowTabs()
	ownTab := tabs[0]

	c.handleNormalMouse(true, false, titleCol, titleRow)
	c.handleNormalMouse(true, false, ownTab.X+1, c.rows-1)
	c.handleNormalMouse(false, true, ownTab.X+1, c.rows-1)
	leavesAfter := len(layout.Leaves(w.root))
	windowCount := len(c.windows)
	c.mu.Unlock()

	if leavesAfter != 2 || windowCount != 1 {
		t.Errorf("dropping a title on its own window's tab should be a no-op: leaves=%d windows=%d", leavesAfter, windowCount)
	}
}
