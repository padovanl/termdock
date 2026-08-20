package core

import (
	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
)

// The floating scratch terminal (Ctrl-B P) — tmux added display-popup in
// 3.2 for exactly this: a quick terminal for a git commit, lazygit, a
// throwaway note, summoned over whatever layout you're already looking
// at without disturbing it. Unlike tmux's popup (one-shot, its process
// dies when you close it), this one is a single persistent pane per
// session: hiding it just hides it, the shell inside keeps running, and
// showing it again picks up exactly where you left off — closer to
// tmux-floax's "scratchpad" than stock tmux. It deliberately lives
// outside every window's layout tree (not in c.panes, not touched by
// persistStateLocked) — a floating utility pane, not a "real" pane the
// rest of the session's bookkeeping needs to know about.

// popupWidthFrac/popupHeightFrac size the popup relative to the pane
// area (not the raw terminal — it should never cover the status bar).
const (
	popupWidthFrac  = 0.8
	popupHeightFrac = 0.7
	popupMinW       = 20
	popupMinH       = 6
)

func (c *Core) togglePopup() {
	if c.popupVisible {
		c.popupVisible = false
		c.mode = ModeNormal
		return
	}
	if c.popup == nil {
		w, h := c.popupSize()
		id := pane.NextID()
		// c.popupCommand == "" behaves exactly like pane.New (an
		// interactive shell) — see NewWithCommand's own doc comment —
		// so there's no need to branch on whether popup-command is set.
		p, err := pane.NewWithCommand(id, w, h, c.popupCommand)
		if err != nil {
			c.statusMsg = "error creating popup: " + err.Error()
			return
		}
		c.popup = p
		c.startPopupPump(p)
	} else {
		w, h := c.popupSize()
		c.popup.Resize(w, h)
	}
	c.popupVisible = true
	c.mode = ModePopup
}

// popupSize computes the popup's content dimensions (border excluded)
// from the current terminal size, used both to size a freshly created
// popup pane and to re-fit an existing one when the terminal resizes
// while it's up.
func (c *Core) popupSize() (w, h int) {
	availW, availH := c.cols, maxi(0, c.rows-c.statusRows())
	w = maxi(popupMinW, int(float64(availW)*popupWidthFrac)) - 2
	h = maxi(popupMinH, int(float64(availH)*popupHeightFrac)) - 2
	return maxi(1, w), maxi(1, h)
}

// popupRect returns the popup's full box (border included), in the same
// coordinate space as everything else in the pane area.
func (c *Core) popupRect() proto.Rect {
	contentW, contentH := c.popupSize()
	w, h := contentW+2, contentH+2
	x := maxi(0, (c.cols-w)/2)
	y := maxi(0, (maxi(0, c.rows-c.statusRows())-h)/2)
	return proto.Rect{X: x, Y: y, W: w, H: h}
}

func (c *Core) startPopupPump(p *pane.Pane) {
	go p.Pump(
		func() { c.markDirty() },
		func() {
			c.mu.Lock()
			if c.popup == p {
				c.popup = nil
				if c.popupVisible {
					c.popupVisible = false
					if c.mode == ModePopup {
						c.mode = ModeNormal
					}
				}
			}
			c.mu.Unlock()
			c.markDirty()
		},
	)
}

// resizePopupLocked re-fits an open popup when the terminal itself
// resizes (see Core.Resize); a no-op if it's not currently up.
func (c *Core) resizePopupLocked() {
	if c.popup == nil {
		return
	}
	w, h := c.popupSize()
	c.popup.Resize(w, h)
}

// handlePopupKey is deliberately narrow: with the popup focused, typed
// keys go straight into it (like any other pane), and only a handful of
// prefix commands still make sense — hiding the popup itself, detaching,
// quitting. Split/window/navigation commands would be operating on a
// window layout you can't currently see, which is more confusing than
// useful, so they're not offered here at all.
func (c *Core) handlePopupKey(key tcell.Key, r rune) Result {
	if !c.prefix {
		if key == c.prefixKey {
			c.prefix = true
			return Result{}
		}
		c.writeToPopup(key, r)
		return Result{}
	}
	c.prefix = false
	c.statusMsg = ""
	var res Result
	switch {
	case key == c.prefixKey:
		c.writeToPopup(key, r) // double prefix-key press: send it through literally
	case r == 'P':
		c.togglePopup()
	case r == 'd':
		res.Detach = true
	case r == 'q':
		c.requestQuit()
	}
	return res
}

func (c *Core) writeToPopup(key tcell.Key, r rune) {
	if c.popup == nil {
		return
	}
	if b := keyBytes(key, r); b != nil {
		c.popup.Write(b)
	}
}

// handlePopupMouse only supports one gesture: clicking outside the
// popup's box dismisses it, the same "click off a floating window to
// close it" convention windowing systems use. Clicks and drags inside it
// (for text selection, say) aren't wired up in this first cut.
func (c *Core) handlePopupMouse(primary, released bool, x, y int) {
	if !primary || released {
		return
	}
	r := c.popupRect()
	if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
		c.popupVisible = false
		c.mode = ModeNormal
	}
}

// popupFrame builds the client-facing snapshot of the popup, or nil when
// it isn't showing.
func (c *Core) popupFrame() *proto.PaneFrame {
	if !c.popupVisible || c.popup == nil {
		return nil
	}
	r := c.popupRect()
	contentW, contentH := r.W-2, r.H-2
	if contentW <= 0 || contentH <= 0 {
		return nil
	}
	contentRect := proto.Rect{X: r.X + 1, Y: r.Y + 1, W: contentW, H: contentH}

	t := c.popup.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()

	cells := make([][]proto.Cell, contentRect.H)
	for y := 0; y < contentRect.H; y++ {
		row := make([]proto.Cell, contentRect.W)
		if y < rows {
			for x := 0; x < contentRect.W && x < cols; x++ {
				row[x] = glyphToCell(t.Cell(x, y))
			}
		}
		cells[y] = row
	}

	cur := t.Cursor()
	return &proto.PaneFrame{
		ID:            c.popup.ID,
		Rect:          contentRect,
		Title:         "scratch (Ctrl-B P to hide)",
		Active:        true,
		CursorVisible: t.CursorVisible(),
		CursorX:       contentRect.X + cur.X,
		CursorY:       contentRect.Y + cur.Y,
		Cells:         cells,
	}
}
