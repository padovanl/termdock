package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
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

	// Scrolling up from 0 must clamp at 0, not go negative.
	c.handleHelpKey(tcell.KeyUp, 0)
	if c.help.scroll != 0 {
		t.Errorf("scroll should clamp at 0, got %d", c.help.scroll)
	}
	// PgDn should advance, clamped eventually at the last entry.
	c.handleHelpKey(tcell.KeyPgDn, 0)
	if want := minInt(10, numEntries-1); c.help.scroll != want {
		t.Errorf("scroll after one PgDn = %d, want %d", c.help.scroll, want)
	}
	for i := 0; i < numEntries+5; i++ {
		c.handleHelpKey(tcell.KeyDown, 0)
	}
	if c.help.scroll != numEntries-1 {
		t.Errorf("scroll should clamp at the last entry (%d), got %d", numEntries-1, c.help.scroll)
	}
	if c.mode != ModeHelp {
		t.Fatalf("scrolling must not close the help screen, mode=%v", c.mode)
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
