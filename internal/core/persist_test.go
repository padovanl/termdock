package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/persist"
)

func TestSessionSurvivesRestartViaSnapshot(t *testing.T) {
	name := "test-restart-" + t.Name()

	c1, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1.mu.Lock()
	c1.doSplit(layout.Vertical)   // window 0: 2 panes side by side
	c1.doSplit(layout.Horizontal) // window 0: 3 panes (right side also split)
	c1.newWindowOpts("logs", "")  // window 1: 1 pane, explicitly named
	c1.mu.Unlock()

	// New(), called again with the same session name, should find the
	// snapshot newWindowOpts/doSplitIn already wrote (persistStateLocked
	// runs after every structural change — see persist.go) and rebuild
	// from it instead of starting fresh. Deliberately read it back
	// *before* touching c1's panes at all: closing them asynchronously
	// triggers their own detach/re-persist (handlePaneExit -> ... ->
	// persistStateLocked) on the pump goroutines, which would race with
	// — and could overwrite — the very snapshot this is trying to
	// verify. This is simulating a crash (the snapshot surviving because
	// nothing got a chance to clean up), not a graceful shutdown, so
	// there's nothing to wait for here in the first place.
	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (restore): %v", err)
	}
	defer closeAllPanes(c2)
	defer closeAllPanes(c1)

	if len(c2.windows) != 2 {
		t.Fatalf("expected 2 restored windows, got %d", len(c2.windows))
	}
	w0, w1 := c2.windows[0], c2.windows[1]
	if got := len(layout.Leaves(w0.root)); got != 3 {
		t.Errorf("window 0: expected 3 restored panes, got %d", got)
	}
	if w0.root.Split != layout.Vertical {
		t.Errorf("window 0 root split = %v, want Vertical", w0.root.Split)
	}
	if got := len(layout.Leaves(w1.root)); got != 1 {
		t.Errorf("window 1: expected 1 restored pane, got %d", got)
	}
	if !w1.renamed || w1.Name != "logs" {
		t.Errorf("window 1 = %+v, want renamed=true Name=logs", w1)
	}

	// Every restored leaf must actually have a live pane registered
	// (buildNodeFromSnapshot's whole point is to relaunch real shells,
	// not just reconstruct the tree shape).
	for _, w := range c2.windows {
		for _, l := range layout.Leaves(w.root) {
			if _, ok := c2.panes[l.ID]; !ok {
				t.Errorf("restored leaf id=%d has no live pane in c2.panes", l.ID)
			}
		}
	}
}

func TestGracefulShutdownDeletesSnapshot(t *testing.T) {
	name := "test-graceful-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := persist.Load(name); !ok {
		t.Fatal("expected a snapshot to exist right after New (newWindowOpts persists)")
	}

	c.Shutdown()

	if _, ok := persist.Load(name); ok {
		t.Fatal("Shutdown should delete the snapshot — a graceful end shouldn't resurrect itself")
	}
}

// TestQuitDeletesSnapshot is the regression test for the loudest bug of
// the lot: quitting a two-pane session and starting one with the same
// name again brought the two panes back instead of the fresh single pane
// a new session should have. Ctrl-B q runs requestQuit, which marks the
// session closed and *then* lets the server's Exited() watcher call
// Shutdown — and Shutdown used to return early on exactly that flag,
// skipping the snapshot delete that is its whole reason for existing.
func TestQuitDeletesSnapshot(t *testing.T) {
	name := "test-quit-deletes-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.confirmQuit()
	c.handleConfirmKey('y') // requestQuit: closes every pane, marks the session closed
	c.mu.Unlock()

	c.Shutdown() // what the server does once Exited() fires

	if _, ok := persist.Load(name); ok {
		t.Fatal("a confirmed quit must not leave a snapshot behind — the next session of this name would silently restore the layout just quit out of")
	}

	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (after quit): %v", err)
	}
	defer closeAllPanes(c2)
	if n := len(layout.Leaves(c2.windows[0].root)); n != 1 {
		t.Fatalf("a session started after quitting should open with 1 pane, got %d", n)
	}
}

// TestNoSnapshotWrittenAfterClose covers the other half of that fix.
// Closing panes makes their pump goroutines run handlePaneExit, which
// ends up back in persistStateLocked — so without a guard, a late exit
// can write the snapshot straight back out after Shutdown deleted it.
func TestNoSnapshotWrittenAfterClose(t *testing.T) {
	name := "test-late-persist-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.mu.Unlock()

	c.Shutdown()

	c.mu.Lock()
	c.persistStateLocked() // stands in for a pump goroutine getting here late
	c.mu.Unlock()
	c.PersistState() // and for the server's periodic snapshot racing shutdown

	if _, ok := persist.Load(name); ok {
		t.Fatal("nothing should be able to write a snapshot once the session is closed")
	}
}

func TestFreshSessionWhenNoSnapshot(t *testing.T) {
	c := newTestCore(t)

	if len(c.windows) != 1 || len(layout.Leaves(c.windows[0].root)) != 1 {
		t.Fatalf("expected the normal single-window single-pane default, got %d windows", len(c.windows))
	}
}
