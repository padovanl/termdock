package core

import "github.com/padovanl/termdock/internal/pane"

// respawnActivePane kills whatever's running in the active pane (if
// still alive) and starts a fresh shell in its place — tmux's
// respawn-pane, bound to Ctrl-B R. The pane's layout slot, size, and
// position are all untouched; only the process and its pane ID change,
// the same "process replaced, layout doesn't move" idea as
// breakPaneToNewWindow/movePaneToWindow apply to whole panes. Useful
// when a shell hangs, an SSH connection drops, or you just want to reset
// a REPL without tearing down the split around it.
func (c *Core) respawnActivePane() {
	w := c.win()
	leaf := w.active

	id := pane.NextID()
	p, err := pane.New(id, 80, 24) // placeholder size; relayoutLocked below resizes it to the leaf's real Rect
	if err != nil {
		c.statusMsg = "respawn error: " + err.Error()
		return
	}

	oldID := leaf.ID
	if old, ok := c.panes[oldID]; ok {
		old.Close()
		delete(c.panes, oldID)
		delete(c.paneLastActive, oldID)
	}
	// Respawning is the one operation that changes a live pane's id while
	// it stays in the same slot, so anything remembering a pane *by id*
	// has to come along. Ctrl-B ;'s target is the one that does: left
	// behind, it named a pane that no longer exists anywhere, and the
	// toggle silently stopped working.
	if w.lastActivePane == oldID {
		w.lastActivePane = id
	}

	leaf.ID = id
	leaf.Pane = p
	c.panes[id] = p
	c.touchPane(id)

	c.relayoutLocked()
	c.startPump(p)
	c.persistStateLocked()
	c.statusMsg = "respawned"
}
