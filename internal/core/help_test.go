package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
)

func TestHelpScrollClampsAndAnyKeyCloses(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterHelp()
	if c.mode != ModeHelp {
		t.Fatalf("enterHelp should set ModeHelp, got %v", c.mode)
	}
	numEntries := len(c.help.entries)
	if ov := c.helpOverlay(); ov == nil || len(ov.Items) != numEntries {
		t.Fatalf("helpOverlay should list all %d entries, got %v", numEntries, ov)
	}
	maxScroll := c.maxHelpScroll()
	if maxScroll <= 0 {
		t.Fatalf("the test terminal (%dx%d) should be too short for all %d entries; nothing here scrolls otherwise", c.cols, c.rows, numEntries)
	}

	// Scrolling up from 0 must clamp at 0, not go negative.
	c.handleHelpKey(tcell.KeyUp, 0)
	if c.help.scroll != 0 {
		t.Errorf("scroll should clamp at 0, got %d", c.help.scroll)
	}
	// PgDn moves by a screenful of the list, clamped at the last position
	// that still shows a full page.
	c.handleHelpKey(tcell.KeyPgDn, 0)
	if want := minInt(c.helpListRows(), maxScroll); c.help.scroll != want {
		t.Errorf("scroll after one PgDn = %d, want %d", c.help.scroll, want)
	}
	for i := 0; i < numEntries+5; i++ {
		c.handleHelpKey(tcell.KeyDown, 0)
	}
	// Clamped at maxHelpScroll, *not* at numEntries-1: stopping there
	// would park the view past the end, where the list shows mostly blank
	// rows and scrolling back up spends a whole screenful of keypresses
	// doing nothing visible before the view finally moves.
	if c.help.scroll != maxScroll {
		t.Errorf("scroll should clamp at the last full page (%d), got %d", maxScroll, c.help.scroll)
	}
	if c.mode != ModeHelp {
		t.Fatalf("scrolling must not close the help screen, mode=%v", c.mode)
	}

	c.handleHelpKey(tcell.KeyHome, 0)
	if c.help.scroll != 0 {
		t.Errorf("Home should jump back to the top, got %d", c.help.scroll)
	}
	c.handleHelpKey(tcell.KeyEnd, 0)
	if c.help.scroll != maxScroll {
		t.Errorf("End should jump to the bottom (%d), got %d", maxScroll, c.help.scroll)
	}

	// Any non-scroll key closes it.
	c.handleHelpKey(0, 'q')
	if c.mode != ModeNormal {
		t.Fatalf("a non-scroll key should return to ModeNormal, got %v", c.mode)
	}
	if ov := c.helpOverlay(); ov != nil {
		t.Fatalf("helpOverlay should be nil once closed, got %v", ov)
	}
	c.mu.Unlock()
}

// TestHelpFirstDownScrollsImmediately pins the bug that made the help
// screen look broken on a short terminal: the overlay's Selected used to
// be run through the picker's keep-the-selection-visible math even
// though help has no selection, so the first screenful of ↓ presses
// changed the reported scroll offset without changing a single visible
// row. Asserted at the overlay level, since that offset is the only
// thing the client has to go on.
func TestHelpFirstDownScrollsImmediately(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterHelp()
	if c.maxHelpScroll() == 0 {
		t.Skip("every entry fits on screen; there's nothing to scroll")
	}
	first := c.helpOverlay().Items[0]

	c.handleHelpKey(tcell.KeyDown, 0)
	ov := c.helpOverlay()
	if ov.Selectable {
		t.Fatal("the help overlay must stay non-selectable — that's what tells the client Selected is a scroll offset")
	}
	if ov.Selected != 1 {
		t.Fatalf("one ↓ should scroll by exactly one entry, got offset %d", ov.Selected)
	}
	if ov.Items[ov.Selected] == first {
		t.Fatal("the first visible entry should have changed after one ↓")
	}
}

// TestHelpWheelScrolls covers reaching for the mouse wheel on the help
// screen, which used to be swallowed wholesale along with clicks.
func TestHelpWheelScrolls(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.enterHelp()
	tooShort := c.maxHelpScroll() > 0
	c.mu.Unlock()
	if !tooShort {
		t.Skip("every entry fits on screen; there's nothing to scroll")
	}

	c.handleMouse(wheelMsg(tcell.WheelDown))
	c.mu.Lock()
	down := c.help.scroll
	mode := c.mode
	c.mu.Unlock()
	if down == 0 {
		t.Fatal("a wheel-down on the help screen should scroll it")
	}
	if mode != ModeHelp {
		t.Fatalf("the wheel must not close the help screen, mode=%v", mode)
	}

	c.handleMouse(wheelMsg(tcell.WheelUp))
	c.mu.Lock()
	up := c.help.scroll
	c.mu.Unlock()
	if up >= down {
		t.Fatalf("a wheel-up should scroll back, went %d -> %d", down, up)
	}
}

// TestHelpClickStillIgnored guards the other half of that change: only
// the wheel got let through, a stray click must still not reach whatever
// is underneath the help screen.
func TestHelpClickStillIgnored(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.enterHelp()
	activeBefore := c.win().active
	c.mu.Unlock()

	c.handleMouse(mouseMsg(2, 2))

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mode != ModeHelp {
		t.Fatalf("a click should not close the help screen, mode=%v", c.mode)
	}
	if c.win().active != activeBefore {
		t.Fatal("a click behind the help screen must not change which pane is focused")
	}
}
