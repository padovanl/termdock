package core

import (
	"testing"

	"termdock/internal/layout"

	"github.com/gdamore/tcell/v2"
)

func TestEnterQuickJumpRequiresMultiplePanes(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.enterQuickJump()
	mode := c.mode
	msg := c.statusMsg
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("quick-jump with a single pane should stay in ModeNormal, got %v", mode)
	}
	if msg == "" {
		t.Fatal("expected a status message explaining why quick-jump didn't open")
	}
}

func TestQuickJumpTagsAndJumps(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	c.enterQuickJump()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeQuickJump {
		t.Fatalf("enterQuickJump with multiple panes: mode = %v, want ModeQuickJump", mode)
	}

	f := c.Frame()
	if len(f.QuickJump) != len(leaves) {
		t.Fatalf("QuickJump tags = %d, want %d (one per pane)", len(f.QuickJump), len(leaves))
	}
	for i, tag := range f.QuickJump {
		if tag.Digit != rune('1'+i) {
			t.Fatalf("tag %d digit = %q, want %q", i, tag.Digit, rune('1'+i))
		}
	}

	c.mu.Lock()
	c.handleQuickJumpKey(tcell.KeyRune, '3')
	activeID := c.win().active.ID
	modeAfter := c.mode
	c.mu.Unlock()

	if modeAfter != ModeNormal {
		t.Fatalf("mode after a digit press = %v, want ModeNormal", modeAfter)
	}
	if activeID != leaves[2].ID {
		t.Fatalf("active pane after pressing '3' = %d, want leaf[2] (%d)", activeID, leaves[2].ID)
	}
}

func TestQuickJumpAnyOtherKeyCancelsWithoutJumping(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	activeBefore := c.win().active.ID
	c.enterQuickJump()
	c.handleQuickJumpKey(tcell.KeyRune, 'z')
	activeAfter := c.win().active.ID
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("mode after a non-digit key = %v, want ModeNormal", mode)
	}
	if activeAfter != activeBefore {
		t.Fatal("a non-digit key shouldn't change the active pane")
	}
}

func TestQuickJumpBlockedWhileZoomed(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.toggleZoom()
	c.enterQuickJump()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("quick-jump while zoomed should stay in ModeNormal, got %v", mode)
	}
}
