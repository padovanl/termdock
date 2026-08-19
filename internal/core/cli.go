package core

import (
	"errors"

	"termdock/internal/layout"
	"termdock/internal/pane"
	"termdock/internal/proto"
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
func (c *Core) CLISendKeys(windowIdx int, windowName string, paneID int, text string, enter bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.resolveTargetPane(windowIdx, windowName, paneID)
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

// CLISplitWindow splits the target window's active pane and returns the
// new pane's ID.
func (c *Core) CLISplitWindow(windowIdx int, windowName string, axis layout.SplitType, command string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return 0, errors.New("no such window")
	}
	id, err := c.doSplitIn(w, axis, command)
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
	for _, l := range layout.Leaves(w.root) {
		info := proto.PaneInfo{ID: l.ID, Active: l == w.active}
		if p, ok := c.panes[l.ID]; ok {
			info.Title = c.paneTitle(l.ID, p)
		}
		out = append(out, info)
	}
	return out, nil
}

// resolveTargetPane finds the pane a scripting command should act on:
// paneID if given (> 0), else the target window's active pane.
func (c *Core) resolveTargetPane(windowIdx int, windowName string, paneID int) (*pane.Pane, bool) {
	if paneID > 0 {
		p, ok := c.panes[paneID]
		return p, ok
	}
	w, ok := c.resolveWindow(windowIdx, windowName)
	if !ok {
		return nil, false
	}
	p, ok := c.panes[w.active.ID]
	return p, ok
}
