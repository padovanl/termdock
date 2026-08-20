package core

import (
	"sort"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/proto"
)

// Switching sessions (Ctrl-B S) without detaching first: tmux needs
// choose-tree or a detach/reattach round-trip; here it's the same
// type-ahead picker as everything else. core itself has no way to
// discover sibling sessions — that's a filesystem/socket-directory
// concern that belongs to internal/server, and core deliberately
// doesn't import server (server already imports core, and Go doesn't
// allow the cycle) — so the list is supplied via the ListSessions func
// field, set once by server.Run.

type sessionPickerState struct {
	query    []rune
	names    []string // snapshotted on entry, like pickerState.items
	filtered []int
	sel      int
}

func (c *Core) enterSessionPicker() {
	if c.ListSessions == nil {
		c.statusMsg = "session switching isn't available"
		return
	}
	names := c.ListSessions()
	if len(names) == 0 {
		c.statusMsg = "no other sessions"
		return
	}
	c.mode = ModeSessions
	c.sessions = sessionPickerState{names: names}
	c.refilterSessions()
}

func (c *Core) handleSessionsKey(key tcell.Key, r rune) Result {
	switch {
	case key == tcell.KeyEsc:
		c.mode = ModeNormal
		c.sessions = sessionPickerState{}
	case key == tcell.KeyEnter:
		var target string
		if c.sessions.sel < len(c.sessions.filtered) {
			target = c.sessions.names[c.sessions.filtered[c.sessions.sel]]
		}
		c.mode = ModeNormal
		c.sessions = sessionPickerState{}
		if target != "" {
			return Result{SwitchSession: target}
		}
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.sessions.query); n > 0 {
			c.sessions.query = c.sessions.query[:n-1]
			c.refilterSessions()
		}
	case key == tcell.KeyCtrlU:
		c.sessions.query = c.sessions.query[:0]
		c.refilterSessions()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.moveSessionSel(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.moveSessionSel(1)
	case r != 0 && key == tcell.KeyRune:
		c.sessions.query = append(c.sessions.query, r)
		c.refilterSessions()
	}
	return Result{}
}

func (c *Core) moveSessionSel(delta int) {
	n := len(c.sessions.filtered)
	if n == 0 {
		return
	}
	c.sessions.sel = ((c.sessions.sel+delta)%n + n) % n
}

func (c *Core) refilterSessions() {
	query := string(c.sessions.query)
	type scored struct{ idx, at int }
	var matches []scored
	for i, name := range c.sessions.names {
		if ok, at := fuzzyMatch(query, name); ok {
			matches = append(matches, scored{i, at})
		}
	}
	sort.SliceStable(matches, func(a, b int) bool { return matches[a].at < matches[b].at })
	filtered := make([]int, len(matches))
	for i, m := range matches {
		filtered[i] = m.idx
	}
	c.sessions.filtered = filtered
	c.sessions.sel = clampi(c.sessions.sel, 0, maxi(0, len(filtered)-1))
}

func (c *Core) sessionsOverlay() *proto.Overlay {
	if c.mode != ModeSessions {
		return nil
	}
	items := make([]string, len(c.sessions.filtered))
	for i, idx := range c.sessions.filtered {
		items[i] = c.sessions.names[idx]
	}
	return &proto.Overlay{
		Title:      "switch session — type to filter, ↑↓ select, enter switch, esc cancel",
		ShowQuery:  true,
		Query:      string(c.sessions.query),
		Selectable: true,
		Items:      items,
		Selected:   c.sessions.sel,
	}
}
