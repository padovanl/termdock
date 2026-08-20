package client

import (
	"github.com/gdamore/tcell/v2"

	"termdock/internal/config"
	"termdock/internal/proto"
)

// These mirror pane.AttrXxx (internal/pane/pane.go), which itself mirrors
// vt10x's glyph attribute bits. Duplicated here so the client doesn't need
// to import the pty/terminal-emulation packages just for 7 constants.
const (
	attrReverse = 1 << iota
	attrUnderline
	attrBold
	attrGfx
	attrItalic
	attrBlink
	attrWrap
)

func draw(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	screen.Clear()

	for _, p := range f.Panes {
		drawPaneContent(screen, p)
	}
	drawBorders(screen, f, cfg)

	if f.ShowStatus {
		drawStatusBar(screen, f, cfg)
	}
	screen.Show()
}

func drawPaneContent(screen tcell.Screen, p proto.PaneFrame) {
	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		return
	}

	for y, row := range p.Cells {
		for x, cell := range row {
			style := tcell.StyleDefault.Foreground(convColor(cell.FG)).Background(convColor(cell.BG))
			if cell.Attr&attrBold != 0 {
				style = style.Bold(true)
			}
			if cell.Attr&attrUnderline != 0 {
				style = style.Underline(true)
			}
			if cell.Attr&attrReverse != 0 {
				style = style.Reverse(true)
			}
			if cell.Attr&attrBlink != 0 {
				style = style.Blink(true)
			}
			ch := cell.Ch
			if ch == 0 {
				ch = ' '
			}
			screen.SetContent(r.X+x, r.Y+y, ch, nil, style)
		}
	}

	if p.Active {
		if p.CursorVisible {
			screen.ShowCursor(p.CursorX, p.CursorY)
		} else {
			screen.HideCursor()
		}
	}
}

// drawBorders draws a one-cell border around every pane in f, entirely
// outside each pane's content Rect (in the margin reserved around the
// whole tree and the single row/column each split reserves between its
// two children — see internal/layout.Compute). Borders are inferred purely
// from the geometry of the pane Rects the server already sends, rather
// than an explicit divider list: two panes can only be geometrically
// adjacent if the split tree actually placed them that way, so unioning
// every pane's own outline and auto-tiling the box-drawing glyphs from
// each cell's neighbors naturally produces correctly joined T/cross
// junctions wherever three or four panes meet, with no tree-topology
// bookkeeping needed here. The active pane's own outline is drawn in the
// accent color so it's obvious at a glance which pane has focus (and,
// practically, which divider a click will grab for resizing).
func drawBorders(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	cells := map[[2]int]bool{}
	var active *proto.PaneFrame
	for i := range f.Panes {
		p := &f.Panes[i]
		if p.Rect.W <= 0 || p.Rect.H <= 0 {
			continue
		}
		addRectBorder(cells, p.Rect)
		if p.Active {
			active = p
		}
	}
	activeCells := map[[2]int]bool{}
	if active != nil {
		addRectBorder(activeCells, active.Rect)
	}

	normalStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	activeStyle := tcell.StyleDefault.Foreground(cfg.PaneActiveBG).Bold(true)

	for cell := range cells {
		x, y := cell[0], cell[1]
		if x < 0 || y < 0 || x >= f.Cols || y >= f.Rows {
			continue
		}
		up := cells[[2]int{x, y - 1}]
		down := cells[[2]int{x, y + 1}]
		left := cells[[2]int{x - 1, y}]
		right := cells[[2]int{x + 1, y}]
		style := normalStyle
		if activeCells[cell] {
			style = activeStyle
		}
		screen.SetContent(x, y, boxChar(up, down, left, right), nil, style)
	}

	for i := range f.Panes {
		p := &f.Panes[i]
		if p.Rect.W <= 0 || p.Rect.H <= 0 {
			continue
		}
		style := normalStyle
		if p.Active {
			style = activeStyle
		}
		drawPaneTitle(screen, p.Rect, p.Title, style, f.Cols, f.Rows)
	}
}

// addRectBorder marks the perimeter of r, expanded by one cell on every
// side, as border cells.
func addRectBorder(cells map[[2]int]bool, r proto.Rect) {
	x0, y0 := r.X-1, r.Y-1
	x1, y1 := r.X+r.W, r.Y+r.H
	for x := x0; x <= x1; x++ {
		cells[[2]int{x, y0}] = true
		cells[[2]int{x, y1}] = true
	}
	for y := y0; y <= y1; y++ {
		cells[[2]int{x0, y}] = true
		cells[[2]int{x1, y}] = true
	}
}

// boxChar picks the Unicode box-drawing glyph matching which of a border
// cell's four neighbors are themselves border cells.
func boxChar(up, down, left, right bool) rune {
	switch {
	case up && down && left && right:
		return '┼'
	case down && left && right:
		return '┬'
	case up && left && right:
		return '┴'
	case up && down && right:
		return '├'
	case up && down && left:
		return '┤'
	case down && right:
		return '┌'
	case down && left:
		return '┐'
	case up && right:
		return '└'
	case up && left:
		return '┘'
	case left, right:
		return '─'
	case up, down:
		return '│'
	default:
		return ' '
	}
}

// drawPaneTitle overlays a pane's title on its own top border edge, e.g.
// "┌─ 1:bash ──────┐". Skipped entirely if the pane is too narrow to fit
// anything more than its corners.
func drawPaneTitle(screen tcell.Screen, r proto.Rect, title string, style tcell.Style, cols, rows int) {
	y := r.Y - 1
	if y < 0 || y >= rows {
		return
	}
	label := " " + title + " "
	x := r.X + 1 // one cell in from the top-left corner/dash
	maxW := r.W - 2
	if maxW < 1 {
		return
	}
	i := 0
	for _, ch := range label {
		if i >= maxW {
			break
		}
		cx := x + i
		if cx >= 0 && cx < cols {
			screen.SetContent(cx, y, ch, nil, style)
		}
		i++
	}
}

func drawStatusBar(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	y := f.Rows - 1
	if y < 0 {
		return
	}
	style := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.StatusFG)
	switch f.StatusStyle {
	case "prefix":
		style = tcell.StyleDefault.Background(tcell.ColorDarkGreen).Foreground(tcell.ColorWhite)
	case "mode":
		style = tcell.StyleDefault.Background(tcell.ColorPurple).Foreground(tcell.ColorWhite)
	}
	drawText(screen, 0, y, f.Cols, style, f.StatusText)

	rw := len([]rune(f.StatusRight))
	if rw > 0 {
		leftLen := len([]rune(f.StatusText))
		if leftLen > f.Cols {
			leftLen = f.Cols // StatusText was itself clipped to the screen width
		}
		x := f.Cols - rw
		if x > leftLen { // don't clobber the left side on a narrow terminal
			drawText(screen, x, y, rw, style, f.StatusRight)
		}
	}
}

func drawText(screen tcell.Screen, x, y, w int, style tcell.Style, text string) {
	for i := 0; i < w; i++ {
		screen.SetContent(x+i, y, ' ', nil, style)
	}
	i := 0
	for _, r := range text {
		if i >= w {
			break
		}
		screen.SetContent(x+i, y, r, nil, style)
		i++
	}
}

// convColor decodes a raw vt10x.Color value (see internal/vt10x/color.go):
// [0,16) base ANSI, [16,256) xterm 256-color palette, [256, 1<<24) 24-bit
// truecolor packed as r<<16|g<<8|b, and >= 1<<24 the terminal's default
// fg/bg/cursor sentinels.
func convColor(raw uint32) tcell.Color {
	switch {
	case raw >= 1<<24:
		return tcell.ColorDefault
	case raw < 256:
		return tcell.PaletteColor(int(raw))
	default:
		r := int32(raw>>16) & 0xff
		g := int32(raw>>8) & 0xff
		b := int32(raw) & 0xff
		return tcell.NewRGBColor(r, g, b)
	}
}
