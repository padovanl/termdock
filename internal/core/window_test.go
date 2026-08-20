package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func TestMoveWindowReordersAndTracksActive(t *testing.T) {
	cases := []struct {
		name       string
		from, to   int
		activeIdx  int // index (into the ORIGINAL A,B,C,D order) active before the move
		wantOrder  []string
		wantActive string // which window (by original name) should still be active after
	}{
		{"forward, active is the one moved", 0, 2, 0, []string{"B", "C", "A", "D"}, "A"},
		{"backward, active is the one moved", 2, 0, 2, []string{"C", "A", "B", "D"}, "C"},
		{"forward, active sits between", 0, 3, 1, []string{"B", "C", "D", "A"}, "B"},
		{"forward, active untouched", 0, 2, 3, []string{"B", "C", "A", "D"}, "D"},
		{"backward, active sits between", 3, 0, 1, []string{"D", "A", "B", "C"}, "B"},
		{"no-op, from == to", 1, 1, 1, []string{"A", "B", "C", "D"}, "B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := setupNamedWindows(t, "A", "B", "C", "D")
			c.mu.Lock()
			c.activeWindow = tc.activeIdx
			c.moveWindow(tc.from, tc.to)
			gotOrder := namesOf(c)
			gotActive := c.windowDisplayName(c.windows[c.activeWindow])
			c.mu.Unlock()

			if !eqStrings(gotOrder, tc.wantOrder) {
				t.Errorf("order = %v, want %v", gotOrder, tc.wantOrder)
			}
			if gotActive != tc.wantActive {
				t.Errorf("active window = %q, want %q", gotActive, tc.wantActive)
			}
		})
	}
}

func TestMovePaneToWindow(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical) // window 0: 2 panes; active = right one
	c.newWindowOpts("target", "")
	src, dst := c.windows[0], c.windows[1]
	movedLeaf := src.root.Second // the right pane, currently active in src
	movedID := movedLeaf.ID

	ok := c.movePaneToWindow(movedLeaf, src, dst)
	srcLeavesAfter := len(layout.Leaves(src.root))
	dstLeavesAfter := len(layout.Leaves(dst.root))
	_, stillInPanes := c.panes[movedID]
	c.mu.Unlock()

	if !ok {
		t.Fatal("movePaneToWindow reported failure")
	}
	if srcLeavesAfter != 1 {
		t.Errorf("source window should have 1 pane left, has %d", srcLeavesAfter)
	}
	if dstLeavesAfter != 2 {
		t.Errorf("target window should have 2 panes now, has %d", dstLeavesAfter)
	}
	if !stillInPanes {
		t.Error("moved pane's process should still be tracked in c.panes — moving must not close it")
	}
	if leaf := findLeafByID(dst.root, movedID); leaf == nil {
		t.Error("moved pane's ID should be reachable from the target window's tree")
	}
	if leaf := findLeafByID(src.root, movedID); leaf != nil {
		t.Error("moved pane's ID should no longer be reachable from the source window's tree")
	}
}

// TestMovePaneEmptyingNonActiveWindowKeepsActiveCorrect is a regression
// test for a latent bug in removeWindow: it used to assume the window
// being removed was always the currently active one — true for every
// caller that existed at the time (killWindow always closes the window
// you're looking at) — and clamped c.activeWindow to the new slice
// length instead of re-deriving it, which would silently point
// activeWindow at the wrong window the first time something removed a
// *different*, non-active window (as movePaneToWindow can, when the pane
// it's moving was the last one in its window).
func TestMovePaneEmptyingNonActiveWindowKeepsActiveCorrect(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	// window 0: default. window 1: "lonely" (1 pane, about to be
	// emptied by the move). window 2: "elsewhere" — made active, and
	// NOT involved in the move at all. This exercises removeWindow's
	// fix: idx (1) is neither 0 nor c.activeWindow (2).
	c.newWindowOpts("lonely", "")
	c.newWindowOpts("elsewhere", "")
	c.selectWindowIndex(2)
	lonely := c.windows[1]
	elsewhere := c.windows[2]
	target := c.windows[0]
	lonelyLeaf := lonely.root

	ok := c.movePaneToWindow(lonelyLeaf, lonely, target)
	stillActive := c.windows[c.activeWindow]
	windowCount := len(c.windows)
	c.mu.Unlock()

	if !ok {
		t.Fatal("movePaneToWindow reported failure")
	}
	if windowCount != 2 {
		t.Fatalf("the now-empty 'lonely' window should have been removed: expected 2 windows, got %d", windowCount)
	}
	if stillActive != elsewhere {
		t.Fatalf("active window should still be 'elsewhere' after an unrelated window was removed, got %q", c.windowDisplayName(stillActive))
	}
}

func TestKillWindowRequiresConfirmation(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	windowsBefore := len(c.windows)
	c.confirmKillWindow()
	modeAfterAsk := c.mode
	c.mu.Unlock()

	if modeAfterAsk != ModeConfirm {
		t.Fatalf("confirmKillWindow should enter ModeConfirm, got %v", modeAfterAsk)
	}

	// Cancel with 'n': window must survive.
	c.mu.Lock()
	c.handleConfirmKey('n')
	windowsAfterCancel := len(c.windows)
	modeAfterCancel := c.mode
	c.mu.Unlock()
	if windowsAfterCancel != windowsBefore {
		t.Fatalf("'n' should NOT kill the window: had %d, now %d", windowsBefore, windowsAfterCancel)
	}
	if modeAfterCancel != ModeNormal {
		t.Fatalf("cancelling should return to ModeNormal, got %v", modeAfterCancel)
	}

	// Confirm with 'y': window must actually go away.
	c.mu.Lock()
	c.confirmKillWindow()
	c.handleConfirmKey('y')
	windowsAfterConfirm := len(c.windows)
	c.mu.Unlock()
	if windowsAfterConfirm != windowsBefore-1 {
		t.Fatalf("'y' should kill the window: had %d, want %d, got %d", windowsBefore, windowsBefore-1, windowsAfterConfirm)
	}
}

func TestBreakPaneToNewWindow(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical) // 2 panes; right one active
	leaf := c.win().active
	leafID := leaf.ID
	src := c.win()
	windowsBefore := len(c.windows)

	c.breakPaneToNewWindow()
	windowsAfter := len(c.windows)
	newWin := c.windows[c.activeWindow]
	srcLeavesAfter := len(layout.Leaves(src.root))
	c.mu.Unlock()

	if windowsAfter != windowsBefore+1 {
		t.Fatalf("expected 1 new window, had %d now %d", windowsBefore, windowsAfter)
	}
	if newWin.root != leaf || newWin.root.ID != leafID {
		t.Fatalf("the broken-out pane should be the new window's only, root, pane")
	}
	if leaf.Parent != nil {
		t.Fatal("the broken-out leaf should be a tree root now (nil Parent)")
	}
	if srcLeavesAfter != 1 {
		t.Fatalf("source window should have 1 pane left, has %d", srcLeavesAfter)
	}
}

func TestBreakPaneAloneInWindowIsANoop(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	windowsBefore := len(c.windows)
	c.breakPaneToNewWindow() // the single default pane has no siblings
	windowsAfter := len(c.windows)
	c.mu.Unlock()

	if windowsAfter != windowsBefore {
		t.Fatalf("breaking the only pane in a window should be a no-op, had %d now %d", windowsBefore, windowsAfter)
	}
}
