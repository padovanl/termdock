package core

import (
	"strconv"
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

// paneIDsOf returns the IDs of w's panes in the same left-to-right,
// top-to-bottom order their on-screen numbers follow.
func paneIDsOf(c *Core, w *Window) []int {
	var ids []int
	for _, l := range layout.Leaves(w.root) {
		ids = append(ids, l.ID)
	}
	return ids
}

// setupDivergedIDs builds a window whose pane IDs no longer line up with
// their positions — which is all it takes for "is this number a position
// or an internal ID?" to stop being an academic question. Splitting to
// four panes and closing the first leaves positions 1..3 held by panes
// with three higher, non-contiguous IDs.
func setupDivergedIDs(t *testing.T) *Core {
	t.Helper()
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	c.doSplit(layout.Vertical)
	w := c.win()
	first := layout.Leaves(w.root)[0]
	c.setActive(first)
	c.killActive()
	ids := paneIDsOf(c, c.win())
	c.mu.Unlock()

	if len(ids) != 3 {
		t.Fatalf("test setup: expected 3 panes, got %d", len(ids))
	}
	for i, id := range ids {
		if id == i+1 {
			t.Fatalf("test setup: pane at position %d still has ID %d; IDs and positions must diverge for this test to mean anything (%v)", i+1, id, ids)
		}
	}
	return c
}

// TestListPanesReportsOnScreenNumbers: list-panes' TITLE column is built
// by the same paneTitle that renders a pane's own title bar, and it was
// being handed the internal pane ID where the screen shows the pane's
// position. A script correlating the two saw "3:bash" for the pane
// labelled "2:bash" on screen.
func TestListPanesReportsOnScreenNumbers(t *testing.T) {
	c := setupDivergedIDs(t)

	panes, err := c.CLIListPanes(-1, "")
	if err != nil {
		t.Fatalf("CLIListPanes: %v", err)
	}
	for i, p := range panes {
		if p.Index != i+1 {
			t.Errorf("pane at position %d reports Index %d", i+1, p.Index)
		}
		wantPrefix := strconv.Itoa(i+1) + ":"
		if !strings.HasPrefix(p.Title, wantPrefix) {
			t.Errorf("pane at position %d has title %q, want it to start with %q (its on-screen number), not its ID %d", i+1, p.Title, wantPrefix, p.ID)
		}
	}
}

// TestSendKeysTargetsPaneByPosition: ".PANE" is the number on screen, and
// it is resolved inside the target window. It used to be looked up as a
// session-wide pane ID, ignoring the window part of the TARGET entirely.
func TestSendKeysTargetsPaneByPosition(t *testing.T) {
	c := setupDivergedIDs(t)
	c.mu.Lock()
	ids := paneIDsOf(c, c.win())
	c.mu.Unlock()

	for pos, wantID := range ids {
		c.mu.Lock()
		got, ok := c.resolveTargetPane(-1, "", pos+1)
		c.mu.Unlock()
		if !ok {
			t.Fatalf("position %d did not resolve to a pane", pos+1)
		}
		if got.ID != wantID {
			t.Errorf("target .%d resolved to pane ID %d, want the pane at that position (%d)", pos+1, got.ID, wantID)
		}
	}

	c.mu.Lock()
	_, ok := c.resolveTargetPane(-1, "", len(ids)+1)
	c.mu.Unlock()
	if ok {
		t.Error("a position past the last pane should not resolve")
	}
}

// TestSendKeysDoesNotEscapeItsWindow: with the number treated as a
// session-wide ID, "-t session:1.2" could act on a pane living in window
// 0 — the window part was never consulted at all.
func TestSendKeysDoesNotEscapeItsWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical) // window 0: two panes
	window0IDs := paneIDsOf(c, c.windows[0])
	c.newWindow() // window 1: one pane
	window1IDs := paneIDsOf(c, c.windows[1])
	c.mu.Unlock()

	if len(window1IDs) != 1 {
		t.Fatalf("test setup: window 1 should have 1 pane, got %d", len(window1IDs))
	}

	// Window 1 has only one pane, so position 2 must not resolve —
	// least of all to one of window 0's panes.
	c.mu.Lock()
	got, ok := c.resolveTargetPane(1, "", 2)
	c.mu.Unlock()
	if ok {
		t.Fatalf("window 1 has one pane, but position 2 resolved to pane ID %d (window 0 holds %v)", got.ID, window0IDs)
	}

	c.mu.Lock()
	got, ok = c.resolveTargetPane(1, "", 1)
	c.mu.Unlock()
	if !ok || got.ID != window1IDs[0] {
		t.Errorf("window 1 position 1 should resolve to its own pane %d", window1IDs[0])
	}
}

// TestSplitWindowHonoursThePaneTarget: the README's own example
// (`split-window -t main:dev.1`) reads as "split pane 1", but the pane
// part was parsed off the target and then discarded here, so it always
// split whichever pane happened to be active.
func TestSplitWindowHonoursThePaneTarget(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical) // two panes; the *second* is active
	w := c.win()
	target := layout.Leaves(w.root)[0]
	targetID := target.ID
	activeID := w.active.ID
	c.mu.Unlock()

	if targetID == activeID {
		t.Fatal("test setup: the pane being targeted must not already be the active one")
	}

	newID, err := c.CLISplitWindow(-1, "", 1, layout.Horizontal, "")
	if err != nil {
		t.Fatalf("CLISplitWindow: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	newLeaf := findLeafByID(c.win().root, newID)
	if newLeaf == nil {
		t.Fatal("the new pane is not in the window's tree")
	}
	// Splitting pane 1 puts the new pane under the same parent as pane 1,
	// not under the pane that was merely active.
	sibling := newLeaf.Parent.First
	if sibling == newLeaf {
		sibling = newLeaf.Parent.Second
	}
	if sibling.ID != targetID {
		t.Errorf("split-window -t .1 split pane %d, want the pane at position 1 (%d)", sibling.ID, targetID)
	}
}

func TestSplitWindowRejectsAPaneThatIsNotThere(t *testing.T) {
	c := newTestCore(t)
	if _, err := c.CLISplitWindow(-1, "", 9, layout.Vertical, ""); err == nil {
		t.Fatal("splitting a pane position that doesn't exist should be an error, not a silent split of the active pane")
	}
}
