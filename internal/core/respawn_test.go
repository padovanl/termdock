package core

import (
	"testing"

	"termdock/internal/layout"
)

func TestRespawnActivePaneReplacesProcessKeepsLayout(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leafCountBefore := len(layout.Leaves(c.win().root))
	oldID := c.win().active.ID
	c.mu.Unlock()

	writeAndWaitEcho(t, c, oldID, "echo before-respawn-marker")

	c.mu.Lock()
	c.respawnActivePane()
	newID := c.win().active.ID
	leafCountAfter := len(layout.Leaves(c.win().root))
	_, oldStillTracked := c.panes[oldID]
	c.mu.Unlock()

	if newID == oldID {
		t.Fatal("respawn should assign the pane a fresh ID")
	}
	if leafCountAfter != leafCountBefore {
		t.Fatalf("respawn changed the pane count: had %d, now %d", leafCountBefore, leafCountAfter)
	}
	if oldStillTracked {
		t.Fatal("the old pane's process should no longer be tracked after respawn")
	}

	// The new pane must be a genuinely fresh, working shell: writing to
	// it and seeing an echo confirms it's alive, not a placeholder stub.
	writeAndWaitEcho(t, c, newID, "echo after-respawn-marker")
}

func TestRespawnActivePaneKeepsWindowShapeAndZoom(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.toggleZoom()
	zoomedBefore := c.win().zoomed != nil
	c.respawnActivePane()
	zoomedAfter := c.win().zoomed != nil
	leafCount := len(layout.Leaves(c.win().root)) // zoom only hides visually; the tree still has both
	c.mu.Unlock()

	if !zoomedBefore || !zoomedAfter {
		t.Fatalf("zoom state should survive a respawn: before=%v after=%v", zoomedBefore, zoomedAfter)
	}
	if leafCount != 2 {
		t.Fatalf("respawn must not alter the window's pane count, got %d", leafCount)
	}
}

func TestRespawnActivePaneOnlyTouchesTheActiveOne(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	otherID := leaves[0].ID // not the active one (doSplit leaves the new/right pane active)
	c.respawnActivePane()
	_, otherStillTracked := c.panes[otherID]
	c.mu.Unlock()

	if !otherStillTracked {
		t.Fatal("respawning the active pane must not touch the other pane in the window")
	}
}
