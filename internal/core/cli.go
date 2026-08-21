package core

import (
	"errors"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
)

// This file implements termdock's scripting surface — the equivalent of
// tmux's command interface (send-keys, new-window, split-window, ...),
// driven from the CLI rather than a live client. Each method acquires
// its own lock, unlike the handleKey/handleMouse family which assume
// Core.mu is already held: these are called directly from the server's
// per-connection goroutine, outside any attach loop.
//
// Every "windowIdx, windowName" pair follows the same convention as
// resolveWindow: idx >= 0 wins if given, else name is looked up, else it
// means the active window.

// CLISendKeys writes text (and, if enter, a carriage return) straight to
// the target pane, exactly as if it had been typed.
func (c *Core) CLISendKeys(windowIdx int, windowName string, paneIndex int, text string, enter bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.resolveTargetPane(windowIdx, windowName, paneIndex)
	if !ok {
		return errors.New("no such pane")
	}
	p.Write([]byte(text))
	if enter {
		p.Write([]byte("\r"))
	}
	c.markDirty()
	return nil
}

// CLINewWindow creates a window (optionally named, optionally running
// command instead of the shell) and returns its index.
func (c *Core) CLINewWindow(name, command string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.newWindowOpts(name, command); err != nil {
		return 0, err
	}
	c.markDirty()
	return c.activeWindow, nil
}

// CLISplitWindow splits a pane in the target window — the one paneIndex
// names (1-based, the number in its title bar), or the window's active
// one when that's 0 — and returns the new pane's ID. paneIndex used to be
// parsed off the TARGET and then dropped on the floor here, so the
// README's own `-t main:dev.1` example quietly split whatever pane
// happened to be active instead of pane 1.
func (c *Core) CLISplitWindow(windowIdx int, windowName string, paneIndex int, axis layout.SplitType, command string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return 0, errors.New("no such window")
	}
	target, ok := paneAt(w, paneIndex)
	if !ok {
		return 0, errors.New("no such pane in that window")
	}
	id, err := c.doSplitLeafIn(w, target, axis, command)
	if err != nil {
		return 0, err
	}
	c.markDirty()
	return id, nil
}

// CLISelectWindow makes the target window the active (visible) one.
func (c *Core) CLISelectWindow(windowIdx int, windowName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return errors.New("no such window")
	}
	c.selectWindowIndex(c.windowIndex(w))
	c.markDirty()
	return nil
}

// CLISelectPane moves the target window's focus one step in a spatial
// direction ("L"/"R"/"U"/"D") — the piece external tooling (a
// vim-tmux-navigator-style plugin, say) needs to move pane focus by
// direction without an interactive client attached, the same way
// CLISelectWindow already lets a script pick a window by index/name.
// Not an error if there's simply nothing in that direction (a no-op,
// same as tmux's own select-pane -D at the edge of a layout) — only an
// unknown window or an unrecognized direction letter is.
func (c *Core) CLISelectPane(windowIdx int, windowName string, direction string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return errors.New("no such window")
	}
	var dx, dy int
	switch direction {
	case "L":
		dx = -1
	case "R":
		dx = 1
	case "U":
		dy = -1
	case "D":
		dy = 1
	default:
		return errors.New("direction must be one of L, R, U, D")
	}
	c.moveFocusIn(w, dx, dy)
	c.markDirty()
	return nil
}

// CLIListWindows summarizes every window in the session.
func (c *Core) CLIListWindows() []proto.WindowInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]proto.WindowInfo, len(c.windows))
	for i, w := range c.windows {
		out[i] = proto.WindowInfo{
			Index:  i,
			Name:   c.windowDisplayName(w),
			Active: i == c.activeWindow,
			Panes:  len(layout.Leaves(w.root)),
		}
	}
	return out
}

// CLIListPanes summarizes every pane in the target window.
func (c *Core) CLIListPanes(windowIdx int, windowName string) ([]proto.PaneInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return nil, errors.New("no such window")
	}
	var out []proto.PaneInfo
	for i, l := range layout.Leaves(w.root) {
		info := proto.PaneInfo{Index: i + 1, ID: l.ID, Active: l == w.active}
		if p, ok := c.panes[l.ID]; ok {
			// Positional index, not l.ID: paneTitle renders the number
			// the pane shows in its own title bar on screen (see
			// buildPaneFrame), and passing the ID here made list-panes
			// disagree with what the user is looking at — "3:bash" for
			// the pane labelled 2:bash — as soon as closing and
			// splitting had let IDs drift away from positions.
			info.Title = c.paneTitle(i+1, p)
		}
		out = append(out, info)
	}
	return out, nil
}

// paneAt returns w's index-th pane (1-based, matching the number in its
// title bar and PaneInfo.Index), or w's active pane when index is 0.
func paneAt(w *Window, index int) (*layout.Node, bool) {
	if index == 0 {
		return w.active, true
	}
	leaves := layout.Leaves(w.root)
	if index < 1 || index > len(leaves) {
		return nil, false
	}
	return leaves[index-1], true
}

// resolveTargetPane finds the pane a scripting command should act on: the
// paneIndex-th pane of the target window (1-based, the number in its
// title bar), or that window's active pane when paneIndex is 0.
//
// This used to treat the number as a session-wide internal pane ID and
// look it up in c.panes directly, which ignored the window part of the
// TARGET entirely — "-t main:1.2" could act on a pane in window 0 — and
// meant the number to pass bore no relation to any number shown on
// screen.
func (c *Core) resolveTargetPane(windowIdx int, windowName string, paneIndex int) (*pane.Pane, bool) {
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return nil, false
	}
	n, ok := paneAt(w, paneIndex)
	if !ok {
		return nil, false
	}
	p, ok := c.panes[n.ID]
	return p, ok
}
