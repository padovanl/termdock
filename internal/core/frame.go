package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/vt10x"
)

// Frame snapshots everything an attached client needs to paint: it never
// touches a real terminal itself, which is what lets any number of
// clients attach, detach and reattach to the same running session.
func (c *Core) Frame() proto.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()

	f := proto.Frame{
		Cols:        c.cols,
		Rows:        c.rows,
		ShowStatus:  c.statusRows() > 0,
		SessionName: c.SessionName,
	}

	if len(c.windows) == 0 {
		// The session's last window just closed (Exited() is about to
		// fire, or just did): nothing left to render. Guards a real race
		// against the periodic clock broadcast, which isn't otherwise
		// synchronized with window teardown.
		return f
	}

	w := c.win()
	if w.zoomed != nil {
		f.Panes = append(f.Panes, c.buildPaneFrame(w.zoomed, true, 1, true))
	} else {
		for i, leaf := range layout.Leaves(w.root) {
			f.Panes = append(f.Panes, c.buildPaneFrame(leaf, leaf == w.active, i+1, false))
		}
	}

	f.StatusPrefix = c.statusPrefix()
	f.Windows = c.windowTabs()
	f.StatusText, f.StatusRight, f.StatusStyle = c.statusLine()
	for _, provider := range []func() *proto.Overlay{
		c.pickerOverlay, c.helpOverlay, c.settingsOverlay, c.registersOverlay, c.sessionsOverlay, c.searchOverlay, c.openerOverlay,
	} {
		if f.Overlay = provider(); f.Overlay != nil {
			break
		}
	}
	f.Overview = c.overviewFrame()
	f.Popup = c.popupFrame()
	f.QuickJump = c.quickJumpFrame()
	f.Settings = c.clientSettings()
	return f
}

// idx is the pane's 1-based position within its window (left-to-right,
// top-to-bottom), not its internal ID: the ID is a session-wide counter
// that never resets or reuses a number, so after a bit of splitting and
// closing panes it stops meaning anything spatial ("why is this pane
// called 7 when there are only two panes?"). idx is what's shown in the
// title bar and status line instead — small, stable-within-the-window,
// and lines up with what the user can actually see and count.
func (c *Core) buildPaneFrame(n *layout.Node, active bool, idx int, zoomed bool) proto.PaneFrame {
	pf := proto.PaneFrame{
		ID:     n.ID,
		Rect:   proto.Rect(n.Rect),
		Active: active,
		Zoomed: zoomed,
	}
	p, ok := c.panes[n.ID]
	if !ok {
		return pf
	}
	pf.Title = c.paneTitle(idx, p)
	if zoomed {
		pf.Title += " [Z]"
	}
	if _, logging := p.LogPath(); logging {
		pf.Title += " [REC]"
	}

	if active && c.mode == ModeCopy && c.copy.paneID == n.ID {
		return c.buildCopyFrame(n, p, pf)
	}

	cr := n.Rect
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()

	cells := make([][]proto.Cell, cr.H)
	for y := 0; y < cr.H; y++ {
		row := make([]proto.Cell, cr.W)
		if y < rows {
			for x := 0; x < cr.W && x < cols; x++ {
				row[x] = glyphToCell(t.Cell(x, y))
			}
		}
		cells[y] = row
	}
	pf.Cells = cells

	if active {
		pf.CursorVisible = t.CursorVisible()
		cur := t.Cursor()
		pf.CursorX = cr.X + cur.X
		pf.CursorY = cr.Y + cur.Y
	}
	return pf
}

func (c *Core) buildCopyFrame(n *layout.Node, p *pane.Pane, pf proto.PaneFrame) proto.PaneFrame {
	cr := n.Rect
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, _ := t.Size()
	hl := t.HistoryLen()

	cells := make([][]proto.Cell, cr.H)
	for y := 0; y < cr.H; y++ {
		absY := c.copy.top + y
		row := make([]proto.Cell, cr.W)
		for x := 0; x < cr.W && x < cols; x++ {
			cell := glyphToCell(cellAt(t, hl, absY, x))
			if c.selected(x, absY) {
				cell.Attr |= pane.AttrReverse
			}
			row[x] = cell
		}
		cells[y] = row
	}
	pf.Cells = cells
	// The copy cursor doesn't have to be on screen: the wheel scrolls the
	// viewport and deliberately leaves the cursor where it was, so it can
	// easily sit above or below what's currently shown. Drawing it anyway
	// put the terminal's real cursor outside this pane's rect — over a
	// neighboring pane, or on the status bar — so it's simply hidden while
	// it's out of view, the same as any editor scrolled away from its
	// caret.
	row := c.copy.curY - c.copy.top
	pf.CursorVisible = row >= 0 && row < cr.H
	pf.CursorX = cr.X + clampi(c.copy.curX, 0, maxi(0, cr.W-1))
	pf.CursorY = cr.Y + row
	return pf
}

func glyphToCell(g vt10x.Glyph) proto.Cell {
	return proto.Cell{Ch: g.Char, FG: uint32(g.FG), BG: uint32(g.BG), Attr: uint16(g.Mode)}
}

func (c *Core) paneTitle(idx int, p *pane.Pane) string {
	if fg := p.ForegroundTitle(); fg != "" {
		return fmt.Sprintf("%d:%s", idx, fg)
	}
	return fmt.Sprintf("%d:%s", idx, c.shellName)
}

// cheatSheet is the terse keybinding cheat-sheet shown transiently
// while the prefix key is held — "Ctrl-B ?" is what the idle status
// bar tells you to press instead, for the full, one-line-per-binding
// reference (see help.go). Built from the session's actual current
// bindings (defaults, overridden per-key by config's "bind" setting)
// rather than a hardcoded string, so a rebound key shows up correctly
// here too instead of the cheat-sheet quietly lying about what a key
// does. hjkl/arrows and o/Tab are shown grouped only when all of a
// group's keys are still on their defaults — a partial rebind (say,
// just 'h' moved elsewhere) falls back to listing each surviving
// binding on its own instead of a group label that would no longer be
// accurate.
func (c *Core) cheatSheet() string {
	var parts []string
	if movementIsDefault(c.bindings) {
		parts = append(parts, "hjkl/arrows move")
	} else {
		for _, act := range []action{actFocusLeft, actFocusDown, actFocusUp, actFocusRight} {
			for _, r := range keysForAction(c.bindings, act) {
				parts = append(parts, keyLabel(r)+" "+actionShort[act])
			}
		}
	}
	if cycleIsDefault(c.bindings) {
		parts = append(parts, "o/Tab cycle")
	} else {
		for _, r := range keysForAction(c.bindings, actCycleFocus) {
			parts = append(parts, keyLabel(r)+" cycle")
		}
	}
	for _, act := range actionOrder {
		switch act {
		case actFocusLeft, actFocusRight, actFocusUp, actFocusDown, actCycleFocus:
			continue // handled above, grouped when possible
		}
		keys := keysForAction(c.bindings, act)
		if len(keys) == 0 {
			continue
		}
		labels := make([]string, len(keys))
		for i, r := range keys {
			labels[i] = keyLabel(r)
		}
		parts = append(parts, strings.Join(labels, "/")+" "+actionShort[act])
	}
	parts = append(parts, "0-9 win#")
	return strings.Join(parts, " | ")
}

func movementIsDefault(b map[rune]action) bool {
	return b['h'] == actFocusLeft && b['j'] == actFocusDown && b['k'] == actFocusUp && b['l'] == actFocusRight
}

func cycleIsDefault(b map[rune]action) bool {
	return b['o'] == actCycleFocus
}

func (c *Core) statusLine() (text, right, style string) {
	// Minimal at rest — like tmux's default status line, not a permanent
	// cheat-sheet — so there's room left for the clock; the full binding
	// list only shows up once you've actually pressed the prefix, when
	// you're presumably looking for it.
	hint := "Ctrl-B ?"
	style = "normal"

	switch {
	case c.mode == ModeInput:
		style = "mode"
		hint = c.input.prompt + string(c.input.buffer) + "_"
	case c.mode == ModeCopy || c.mode == ModeResize:
		style = "mode"
		hint = c.statusMsg
	case c.mode == ModeConfirm:
		style = "confirm"
		hint = c.statusMsg
	case c.mode == ModePicker:
		style = "mode"
		hint = "type to filter, ↑↓/Tab select, enter jump, esc cancel"
	case c.mode == ModeHelp:
		style = "mode"
		hint = "↑↓/jk/PgUp/PgDn scroll, any other key closes"
	case c.mode == ModeRegisters:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter paste, esc cancel"
	case c.mode == ModeSessions:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter switch, esc cancel"
	case c.mode == ModeSearch:
		style = "mode"
		hint = "type to search every pane's scrollback (regex or text), ↑↓ select, enter jump, esc cancel"
	case c.mode == ModeOverview:
		style = "mode"
		hint = "arrows/hjkl move, click or enter jump, esc cancel"
	case c.mode == ModePopup:
		style = "mode"
		hint = "typing goes to the popup — Ctrl-B P hides it, Ctrl-B d/q still work"
	case c.mode == ModeOpener:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter copies to clipboard, esc cancel"
	case c.mode == ModeSettings:
		style = "mode"
		hint = "↑↓ move, enter edit, S edit+save to the config file, esc close"
	case c.mode == ModeQuickJump:
		style = "mode"
		hint = "press a pane's number to jump there, any other key cancels"
	case c.prefix:
		style = "prefix"
		hint = "PREFIX > " + c.cheatSheet()
	case c.statusMsg != "":
		hint = c.statusMsg
	}

	w := c.win()
	sync := ""
	if w.syncPanes {
		sync = " [SYNC]"
	}
	text = fmt.Sprintf(" | active pane: %d%s | %s", activePaneIndex(w), sync, hint)

	right = time.Now().Format("15:04 02-Jan-06")
	if c.hostname != "" {
		right = c.hostname + " " + right
	}
	if seg := c.statusSegmentsText(); seg != "" {
		right = seg + " | " + right
	}
	right += " "
	return text, right, style
}

// activePaneIndex returns the active pane's 1-based position within its
// window (matching the number shown in its title bar; see
// Core.buildPaneFrame), or 1 when zoomed, since zoom always shows exactly
// the one pane.
func activePaneIndex(w *Window) int {
	if w.zoomed != nil {
		return 1
	}
	for i, l := range layout.Leaves(w.root) {
		if l == w.active {
			return i + 1
		}
	}
	return 1
}

// statusPrefix is the fixed lead-in text on the status bar's left side,
// drawn before the window tab strip.
func (c *Core) statusPrefix() string {
	return fmt.Sprintf(" termdock:%s ", c.SessionName)
}

// windowTabs lays out the status bar's window tab strip, one WindowTab per
// window with its exact display label and the column range it occupies —
// the single source of truth for both how the client draws it (colored per
// active/activity state) and how a click on it is hit-tested back to a
// window index (see handleNormalMouse), so the two can never drift apart.
// "*" marks the window you're looking at, "!" marks one that produced
// output while you weren't — tmux's monitor-activity, on by default here
// since there's no window list UI to toggle it from otherwise.
func (c *Core) windowTabs() []proto.WindowTab {
	tabs := make([]proto.WindowTab, len(c.windows))
	x := len([]rune(c.statusPrefix()))
	for i, w := range c.windows {
		label := fmt.Sprintf(" %d:%s", i, c.windowDisplayName(w))
		if w.activity {
			label += "!"
		}
		if i == c.activeWindow {
			label += "*"
		}
		label += " "
		width := len([]rune(label))
		tabs[i] = proto.WindowTab{
			Index:    i,
			Label:    label,
			Active:   i == c.activeWindow,
			Activity: w.activity,
			Dragging: c.tabDrag != nil && c.tabDrag.win == w,
			X:        x,
			W:        width,
		}
		x += width
	}
	return tabs
}
