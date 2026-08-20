package core

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/vt10x"
)

// copyState tracks scrollback/selection for whichever pane is in copy
// mode. Positions are absolute row indices into the pane's "virtual
// buffer" (scrollback history followed by the live grid); because history
// only ever grows by appending, an absolute index keeps pointing at the
// same line even as new output arrives while frozen in copy mode.
type copyState struct {
	active bool
	paneID int

	top        int // absolute row index of the topmost visible row
	curX, curY int // absolute cursor position

	selecting        bool
	anchorX, anchorY int

	searchTerm string
}

func (c *Core) enterCopyMode() {
	active := c.win().active
	p, ok := c.panes[active.ID]
	if !ok {
		return
	}
	t := p.Term()
	t.Lock()
	hl := t.HistoryLen()
	_, rows := t.Size()
	cur := t.Cursor()
	t.Unlock()

	c.mode = ModeCopy
	c.copy = copyState{
		active: true,
		paneID: active.ID,
		curX:   cur.X,
		curY:   hl + cur.Y,
	}
	maxTop := hl + rows - rows
	if maxTop < 0 {
		maxTop = 0
	}
	c.copy.top = maxTop
	c.statusMsg = "COPY: hjkl/arrows move, v select, y copy, q/Esc exit"
}

func (c *Core) exitCopyMode() {
	c.copy = copyState{}
	c.mode = ModeNormal
	c.statusMsg = ""
}

func (c *Core) copyPaneDims() (cols, rows, total int, ok bool) {
	p, found := c.panes[c.copy.paneID]
	if !found {
		return 0, 0, 0, false
	}
	t := p.Term()
	t.Lock()
	cols, rows = t.Size()
	hl := t.HistoryLen()
	t.Unlock()
	return cols, rows, hl + rows, true
}

func (c *Core) moveCursorBy(dx, dy int) {
	cols, rows, total, ok := c.copyPaneDims()
	if !ok {
		return
	}
	c.copy.curX = clampi(c.copy.curX+dx, 0, maxi(0, cols-1))
	c.copy.curY = clampi(c.copy.curY+dy, 0, maxi(0, total-1))
	if c.copy.curY < c.copy.top {
		c.copy.top = c.copy.curY
	} else if c.copy.curY >= c.copy.top+rows {
		c.copy.top = c.copy.curY - rows + 1
	}
	c.clampTop(rows, total)
}

func (c *Core) clampTop(rows, total int) {
	maxTop := maxi(0, total-rows)
	c.copy.top = clampi(c.copy.top, 0, maxTop)
}

// scrollView moves only the viewport (mouse wheel), leaving the cursor
// where it is. Returns true if the view is now pinned to the live bottom.
func (c *Core) scrollView(delta int) bool {
	_, rows, total, ok := c.copyPaneDims()
	if !ok {
		return true
	}
	c.copy.top += delta
	c.clampTop(rows, total)
	return c.copy.top >= maxi(0, total-rows)
}

func (c *Core) handleCopyKey(key tcell.Key, r rune) Result {
	_, rows, total, ok := c.copyPaneDims()
	if !ok {
		c.exitCopyMode()
		return Result{}
	}
	switch {
	case key == tcell.KeyEsc || r == 'q':
		c.exitCopyMode()
	case r == 'v':
		if c.copy.selecting {
			c.copy.selecting = false
		} else {
			c.copy.selecting = true
			c.copy.anchorX, c.copy.anchorY = c.copy.curX, c.copy.curY
		}
	case r == 'y' || key == tcell.KeyEnter:
		text, ok := c.yank()
		c.exitCopyMode()
		return Result{Clipboard: text, HasClipboard: ok}
	case r == '/':
		c.startInput("search", "Search: ", c.copy.searchTerm, ModeCopy)
	case r == 'n':
		c.searchNext(1)
	case r == 'N':
		c.searchNext(-1)
	case key == tcell.KeyLeft || r == 'h':
		c.moveCursorBy(-1, 0)
	case key == tcell.KeyRight || r == 'l':
		c.moveCursorBy(1, 0)
	case key == tcell.KeyUp || r == 'k':
		c.moveCursorBy(0, -1)
	case key == tcell.KeyDown || r == 'j':
		c.moveCursorBy(0, 1)
	case r == 'g' || key == tcell.KeyHome:
		c.copy.curX, c.copy.curY = 0, 0
		c.clampTop(rows, total)
		if c.copy.curY < c.copy.top {
			c.copy.top = c.copy.curY
		}
	case r == 'G' || key == tcell.KeyEnd:
		c.copy.curY = maxi(0, total-1)
		c.moveCursorBy(0, 0)
	case key == tcell.KeyPgUp:
		c.moveCursorBy(0, -rows)
	case key == tcell.KeyPgDn:
		c.moveCursorBy(0, rows)
	case key == tcell.KeyCtrlU:
		c.moveCursorBy(0, -maxi(1, rows/2))
	case key == tcell.KeyCtrlD:
		c.moveCursorBy(0, maxi(1, rows/2))
	}
	return Result{}
}

// yank returns the text of the current selection (if any), for the caller
// to forward to the requesting client as an OSC52 clipboard write.
func (c *Core) yank() (string, bool) {
	if !c.copy.selecting {
		return "", false
	}
	p, ok := c.panes[c.copy.paneID]
	if !ok {
		return "", false
	}
	sy, sx, ey, ex := c.copy.anchorY, c.copy.anchorX, c.copy.curY, c.copy.curX
	if sy > ey || (sy == ey && sx > ex) {
		sy, sx, ey, ex = ey, ex, sy, sx
	}

	t := p.Term()
	t.Lock()
	defer t.Unlock()
	hl := t.HistoryLen()
	cols, _ := t.Size()

	var b strings.Builder
	for y := sy; y <= ey; y++ {
		startX, endX := 0, cols-1
		if y == sy {
			startX = sx
		}
		if y == ey {
			endX = ex
		}
		row := make([]rune, 0, cols)
		for x := startX; x <= endX && x < cols; x++ {
			g := cellAt(t, hl, y, x)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			row = append(row, ch)
		}
		b.WriteString(strings.TrimRight(string(row), " "))
		if y < ey {
			b.WriteByte('\n')
		}
	}
	text := b.String()
	c.pushRegister(text)
	return text, true
}

// searchNext looks for the next (dir=1) or previous (dir=-1) occurrence of
// c.copy.searchTerm, wrapping around the whole scrollback+live buffer,
// case-insensitively, starting just past the cursor. Moves the cursor and
// scrolls it into view on a hit; leaves a "no match" status otherwise.
func (c *Core) searchNext(dir int) {
	needle := strings.ToLower(c.copy.searchTerm)
	if needle == "" {
		return
	}
	p, ok := c.panes[c.copy.paneID]
	if !ok {
		return
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	total := hl + rows
	if total == 0 {
		return
	}

	for i := 1; i <= total; i++ {
		y := ((c.copy.curY+dir*i)%total + total) % total
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			g := cellAt(t, hl, y, x)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		idx := strings.Index(strings.ToLower(sb.String()), needle)
		if idx < 0 {
			continue
		}
		c.copy.curX, c.copy.curY = idx, y
		c.clampTop(rows, total)
		if c.copy.curY < c.copy.top || c.copy.curY >= c.copy.top+rows {
			c.copy.top = clampi(y-rows/2, 0, maxi(0, total-rows))
		}
		c.statusMsg = ""
		return
	}
	c.statusMsg = "no match: " + c.copy.searchTerm
}

// cellAt resolves an absolute row index (scrollback history followed by
// the live grid) to a glyph.
func cellAt(t vt10x.Terminal, historyLen, absY, x int) vt10x.Glyph {
	if absY < historyLen {
		return t.HistoryCell(absY, x)
	}
	return t.Cell(x, absY-historyLen)
}

// selected reports whether absolute position (x,y) falls within the
// current copy-mode selection.
func (c *Core) selected(x, y int) bool {
	if !c.copy.selecting {
		return false
	}
	sy, sx, ey, ex := c.copy.anchorY, c.copy.anchorX, c.copy.curY, c.copy.curX
	if sy > ey || (sy == ey && sx > ex) {
		sy, sx, ey, ex = ey, ex, sy, sx
	}
	if y < sy || y > ey {
		return false
	}
	if y == sy && x < sx {
		return false
	}
	if y == ey && x > ex {
		return false
	}
	return true
}

func clampi(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
