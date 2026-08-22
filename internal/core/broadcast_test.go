package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func fourPanes(t *testing.T) (*Core, []int) {
	t.Helper()
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	ids := make([]int, len(leaves))
	for i, l := range leaves {
		ids[i] = l.ID
	}
	c.mu.Unlock()
	return c, ids
}

// The old behaviour has to survive untouched: no selection means the
// whole window, which is what everyone using it today expects.
func TestBroadcastWithNoSelectionReachesEveryPane(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	w.syncPanes = true
	if got := len(c.broadcastTargets(w)); got != len(ids) {
		t.Fatalf("targets = %d, want all %d panes", got, len(ids))
	}
}

// The point of the feature: the pane holding the output you are
// comparing against must be able to stay out of it.
func TestBroadcastReachesOnlyTheChosenPanes(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	c.toggleBroadcastPane(w, ids[0])
	c.toggleBroadcastPane(w, ids[2])

	targets := c.broadcastTargets(w)
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	got := map[int]bool{}
	for _, l := range targets {
		got[l.ID] = true
	}
	if !got[ids[0]] || !got[ids[2]] {
		t.Errorf("targets %v, want exactly panes %d and %d", got, ids[0], ids[2])
	}
	if got[ids[1]] || got[ids[3]] {
		t.Error("a pane that was never selected is receiving input")
	}
}

// Picking panes turns synchronization on by itself: selecting targets
// and then finding nothing happens because a separate switch was off is
// a small, annoying puzzle.
func TestPickingAPaneEnablesSync(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	if w.syncPanes {
		t.Fatal("sync should start off")
	}
	c.toggleBroadcastPane(w, ids[0])
	if !w.syncPanes {
		t.Error("choosing a pane should turn synchronized input on")
	}
}

// Deselecting the last one means "stop", not "send to everything" —
// which is what an empty set would otherwise be read as.
func TestDeselectingTheLastPaneTurnsSyncOff(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	c.toggleBroadcastPane(w, ids[0])
	c.toggleBroadcastPane(w, ids[0]) // and off again

	if w.syncPanes {
		t.Error("with nothing selected, sync should be off rather than reaching every pane")
	}
	if !strings.Contains(c.statusMsg, "off") {
		t.Errorf("status %q should say it stopped", c.statusMsg)
	}
}

// The status bar must say how many, because "you are typing into three
// of these seven" is exactly what you want confirmed before pressing
// Enter.
func TestStatusShowsTheBroadcastFraction(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	c.toggleBroadcastPane(w, ids[0])
	c.toggleBroadcastPane(w, ids[1])

	if got := c.broadcastLabel(w); !strings.Contains(got, "2/4") {
		t.Errorf("status label = %q, want it to show 2 of 4", got)
	}

	// A whole-window sync stays the plain marker it always was.
	w.syncOnly = nil
	w.syncPanes = true
	if got := c.broadcastLabel(w); got != " [SYNC]" {
		t.Errorf("whole-window label = %q, want the plain marker", got)
	}
}

// A closed pane must drop out of the set, or the count goes on naming
// something that no longer exists.
func TestClosingAPaneRemovesItFromTheBroadcastSet(t *testing.T) {
	c, ids := fourPanes(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	w := c.win()
	c.toggleBroadcastPane(w, ids[0])
	c.toggleBroadcastPane(w, ids[1])

	leaves := layout.Leaves(w.root)
	var victim *layout.Node
	for _, l := range leaves {
		if l.ID == ids[1] {
			victim = l
		}
	}
	c.detachLeafIn(w, victim)

	if w.syncOnly[ids[1]] {
		t.Error("a closed pane is still in the broadcast set")
	}
	if got := len(c.broadcastTargets(w)); got != 1 {
		t.Errorf("targets = %d, want just the surviving one", got)
	}
}
