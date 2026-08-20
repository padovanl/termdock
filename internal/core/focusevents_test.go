package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

// These tests deliberately check c.focusedPaneID transitions rather
// than trying to observe the raw \x1b[I/\x1b[O bytes through a pane's
// rendered terminal grid: vt10x parses whatever reaches a pane as
// terminal *output*, so writing an escape sequence *in* would get
// interpreted as a control sequence, not shown as visible text — not a
// reliable thing to assert on. Pane.Write() delivering bytes to a real
// pty is already exercised throughout the suite (every
// writeAndWaitEcho-based test proves it); what's specific to this
// feature, and what's worth pinning down here, is the *decision* of
// when updateFocusEvents fires and for which pane.

func TestFocusEventsOffByDefaultDoesNothing(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	before := c.focusedPaneID
	c.setActive(leaves[0])
	after := c.focusedPaneID
	c.mu.Unlock()

	if before != 0 || after != 0 {
		t.Fatalf("focusedPaneID should stay untouched while focus-events is off: before=%d after=%d", before, after)
	}
}

func TestFocusEventsTracksTheActivePaneWithinAWindow(t *testing.T) {
	c := newTestCore(t)
	c.SetFocusEvents(true)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[1])
	afterFirst := c.focusedPaneID
	c.setActive(leaves[0])
	afterSecond := c.focusedPaneID
	c.mu.Unlock()

	if afterFirst != leaves[1].ID {
		t.Fatalf("focusedPaneID after switching to leaves[1] = %d, want %d", afterFirst, leaves[1].ID)
	}
	if afterSecond != leaves[0].ID {
		t.Fatalf("focusedPaneID after switching to leaves[0] = %d, want %d", afterSecond, leaves[0].ID)
	}
}

func TestFocusEventsTracksWindowSwitches(t *testing.T) {
	c := newTestCore(t)
	c.SetFocusEvents(true)

	c.mu.Lock()
	firstPaneID := c.win().active.ID
	c.updateFocusEvents(firstPaneID) // establish the baseline the same way the first real switch would
	c.newWindow()
	secondPaneID := c.win().active.ID
	afterNewWindow := c.focusedPaneID
	c.selectWindowIndex(0)
	afterSwitchBack := c.focusedPaneID
	c.mu.Unlock()

	if afterNewWindow != secondPaneID {
		t.Fatalf("focusedPaneID after creating+switching to a new window = %d, want %d", afterNewWindow, secondPaneID)
	}
	if afterSwitchBack != firstPaneID {
		t.Fatalf("focusedPaneID after switching back = %d, want %d", afterSwitchBack, firstPaneID)
	}
}

func TestUpdateFocusEventsIsANoopWhenTargetUnchanged(t *testing.T) {
	c := newTestCore(t)
	c.SetFocusEvents(true)

	c.mu.Lock()
	id := c.win().active.ID
	c.updateFocusEvents(id)
	first := c.focusedPaneID
	c.updateFocusEvents(id) // same target again
	second := c.focusedPaneID
	c.mu.Unlock()

	if first != id || second != id {
		t.Fatalf("focusedPaneID should stay %d across a repeated no-op call, got %d then %d", id, first, second)
	}
}

func TestSetFocusEventsToggle(t *testing.T) {
	c := newTestCore(t)

	c.SetFocusEvents(true)
	c.mu.Lock()
	enabled := c.focusEvents
	c.mu.Unlock()
	if !enabled {
		t.Fatal("SetFocusEvents(true) should enable it")
	}

	c.SetFocusEvents(false)
	c.mu.Lock()
	enabled = c.focusEvents
	c.mu.Unlock()
	if enabled {
		t.Fatal("SetFocusEvents(false) should disable it")
	}
}
