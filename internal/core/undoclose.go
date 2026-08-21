package core

import (
	"fmt"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
)

// Closing a pane is the one destructive thing in a multiplexer that
// happens by accident constantly: a stray Ctrl-B x, or an `exit` typed
// into the wrong pane. Everything else here either asks first (killing a
// window, quitting) or is harmless to redo. So termdock keeps the last
// few closed panes on a stack and Ctrl-B Z brings one back — in the
// window it came from, in the directory it was sitting in.
//
// What comes back is a fresh shell, not the process that was running:
// nothing can resurrect that, the same honest limit session persistence
// has (see internal/persist). Recovering the *place* — right window,
// right directory — is the part that is actually tedious to redo by
// hand, and it is the part that can be recovered.
//
// tmux has no equivalent at all: a closed pane is simply gone.

// undoStackLimit caps how far back Ctrl-B Z can reach. Deep enough for
// the "wait, not that one" it exists for, shallow enough that it cannot
// quietly pin a lot of stale paths in memory.
const undoStackLimit = 16

// closedPane is where a pane was, so it can be put back there.
type closedPane struct {
	windowID int    // 0 if its window went too, in which case it reopens here
	cwd      string // "" means wherever the daemon started, same as a plain new pane
	title    string // what to call it in the status line when it comes back
}

// recordClosedPane pushes n's whereabouts onto the undo stack. Called
// from detachLeafIn just before the pane leaves the tree — the last
// moment its working directory can still be read off the live process.
func (c *Core) recordClosedPane(w *Window, n *layout.Node) {
	entry := closedPane{title: c.pickerPaneTitle(n.ID)}
	if w != nil {
		entry.windowID = w.ID
	}
	if p, ok := c.panes[n.ID]; ok {
		entry.cwd = p.Cwd()
	}
	c.closedPanes = append(c.closedPanes, entry)
	if len(c.closedPanes) > undoStackLimit {
		c.closedPanes = c.closedPanes[len(c.closedPanes)-undoStackLimit:]
	}
}

// reopenClosedPane is Ctrl-B Z: bring back the most recently closed pane.
func (c *Core) reopenClosedPane() {
	if len(c.closedPanes) == 0 {
		c.statusMsg = "no recently closed pane to reopen"
		return
	}
	entry := c.closedPanes[len(c.closedPanes)-1]

	// Back to its own window when that still exists; otherwise here,
	// which beats refusing outright — the directory is the valuable part
	// and it is still recoverable.
	target, windowGone := c.win(), false
	if w := c.windowByID(entry.windowID); w != nil {
		target = w
	} else if entry.windowID != 0 {
		windowGone = true
	}
	// Every failure below leaves the stack untouched, so a pane is never
	// lost to a reopen that could not happen.
	if target.zoomed != nil {
		// Same rule an ordinary split follows, and clearer than silently
		// un-zooming something the user is deliberately looking at.
		c.statusMsg = "exit zoom (prefix z) before reopening a pane"
		return
	}

	id := pane.NextID()
	p, err := pane.NewInDir(id, 80, 24, entry.cwd)
	if err != nil {
		c.statusMsg = "error reopening pane: " + err.Error()
		return
	}
	// Split whichever pane is active in the target window, picking the
	// axis that keeps the result closer to square rather than always
	// halving the same way.
	st := layout.Vertical
	if r := target.active.Rect; r.H > r.W/2 {
		st = layout.Horizontal
	}
	newLeaf, ok := layout.Split(target.active, st, id, p)
	if !ok {
		p.Close()
		c.statusMsg = "not enough room to reopen that pane"
		return
	}

	c.closedPanes = c.closedPanes[:len(c.closedPanes)-1]
	c.panes[id] = p
	c.setWindowActiveLeaf(target, newLeaf)
	if target != c.win() {
		if idx := c.windowIndex(target); idx >= 0 {
			c.setActiveWindowIndex(idx)
		}
	}
	c.relayoutLocked()
	c.startPump(p)
	c.persistStateLocked()

	where := entry.cwd
	if where == "" {
		where = "the default directory"
	}
	c.statusMsg = fmt.Sprintf("reopened %s in %s", entry.title, where)
	if windowGone {
		c.statusMsg += " (its own window is gone, so it came back here)"
	}
}

// windowByID finds a window by its stable id, or nil once it has closed.
func (c *Core) windowByID(id int) *Window {
	if id == 0 {
		return nil
	}
	for _, w := range c.windows {
		if w.ID == id {
			return w
		}
	}
	return nil
}
