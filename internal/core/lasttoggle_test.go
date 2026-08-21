package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func TestToggleLastWindowFlipsBackAndForth(t *testing.T) {
	c := setupNamedWindows(t, "A", "B", "C")
	c.mu.Lock()
	c.selectWindowIndex(0) // A active, lastWindow now whatever was active before (C, from setupNamedWindows' own creation)

	c.selectWindowIndex(2) // C active, lastWindow = A
	c.toggleLastWindow()   // back to A, lastWindow = C
	nameAfterFirst := c.windowDisplayName(c.win())
	c.toggleLastWindow() // back to C
	nameAfterSecond := c.windowDisplayName(c.win())
	c.mu.Unlock()

	if nameAfterFirst != "A" {
		t.Fatalf("after first toggle, active window = %q, want %q", nameAfterFirst, "A")
	}
	if nameAfterSecond != "C" {
		t.Fatalf("after second toggle, active window = %q, want %q", nameAfterSecond, "C")
	}
}

func TestToggleLastWindowNoopWithoutHistory(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	before := c.activeWindow
	c.toggleLastWindow() // freshly created session has no lastWindow yet
	after := c.activeWindow
	c.mu.Unlock()

	if before != after {
		t.Fatalf("toggling with no recorded last window should be a no-op, activeWindow changed from %d to %d", before, after)
	}
}

// TestToggleLastWindowNoopWhenTargetClosed closes A (the recorded
// lastWindow) the same way killWindow itself does — close every pane in
// it, then removeWindow — out from under B, the window actually still
// active, and checks the dangling pointer this would otherwise leave in
// c.lastWindow gets cleared rather than making Ctrl-B W jump somewhere
// wrong (or panic).
func TestToggleLastWindowNoopWhenTargetClosed(t *testing.T) {
	c := setupNamedWindows(t, "A", "B")
	c.mu.Lock()
	c.selectWindowIndex(1) // B active, lastWindow = A

	windowA := c.windows[0]
	for _, l := range layout.Leaves(windowA.root) {
		if p, ok := c.panes[l.ID]; ok {
			p.Close()
			delete(c.panes, l.ID)
		}
	}
	c.removeWindow(0)

	before := c.activeWindow
	c.toggleLastWindow()
	after := c.activeWindow
	stillHasLastWindow := c.lastWindow != nil
	c.mu.Unlock()

	if before != after {
		t.Fatalf("toggling to a closed window should be a no-op, activeWindow changed from %d to %d", before, after)
	}
	if stillHasLastWindow {
		t.Fatal("lastWindow should be cleared once the window it pointed to is gone")
	}
}

func TestNewWindowUpdatesLastWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	first := c.win()
	c.newWindow() // creates+switches to a second window
	c.mu.Unlock()

	c.mu.Lock()
	last := c.lastWindow
	c.mu.Unlock()

	if last != first {
		t.Fatal("creating a new window should record the previously active one as lastWindow")
	}
}

func TestBreakPaneToNewWindowUpdatesLastWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	src := c.win()
	c.breakPaneToNewWindow()
	last := c.lastWindow
	c.mu.Unlock()

	if last != src {
		t.Fatal("break-pane should record the source window as lastWindow")
	}
}

func TestToggleLastPaneFlipsBackAndForth(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	c.setActive(leaves[2]) // lastActive = leaves[0]

	c.toggleLastPane()
	afterFirst := c.win().active.ID
	c.toggleLastPane()
	afterSecond := c.win().active.ID
	c.mu.Unlock()

	if afterFirst != leaves[0].ID {
		t.Fatalf("after first toggle, active pane = %d, want leaf[0] (%d)", afterFirst, leaves[0].ID)
	}
	if afterSecond != leaves[2].ID {
		t.Fatalf("after second toggle, active pane = %d, want leaf[2] (%d)", afterSecond, leaves[2].ID)
	}
}

func TestToggleLastPaneNoopWithoutHistory(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	before := c.win().active.ID
	c.toggleLastPane() // a single fresh pane has no lastActive yet
	after := c.win().active.ID
	c.mu.Unlock()

	if before != after {
		t.Fatal("toggling with no recorded last pane should be a no-op")
	}
}

func TestToggleLastPaneClearedWhenTargetCloses(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	c.setActive(leaves[1]) // lastActive = leaves[0]

	// Close leaves[0] (the recorded lastActive) directly, same path a
	// shell exiting on its own takes.
	c.detachLeafIn(c.win(), leaves[0])
	stillHasLastActive := c.win().lastActivePane != 0
	c.mu.Unlock()

	if stillHasLastActive {
		t.Fatal("lastActive should be cleared once the pane it pointed to is gone")
	}
}

func TestToggleLastPaneClearedWhenMovedToAnotherWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	c.setActive(leaves[1]) // lastActive = leaves[0]
	src := c.win()
	c.newWindowOpts("target", "")
	dst := c.windows[len(c.windows)-1]

	c.movePaneToWindow(leaves[0], src, dst)
	stillHasLastActive := src.lastActivePane != 0
	c.mu.Unlock()

	if stillHasLastActive {
		t.Fatal("lastActive should be cleared once the pane it pointed to moved to a different window")
	}
}

// The jump picker switches window *and* pane in one step, so it can't go
// through setActive — it used to assign w.active directly, which skipped
// the lastActive bookkeeping and left Ctrl-B ; a no-op after any jump.
func TestToggleLastPaneAfterPickerJump(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	from := c.win().active.ID
	target := leaves[1].ID

	c.enterPicker()
	for fi, idx := range c.picker.filtered {
		if c.picker.items[idx].paneID == target {
			c.picker.sel = fi
		}
	}
	c.confirmPicker()
	landed := c.win().active.ID

	c.toggleLastPane()
	back := c.win().active.ID
	c.mu.Unlock()

	if landed != target {
		t.Fatalf("picker jumped to pane %d, want %d", landed, target)
	}
	if back != from {
		t.Fatalf("Ctrl-B ; after a picker jump landed on pane %d, want %d (the one we left)", back, from)
	}
}

// Same contract via the Ctrl-B g overview, the other path that switches
// window and pane together.
func TestToggleLastPaneAfterOverviewJump(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	from := c.win().active.ID
	target := leaves[1].ID

	c.enterOverview()
	for i, tl := range c.overview.tiles {
		if tl.paneID == target {
			c.overview.sel = i
		}
	}
	c.confirmOverview()

	c.toggleLastPane()
	back := c.win().active.ID
	c.mu.Unlock()

	if back != from {
		t.Fatalf("Ctrl-B ; after an overview jump landed on pane %d, want %d (the one we left)", back, from)
	}
}
