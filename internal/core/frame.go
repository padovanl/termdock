package core

import (
	"fmt"
	"time"

	"termdock/internal/layout"
	"termdock/internal/pane"
	"termdock/internal/proto"
	"termdock/internal/vt10x"
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
	pf.CursorVisible = true
	pf.CursorX = cr.X + clampi(c.copy.curX, 0, maxi(0, cr.W-1))
	pf.CursorY = cr.Y + (c.copy.curY - c.copy.top)
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

// helpText is the full keybinding cheat-sheet, shown both transiently
// while the prefix key is held and persistently after Ctrl-B ? — until
// this line exists, "Ctrl-B ?" is what the idle status bar tells you to
// press, so it needs to actually go somewhere.
const helpText = "v/% vsplit | s/\" hsplit | hjkl/arrows move | o/Tab cycle | z zoom | r resize | " +
	"[ copy | ] paste | y sync | c new-win | n/p next/prev-win | 0-9 win# | , rename | & kill-win | " +
	"x close-pane | d detach | q quit | ? help"

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
	case c.prefix:
		style = "prefix"
		hint = "PREFIX > " + helpText
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
			X:        x,
			W:        width,
		}
		x += width
	}
	return tabs
}
