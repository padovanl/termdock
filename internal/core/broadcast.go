package core

import (
	"fmt"

	"github.com/padovanl/termdock/internal/layout"
)

// Typing into several panes at once is the thing you reach for when
// you're driving a handful of machines together: the same tail, the same
// deploy, the same "why is this one different". tmux has it as
// synchronize-panes, and so did termdock — but only as all-or-nothing
// over a window, which is a poor fit for the actual job. The pane
// running your editor, or the one holding the output you are comparing
// against, is exactly the one that must *not* receive the keystrokes,
// and being forced to move it to another window first is enough friction
// that the feature goes unused.
//
// So the set is choosable. Ctrl-B y still toggles the whole window, as
// before; inside the overview (Ctrl-B g), space adds or removes the pane
// under the cursor, and the tiles say which are in. An empty set means
// "all of them", so the simple case stays simple and the old behaviour
// is unchanged.

// broadcastTargets is which panes of w a keystroke should reach.
func (c *Core) broadcastTargets(w *Window) []*layout.Node {
	leaves := layout.Leaves(w.root)
	if len(w.syncOnly) == 0 {
		return leaves // no selection: the whole window, as before
	}
	var out []*layout.Node
	for _, l := range leaves {
		if w.syncOnly[l.ID] {
			out = append(out, l)
		}
	}
	return out
}

// inBroadcast reports whether a pane receives synchronized input, for
// marking it in the overview and in its title.
func (c *Core) inBroadcast(w *Window, paneID int) bool {
	if !w.syncPanes {
		return false
	}
	return len(w.syncOnly) == 0 || w.syncOnly[paneID]
}

// toggleBroadcastPane adds or removes a pane from the set, from the
// overview. Turning sync on implicitly if it wasn't: picking panes to
// broadcast to and then finding nothing happens because a separate
// switch was off would be a small, annoying puzzle.
func (c *Core) toggleBroadcastPane(w *Window, paneID int) {
	if w.syncOnly == nil {
		w.syncOnly = map[int]bool{}
	}
	if w.syncOnly[paneID] {
		delete(w.syncOnly, paneID)
	} else {
		w.syncOnly[paneID] = true
	}
	// An empty set means "everything", which is not what someone who
	// just deselected their last pane meant. Turn sync off instead.
	if len(w.syncOnly) == 0 {
		w.syncPanes = false
		c.statusMsg = "synchronized input: off"
		return
	}
	w.syncPanes = true
	c.statusMsg = fmt.Sprintf("synchronized input: %d of %d panes", len(w.syncOnly), len(layout.Leaves(w.root)))
}

// pruneBroadcast drops panes that have closed, so a set does not quietly
// keep naming panes that no longer exist and reappear as a stale count.
func (c *Core) pruneBroadcast(w *Window) {
	if len(w.syncOnly) == 0 {
		return
	}
	live := map[int]bool{}
	for _, l := range layout.Leaves(w.root) {
		live[l.ID] = true
	}
	for id := range w.syncOnly {
		if !live[id] {
			delete(w.syncOnly, id)
		}
	}
	if len(w.syncOnly) == 0 {
		w.syncPanes = false
	}
}

// broadcastLabel is what the status bar shows: plain [SYNC] for a whole
// window, and the fraction when only some panes are in, because "you are
// typing into three of these seven" is exactly the thing you want
// confirmed before you press Enter.
func (c *Core) broadcastLabel(w *Window) string {
	if !w.syncPanes {
		return ""
	}
	if len(w.syncOnly) == 0 {
		return " [SYNC]"
	}
	return fmt.Sprintf(" [SYNC %d/%d]", len(w.syncOnly), len(layout.Leaves(w.root)))
}
