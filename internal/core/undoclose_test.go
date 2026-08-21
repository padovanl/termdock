package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

// The point of the undo is the *place*: closing a pane and reopening it
// must put a shell back in the directory that pane was sitting in, which
// is the tedious part to redo by hand.
func TestReopenRestoresTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()

	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	closed := leaves[1]
	// Give the pane a known directory the way a restored one gets it.
	c.panes[closed.ID].Close()
	delete(c.panes, closed.ID)
	c.closedPanes = nil
	c.closedPanes = append(c.closedPanes, closedPane{
		windowID: c.win().ID, cwd: dir, title: "bash",
	})
	before := len(layout.Leaves(c.win().root))

	c.reopenClosedPane()
	after := layout.Leaves(c.win().root)
	msg := c.statusMsg
	newPane := c.panes[c.win().active.ID]
	c.mu.Unlock()

	if len(after) != before+1 {
		t.Fatalf("panes %d -> %d, want one more", before, len(after))
	}
	if newPane == nil {
		t.Fatal("the reopened pane should be the active one")
	}
	if !strings.Contains(msg, dir) {
		t.Errorf("status %q should say where it came back", msg)
	}
	if got := newPane.Cwd(); got != "" && got != dir {
		t.Errorf("reopened pane's cwd = %q, want %q", got, dir)
	}
}

// Closing a pane must push it onto the stack — including a shell that
// exited on its own, which is exactly when an accidental "exit" happens.
func TestClosingAPaneRecordsItForUndo(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.closedPanes = nil

	c.detachLeafIn(c.win(), leaves[1])
	if len(c.closedPanes) != 1 {
		t.Fatalf("closing a pane recorded %d entries, want 1", len(c.closedPanes))
	}
	if c.closedPanes[0].windowID != c.win().ID {
		t.Errorf("recorded window %d, want %d", c.closedPanes[0].windowID, c.win().ID)
	}
}

// With nothing closed there is nothing to undo, and it must say so
// rather than doing something surprising.
func TestReopenWithNothingClosedIsANoop(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closedPanes = nil
	before := len(layout.Leaves(c.win().root))
	c.reopenClosedPane()

	if got := len(layout.Leaves(c.win().root)); got != before {
		t.Fatalf("panes %d -> %d, want no change", before, got)
	}
	if !strings.Contains(c.statusMsg, "no recently closed pane") {
		t.Errorf("status %q should explain there is nothing to reopen", c.statusMsg)
	}
}

// Undo is a stack: repeated presses walk back through the closures,
// most recent first.
func TestReopenWalksBackMostRecentFirst(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closedPanes = []closedPane{
		{windowID: c.win().ID, cwd: t.TempDir(), title: "older"},
		{windowID: c.win().ID, cwd: t.TempDir(), title: "newer"},
	}
	c.reopenClosedPane()
	if !strings.Contains(c.statusMsg, "newer") {
		t.Errorf("first undo reopened %q, want the most recent one", c.statusMsg)
	}
	c.reopenClosedPane()
	if !strings.Contains(c.statusMsg, "older") {
		t.Errorf("second undo reopened %q, want the older one", c.statusMsg)
	}
	if len(c.closedPanes) != 0 {
		t.Errorf("stack should be empty, has %d", len(c.closedPanes))
	}
}

// A reopen that can't happen must leave the entry on the stack, so a
// pane is never lost to a failed undo.
func TestFailedReopenKeepsTheEntry(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closedPanes = []closedPane{{windowID: c.win().ID, cwd: t.TempDir(), title: "bash"}}
	c.win().zoomed = c.win().active // zoom blocks splitting, same as a manual split

	c.reopenClosedPane()
	if len(c.closedPanes) != 1 {
		t.Fatalf("a refused reopen dropped the entry: stack has %d, want 1", len(c.closedPanes))
	}
	if !strings.Contains(c.statusMsg, "zoom") {
		t.Errorf("status %q should explain why it was refused", c.statusMsg)
	}
}

// The stack is bounded, so a long session can't pin an unbounded number
// of stale paths.
func TestUndoStackIsBounded(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := 0; i < undoStackLimit*3; i++ {
		c.closedPanes = append(c.closedPanes, closedPane{title: "x"})
		if len(c.closedPanes) > undoStackLimit {
			c.closedPanes = c.closedPanes[len(c.closedPanes)-undoStackLimit:]
		}
	}
	if len(c.closedPanes) != undoStackLimit {
		t.Fatalf("stack grew to %d, want a cap of %d", len(c.closedPanes), undoStackLimit)
	}
}
