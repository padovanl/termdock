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
		drawPane(screen, p, cfg)
	}
	dividerStyle := tcell.StyleDefault.Foreground(tcell.ColorGray)
	for _, dv := range f.VDividers {
		x, y0, y1 := dv[0], dv[1], dv[2]
		for y := y0; y <= y1; y++ {
			screen.SetContent(x, y, tcell.RuneVLine, nil, dividerStyle)
		}
	}

	if f.ShowStatus {
		drawStatusBar(screen, f, cfg)
	}
	screen.Show()
}

func drawPane(screen tcell.Screen, p proto.PaneFrame, cfg config.Config) {
	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		return
	}

	// The server only reserves a title row when the pane is tall enough
	// to spare one (see layout.Node.ContentRect); infer that from how
	// many content rows actually came back rather than assuming r.H-1.
	contentY := r.Y
	if len(p.Cells) < r.H {
		titleStyle := tcell.StyleDefault.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
		if p.Active {
			titleStyle = tcell.StyleDefault.Background(cfg.PaneActiveBG).Foreground(tcell.ColorBlack).Bold(true)
		}
		drawText(screen, r.X, r.Y, r.W, titleStyle, " "+p.Title+" ")
		contentY = r.Y + 1
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
			screen.SetContent(r.X+x, contentY+y, ch, nil, style)
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
