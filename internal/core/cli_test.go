package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func TestCLISelectPaneMovesFocusByDirection(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical) // left/right leaves; right one active
	leaves := layout.Leaves(c.win().root)
	leftID, rightID := leaves[0].ID, leaves[1].ID
	c.mu.Unlock()

	if err := c.CLISelectPane(-1, "", "L"); err != nil {
		t.Fatalf("CLISelectPane L: %v", err)
	}
	c.mu.Lock()
	active := c.win().active.ID
	c.mu.Unlock()
	if active != leftID {
		t.Fatalf("after select-pane -L, active pane = %d, want the left leaf (%d)", active, leftID)
	}

	if err := c.CLISelectPane(-1, "", "R"); err != nil {
		t.Fatalf("CLISelectPane R: %v", err)
	}
	c.mu.Lock()
	active = c.win().active.ID
	c.mu.Unlock()
	if active != rightID {
		t.Fatalf("after select-pane -R, active pane = %d, want the right leaf (%d)", active, rightID)
	}
}

func TestCLISelectPaneNoopAtTheEdge(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	before := c.win().active.ID
	c.mu.Unlock()

	// A single pane has nowhere to go in any direction — a no-op, not
	// an error, the same as tmux's own select-pane at the edge of a
	// layout.
	if err := c.CLISelectPane(-1, "", "L"); err != nil {
		t.Fatalf("CLISelectPane at the edge should not error, got %v", err)
	}
	c.mu.Lock()
	after := c.win().active.ID
	c.mu.Unlock()
	if after != before {
		t.Fatalf("active pane should be unchanged, was %d now %d", before, after)
	}
}

func TestCLISelectPaneUnknownDirection(t *testing.T) {
	c := newTestCore(t)
	if err := c.CLISelectPane(-1, "", "sideways"); err == nil {
		t.Fatal("an unrecognized direction should be an error")
	}
}

func TestCLISelectPaneUnknownWindow(t *testing.T) {
	c := newTestCore(t)
	if err := c.CLISelectPane(99, "", "L"); err == nil {
		t.Fatal("a nonexistent window index should be an error")
	}
}

// TestCLISelectPaneTargetsABackgroundWindow checks the whole point of
// exposing this over the *external* CLI surface rather than just the
// interactive hjkl keys: moving focus in a window that ISN'T the one
// currently visible, without disturbing what's actually on screen.
func TestCLISelectPaneTargetsABackgroundWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	bgWindow := c.win()
	leaves := layout.Leaves(bgWindow.root)
	leftID := leaves[0].ID
	c.newWindowOpts("fg", "") // second window, now the visible/active one
	visibleWindow := c.win()
	c.mu.Unlock()

	if err := c.CLISelectPane(0, "", "L"); err != nil {
		t.Fatalf("CLISelectPane targeting window 0: %v", err)
	}

	c.mu.Lock()
	bgActive := bgWindow.active.ID
	stillOnVisible := c.win() == visibleWindow
	c.mu.Unlock()

	if bgActive != leftID {
		t.Fatalf("background window's active pane = %d, want the left leaf (%d)", bgActive, leftID)
	}
	if !stillOnVisible {
		t.Fatal("targeting a background window's pane shouldn't switch which window is visible")
	}
}
