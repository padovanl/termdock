package core

import (
	"github.com/gdamore/tcell/v2"

	"termdock/internal/layout"
	"termdock/internal/proto"
)

func (c *Core) handleMouse(m proto.ClientMsg) Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.windows) == 0 {
		return Result{} // session mid-shutdown; a lingering connection raced us here
	}

	buttons := tcell.ButtonMask(m.MouseButtons)
	x, y := m.MouseX, m.MouseY

	if buttons&tcell.WheelUp != 0 {
		c.wheelUp(x, y)
		c.markDirty()
		return Result{}
	}
	if buttons&tcell.WheelDown != 0 {
		c.wheelDown()
		c.markDirty()
		return Result{}
	}

	primary := buttons&tcell.Button1 != 0
	released := buttons == tcell.ButtonNone

	var res Result
	if c.mode == ModeCopy {
		res = c.handleCopyMouse(primary, released, x, y)
	} else {
		c.handleNormalMouse(primary, released, x, y)
	}
	c.markDirty()
	return res
}

func (c *Core) handleNormalMouse(primary, released bool, x, y int) {
	if released {
		c.drag = nil
		return
	}
	if !primary {
		return
	}
	if c.drag != nil {
		layout.SetRatioFromColumn(c.drag.node, x)
		c.relayoutLocked()
		return
	}
	if c.win().zoomed == nil {
		if node := layout.HitDivider(c.win().root, x, y); node != nil {
			c.drag = &dragState{node: node}
			return
		}
	}
	c.focusAt(x, y)
}

func (c *Core) handleCopyMouse(primary, released bool, x, y int) Result {
	leaf := findLeafByID(c.win().root, c.copy.paneID)
	if leaf == nil {
		c.exitCopyMode()
		return Result{}
	}

	if released {
		if c.mouseDown {
			c.mouseDown = false
			moved := x != c.mouseDownX || y != c.mouseDownY
			if moved {
				text, ok := c.yank()
				return Result{Clipboard: text, HasClipboard: ok}
			}
			c.copy.selecting = false
		}
		return Result{}
	}
	if !primary {
		return Result{}
	}

	cr := leaf.ContentRect()
	cols, rows, total, ok := c.copyPaneDims()
	if !ok {
		return Result{}
	}
	localX := clampi(x-cr.X, 0, maxi(0, cols-1))
	localY := clampi(y-cr.Y, 0, maxi(0, rows-1))
	absY := clampi(c.copy.top+localY, 0, maxi(0, total-1))

	if !c.mouseDown {
		c.mouseDown = true
		c.mouseDownX, c.mouseDownY = x, y
		c.copy.selecting = true
		c.copy.anchorX, c.copy.anchorY = localX, absY
	}
	c.copy.curX, c.copy.curY = localX, absY
	c.clampTop(rows, total)
	return Result{}
}

func (c *Core) wheelUp(x, y int) {
	leaf := c.leafAt(x, y)
	if leaf == nil {
		return
	}
	if c.mode != ModeCopy || c.copy.paneID != leaf.ID {
		p, ok := c.panes[leaf.ID]
		if !ok {
			return
		}
		t := p.Term()
		t.Lock()
		hl := t.HistoryLen()
		t.Unlock()
		if hl == 0 {
			return
		}
		c.setActive(leaf)
		c.enterCopyMode()
	}
	c.scrollView(-3)
}

func (c *Core) wheelDown() {
	if c.mode != ModeCopy {
		return
	}
	if c.scrollView(3) {
		c.exitCopyMode()
	}
}
