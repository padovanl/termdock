package core

import (
	"fmt"

	"termdock/internal/layout"
	"termdock/internal/pane"
)

// Window is one tab: its own independent pane layout. Panes themselves
// live in Core.panes, keyed by a session-wide unique ID; a window's
// layout tree just references the ones that belong to it, the same way
// tmux windows each have their own pane tree within one session.
type Window struct {
	ID      int
	Name    string
	renamed bool // true once the user has explicitly set Name

	root, active, zoomed *layout.Node
	syncPanes            bool

	activity bool // output arrived while this window wasn't the active one
}

// win returns the active window. Every Core method that manipulates "the"
// pane tree operates on this one; findWindowAndLeaf is used instead for
// the few cases (an exiting pane, mouse coordinates) that might belong to
// a different, currently-hidden window.
func (c *Core) win() *Window {
	return c.windows[c.activeWindow]
}

func (c *Core) windowIndex(w *Window) int {
	for i, ww := range c.windows {
		if ww == w {
			return i
		}
	}
	return -1
}

// resolveWindow returns the window at idx; if idx < 0 and name is set, it
// looks up name against each window's display name instead (so scripting
// commands can target a window by the name it was created with, not just
// its numeric index); if both are unset, it's the active window.
func (c *Core) resolveWindow(idx int, name string) (*Window, bool) {
	if idx >= 0 {
		if idx >= len(c.windows) {
			return nil, false
		}
		return c.windows[idx], true
	}
	if name != "" {
		for _, w := range c.windows {
			if c.windowDisplayName(w) == name {
				return w, true
			}
		}
		return nil, false
	}
	if len(c.windows) == 0 {
		return nil, false // session mid-shutdown; a lingering connection raced us here
	}
	return c.win(), true
}

func (c *Core) findWindowAndLeaf(paneID int) (*Window, *layout.Node) {
	for _, w := range c.windows {
		if l := findLeafByID(w.root, paneID); l != nil {
			return w, l
		}
	}
	return nil, nil
}

// windowDisplayName returns what the status bar shows for w: its custom
// name if the user set one (Ctrl-B ,), otherwise the foreground command
// of its active pane — mirroring tmux's automatic-rename default.
func (c *Core) windowDisplayName(w *Window) string {
	if w.renamed {
		return w.Name
	}
	if p, ok := c.panes[w.active.ID]; ok {
		if fg := p.ForegroundTitle(); fg != "" {
			return fg
		}
	}
	return c.shellName
}

// newWindow creates a window with one fresh pane and makes it active.
// Only the very first call (from New) can fail outright; later calls
// (Ctrl-B c) just report the error in the status bar and leave the
// session as it was.
func (c *Core) newWindow() error {
	_, err := c.newWindowOpts("", "")
	return err
}

// newWindowOpts is newWindow with an optional custom name and an optional
// command to run instead of the interactive shell (used by the
// new-window CLI command; the interactive Ctrl-B c binding just passes
// "", "").
func (c *Core) newWindowOpts(name, command string) (*Window, error) {
	id := pane.NextID()
	p, err := pane.NewWithCommand(id, max(c.cols, 1), max(c.rows-c.statusRows(), 1), command)
	if err != nil {
		c.statusMsg = "error creating window: " + err.Error()
		return nil, err
	}
	root := layout.NewLeaf(id, p)
	w := &Window{ID: c.nextWindowID, root: root, active: root}
	if name != "" {
		w.Name = name
		w.renamed = true
	}
	c.nextWindowID++

	c.panes[id] = p
	c.windows = append(c.windows, w)
	c.activeWindow = len(c.windows) - 1
	c.afterWindowSwitch()
	c.relayoutLocked()
	c.startPump(p)
	return w, nil
}

func (c *Core) switchWindow(delta int) {
	if len(c.windows) < 2 {
		return
	}
	n := len(c.windows)
	c.activeWindow = ((c.activeWindow+delta)%n + n) % n
	c.afterWindowSwitch()
}

func (c *Core) selectWindowIndex(i int) {
	if i < 0 || i >= len(c.windows) {
		return
	}
	c.activeWindow = i
	c.afterWindowSwitch()
}

// afterWindowSwitch clears state that's tied to whichever pane/window you
// were just looking at — copy-mode and an in-flight mouse drag don't
// carry over when you jump to a different window — and clears the
// activity flag on the window you just switched to, the same way tmux
// stops flagging a window the moment you look at it.
func (c *Core) afterWindowSwitch() {
	c.win().activity = false
	c.copy = copyState{}
	if c.mode == ModeCopy {
		c.mode = ModeNormal
	}
	c.drag = nil
	c.mouseDown = false
	c.lastTitleClickID = 0
}

// confirmKillWindow asks before killWindow actually runs: closing a
// window takes every pane in it down at once, with no undo, so — unlike
// closing a single pane with 'x' — it's worth one extra keypress to catch
// a stray 'x'-shaped typo. y/Y confirms, anything else (including Esc)
// cancels; see handleConfirmKey.
func (c *Core) confirmKillWindow() {
	w := c.win()
	n := len(layout.Leaves(w.root))
	plural := "s"
	if n == 1 {
		plural = ""
	}
	c.mode = ModeConfirm
	c.statusMsg = fmt.Sprintf("kill window %q and its %d pane%s? (y/n)", c.windowDisplayName(w), n, plural)
}

// killWindow closes every pane in the active window and removes it.
func (c *Core) killWindow() {
	w := c.win()
	for _, l := range layout.Leaves(w.root) {
		if p, ok := c.panes[l.ID]; ok {
			p.Close()
			delete(c.panes, l.ID)
		}
	}
	c.removeWindow(c.activeWindow)
}

// removeWindow drops window idx from the list (its panes must already be
// closed) and quits the session if that was the last one.
func (c *Core) removeWindow(idx int) {
	c.windows = append(c.windows[:idx], c.windows[idx+1:]...)
	if len(c.windows) == 0 {
		c.requestQuit()
		return
	}
	if c.activeWindow >= len(c.windows) {
		c.activeWindow = len(c.windows) - 1
	}
	c.afterWindowSwitch()
	c.relayoutLocked()
}
