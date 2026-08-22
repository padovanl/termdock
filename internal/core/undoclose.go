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
	name     string // the name the user gave it, if any
	title    string // what to call it in the status line when it comes back
	// split is how the pane sat relative to its sibling. Reopening
	// picked an axis from the target's aspect ratio instead, which meant
	// a pane you closed from a stacked layout came back beside its
	// neighbour — the undo visibly not undoing.
	split layout.SplitType
}

// recordClosedPane pushes n's whereabouts onto the undo stack. Called
// from detachLeafIn just before the pane leaves the tree — the last
// moment its working directory can still be read off the live process.
func (c *Core) recordClosedPane(w *Window, n *layout.Node) {
	entry := closedPane{
		title: c.pickerPaneTitle(n.ID),
		name:  c.paneNames[n.ID],
	}
	// The split that placed it: its parent's, which is the one that will
	// have to be recreated to put it back where it was.
	if n.Parent != nil {
		entry.split = n.Parent.Split
	}
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
	// Put it back on the axis it was on. Falling back to the shape of the
	// target pane only when nothing was recorded (a pane that was the
	// window's only one, so had no parent split to remember).
	st := entry.split
	if st != layout.Vertical && st != layout.Horizontal {
		st = layout.Vertical
		if r := target.active.Rect; r.H > r.W/2 {
			st = layout.Horizontal
		}
	}
	newLeaf, ok := layout.Split(target.active, st, id, p)
	if !ok {
		p.Close()
		c.statusMsg = "not enough room to reopen that pane"
		return
	}

	c.closedPanes = c.closedPanes[:len(c.closedPanes)-1]
	c.panes[id] = p
	if entry.name != "" {
		if c.paneNames == nil {
			c.paneNames = map[int]string{}
		}
		c.paneNames[id] = entry.name
	}
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
