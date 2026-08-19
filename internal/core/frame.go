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
		f.Panes = append(f.Panes, c.buildPaneFrame(w.zoomed, true))
	} else {
		for _, leaf := range layout.Leaves(w.root) {
			f.Panes = append(f.Panes, c.buildPaneFrame(leaf, leaf == w.active))
		}
		f.VDividers = layout.VerticalDividers(w.root)
	}

	f.StatusText, f.StatusRight, f.StatusStyle = c.statusLine()
	return f
}

func (c *Core) buildPaneFrame(n *layout.Node, active bool) proto.PaneFrame {
	pf := proto.PaneFrame{
		ID:     n.ID,
		Rect:   proto.Rect(n.Rect),
		Active: active,
	}
	p, ok := c.panes[n.ID]
	if !ok {
		return pf
	}
	pf.Title = c.paneTitle(n.ID, p)

	if active && c.mode == ModeCopy && c.copy.paneID == n.ID {
		return c.buildCopyFrame(n, p, pf)
	}

	cr := n.ContentRect()
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
	cr := n.ContentRect()
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

func (c *Core) paneTitle(id int, p *pane.Pane) string {
	if fg := p.ForegroundTitle(); fg != "" {
		return fmt.Sprintf("%d:%s", id, fg)
	}
	return fmt.Sprintf("%d:%s", id, c.shellName)
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
	case c.prefix:
		style = "prefix"
		hint = "PREFIX > v/s split | c/n/p window | z zoom | r resize | [ copy | y sync | ] paste | x close | d detach | q quit"
	case c.statusMsg != "":
		hint = c.statusMsg
	}

	w := c.win()
	sync := ""
	if w.syncPanes {
		sync = " [SYNC]"
	}
	text = fmt.Sprintf(" termdock:%s %s | active pane: %d%s | %s", c.SessionName, c.windowListText(), w.active.ID, sync, hint)

	right = time.Now().Format("15:04 02-Jan-06")
	if c.hostname != "" {
		right = c.hostname + " " + right
	}
	right += " "
	return text, right, style
}

// windowListText renders the tab bar segment of the status line, e.g.
// "[0:bash 1:vim! 2:htop*]": "*" marks the window you're looking at, "!"
// marks one that produced output while you weren't — tmux's
// monitor-activity, on by default here since there's no window list UI
// to toggle it from otherwise.
func (c *Core) windowListText() string {
	var b []byte
	b = append(b, '[')
	for i, w := range c.windows {
		if i > 0 {
			b = append(b, ' ')
		}
		b = append(b, fmt.Sprintf("%d:%s", i, c.windowDisplayName(w))...)
		if w.activity {
			b = append(b, '!')
		}
		if i == c.activeWindow {
			b = append(b, '*')
		}
	}
	b = append(b, ']')
	return string(b)
}
