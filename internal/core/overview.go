package core

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

// The pane overview (Ctrl-B g) is a "mission control" grid: every pane in
// every window of the session, each with a small live preview, arranged
// to fill the screen. Nothing in tmux shows more than one pane's content
// at a time outside of actually looking at it. Arrows/hjkl move the
// selection (a grid, so up/down jump by a row's width, not just ±1),
// Enter or a click jumps straight to that pane, Esc cancels.

type overviewTarget struct {
	windowIdx int
	paneID    int
}

type overviewState struct {
	tiles []overviewTarget
	sel   int
}

type overviewTileLayout struct {
	target overviewTarget
	rect   proto.Rect
}

func (c *Core) enterOverview() {
	var tiles []overviewTarget
	sel := 0
	for wi, w := range c.windows {
		for _, l := range layout.Leaves(w.root) {
			if wi == c.activeWindow && l == w.active {
				sel = len(tiles)
			}
			tiles = append(tiles, overviewTarget{windowIdx: wi, paneID: l.ID})
		}
	}
	if len(tiles) == 0 {
		return
	}
	c.mode = ModeOverview
	c.overview = overviewState{tiles: tiles, sel: sel}
}

func (c *Core) handleOverviewKey(key tcell.Key, r rune) {
	n := len(c.overview.tiles)
	cols := overviewCols(n)
	switch {
	case key == tcell.KeyEsc || r == 'q':
		c.mode = ModeNormal
		c.overview = overviewState{}
	case key == tcell.KeyEnter:
		c.confirmOverview()
	case key == tcell.KeyLeft || r == 'h':
		c.overview.sel = clampi(c.overview.sel-1, 0, maxi(0, n-1))
	case key == tcell.KeyRight || r == 'l':
		c.overview.sel = clampi(c.overview.sel+1, 0, maxi(0, n-1))
	case key == tcell.KeyUp || r == 'k':
		c.overview.sel = clampi(c.overview.sel-cols, 0, maxi(0, n-1))
	case key == tcell.KeyDown || r == 'j':
		c.overview.sel = clampi(c.overview.sel+cols, 0, maxi(0, n-1))
	}
}

func (c *Core) handleOverviewMouse(primary, released bool, x, y int) {
	if !primary || released {
		return
	}
	for i, tl := range c.overviewLayout() {
		r := tl.rect
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			c.overview.sel = i
			c.confirmOverview()
			return
		}
	}
}

// confirmOverview jumps to the selected tile's window/pane, re-resolving
// it against live state rather than trusting the snapshot taken on entry
// (same reasoning as confirmPicker): if the target closed in the
// meantime, this just cancels instead of jumping somewhere wrong.
func (c *Core) confirmOverview() {
	if c.overview.sel < len(c.overview.tiles) {
		t := c.overview.tiles[c.overview.sel]
		if t.windowIdx < len(c.windows) {
			w := c.windows[t.windowIdx]
			if leaf := findLeafByID(w.root, t.paneID); leaf != nil {
				c.activeWindow = t.windowIdx
				w.active = leaf
				c.afterWindowSwitch()
			}
		}
	}
	c.mode = ModeNormal
	c.overview = overviewState{}
	c.relayoutLocked()
}

// overviewCols picks a roughly-square grid width for n tiles.
func overviewCols(n int) int {
	cols := 1
	for cols*cols < n {
		cols++
	}
	return cols
}

// overviewLayout computes the grid rect for every tile — the single
// source of truth for both what overviewFrame sends the client to draw
// and what handleOverviewMouse hit-tests a click against, so the two
// can't drift apart the way a window tab's X/W already doesn't.
func (c *Core) overviewLayout() []overviewTileLayout {
	n := len(c.overview.tiles)
	if n == 0 {
		return nil
	}
	cols := overviewCols(n)
	rows := (n + cols - 1) / cols
	availW, availH := c.cols, maxi(0, c.rows-c.statusRows())
	cellW, cellH := availW/cols, availH/rows

	out := make([]overviewTileLayout, n)
	for i, target := range c.overview.tiles {
		col, row := i%cols, i/cols
		x, y := col*cellW, row*cellH
		w, h := cellW, cellH
		if col == cols-1 {
			w = availW - col*cellW // last column absorbs the remainder
		}
		if row == rows-1 {
			h = availH - row*cellH // last row absorbs the remainder
		}
		out[i] = overviewTileLayout{target: target, rect: proto.Rect{X: x, Y: y, W: w, H: h}}
	}
	return out
}

func (c *Core) overviewFrame() *proto.Overview {
	if c.mode != ModeOverview {
		return nil
	}
	tileLayout := c.overviewLayout()
	tiles := make([]proto.OverviewTile, len(tileLayout))
	for i, tl := range tileLayout {
		title := "?"
		if tl.target.windowIdx < len(c.windows) {
			title = fmt.Sprintf("%d:%s", tl.target.windowIdx, c.pickerPaneTitle(tl.target.paneID))
		}
		innerW, innerH := maxi(0, tl.rect.W-2), maxi(0, tl.rect.H-2)
		tiles[i] = proto.OverviewTile{
			Title:  title,
			Cells:  c.buildPreview(tl.target.paneID, innerW, innerH),
			Active: i == c.overview.sel,
			Rect:   tl.rect,
		}
	}
	return &proto.Overview{Tiles: tiles}
}
