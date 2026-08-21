package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

// The jump picker (Ctrl-B w) is termdock's answer to tmux's choose-tree:
// instead of a modal list you page through with j/k, it's type-ahead —
// every keystroke narrows a fuzzy-matched list of every pane in every
// window of this session, Enter jumps straight to it (switching windows
// and focusing that pane in one step), Esc cancels. One list covers what
// tmux splits across choose-window and choose-pane.

// pickerItem is one jump target: window windowIdx, and paneID within it
// (a window with more than one pane gets one item per pane; a
// single-pane window gets just the one, since "jump to the window" and
// "jump to its only pane" are the same action).
type pickerItem struct {
	windowIdx int
	paneID    int
	label     string
}

type pickerState struct {
	query    []rune
	items    []pickerItem // built once on entry; the list doesn't change while typing
	filtered []int        // indices into items, current query's matches, best first
	sel      int          // index into filtered
}

// enterPicker snapshots every window/pane as of right now into a fresh
// picker list and switches to ModePicker. The list isn't kept live while
// typing — closing a pane while the picker happens to be open and you
// happen to be looking at a stale entry for it is an edge case rare
// enough that refreshing on every keystroke isn't worth the complexity;
// confirmSelection re-resolves the target at Enter time regardless (see
// below), so a stale entry just fails closed instead of jumping to the
// wrong place.
func (c *Core) enterPicker() {
	c.mode = ModePicker
	c.picker = pickerState{items: c.buildPickerItems()}
	c.refilterPicker()
}

func (c *Core) buildPickerItems() []pickerItem {
	var items []pickerItem
	for wi, w := range c.windows {
		leaves := layout.Leaves(w.root)
		wname := c.windowDisplayName(w)
		for pi, l := range leaves {
			label := fmt.Sprintf("%d:%s", wi, wname)
			if len(leaves) > 1 {
				label += fmt.Sprintf(" › %d:%s", pi+1, c.pickerPaneTitle(l.ID))
			}
			items = append(items, pickerItem{windowIdx: wi, paneID: l.ID, label: label})
		}
	}
	return items
}

func (c *Core) pickerPaneTitle(id int) string {
	// A name the user gave the pane is the whole reason they gave it: it
	// has to be what the picker offers, or renaming buys nothing exactly
	// where it matters most.
	if given, ok := c.paneNames[id]; ok && given != "" {
		return given
	}
	if p, ok := c.panes[id]; ok {
		if fg := p.ForegroundTitle(); fg != "" {
			return fg
		}
	}
	return c.shellName
}

func (c *Core) handlePickerKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.cancelPicker()
	case key == tcell.KeyEnter:
		c.confirmPicker()
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.picker.query); n > 0 {
			c.picker.query = c.picker.query[:n-1]
			c.refilterPicker()
		}
	case key == tcell.KeyCtrlU:
		c.picker.query = c.picker.query[:0]
		c.refilterPicker()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.movePicker(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.movePicker(1)
	case r != 0 && key == tcell.KeyRune:
		c.picker.query = append(c.picker.query, r)
		c.refilterPicker()
	}
}

func (c *Core) movePicker(delta int) {
	n := len(c.picker.filtered)
	if n == 0 {
		return
	}
	c.picker.sel = ((c.picker.sel+delta)%n + n) % n
}

func (c *Core) cancelPicker() {
	c.mode = ModeNormal
	c.picker = pickerState{}
}

// confirmPicker jumps to the selected item, re-resolving it against the
// live window/pane state rather than trusting the snapshot taken when the
// picker opened (see enterPicker) — if the target closed in the meantime,
// this just cancels instead of jumping somewhere wrong.
func (c *Core) confirmPicker() {
	if c.picker.sel < len(c.picker.filtered) {
		it := c.picker.items[c.picker.filtered[c.picker.sel]]
		if it.windowIdx < len(c.windows) {
			w := c.windows[it.windowIdx]
			if leaf := findLeafByID(w.root, it.paneID); leaf != nil {
				c.setWindowActiveLeaf(w, leaf) // before setActiveWindowIndex, so its afterWindowSwitch/touchPane stamps the pane we're jumping *to*
				c.setActiveWindowIndex(it.windowIdx)
			}
		}
	}
	c.mode = ModeNormal
	c.picker = pickerState{}
	c.relayoutLocked()
}

// refilterPicker recomputes picker.filtered from picker.query. With a
// query, it's a fuzzy (subsequence) match against every item's label,
// ranked by how early the match starts — not a full fzf-grade scorer,
// but with a session's worth of windows and panes (tens, not thousands)
// simplicity buys more than a fancier ranking would. With an empty
// query, every item matches and they're ranked most-recently-used
// first instead (see touchPane) — Ctrl-B w, Enter becomes a fast
// "jump to whatever I was just looking at," Alt-Tab style, rather than
// always landing back on window 0's first pane.
func (c *Core) refilterPicker() {
	query := string(c.picker.query)
	type scored struct {
		idx  int
		at   int
		used time.Time
	}
	var matches []scored
	for i, it := range c.picker.items {
		if ok, at := fuzzyMatch(query, it.label); ok {
			matches = append(matches, scored{i, at, c.paneLastActive[it.paneID]})
		}
	}
	if query == "" {
		sort.SliceStable(matches, func(a, b int) bool { return matches[a].used.After(matches[b].used) })
	} else {
		sort.SliceStable(matches, func(a, b int) bool { return matches[a].at < matches[b].at })
	}
	filtered := make([]int, len(matches))
	for i, m := range matches {
		filtered[i] = m.idx
	}
	c.picker.filtered = filtered
	c.picker.sel = clampi(c.picker.sel, 0, maxi(0, len(filtered)-1))
}

// fuzzyMatch reports whether every rune of query appears in target, in
// order, case-insensitively (not necessarily contiguously — "1vim"
// matches "1:vim"), and the position of the first matched rune, used as
// a cheap "how early does this match" ranking signal.
func fuzzyMatch(query, target string) (ok bool, at int) {
	if query == "" {
		return true, 0
	}
	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(target))
	qi, first := 0, -1
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if t[ti] == q[qi] {
			if first < 0 {
				first = ti
			}
			qi++
		}
	}
	if qi == len(q) {
		return true, first
	}
	return false, 0
}

// previewCols/previewRows are the picker's live preview box, in cells.
// Fixed, never sized to the selected pane: a preview that grew and shrank
// with each pane's dimensions made the whole centered overlay jump around
// the screen as you moved the selection. The client crops it to whatever
// room is left beside the list rather than dropping it, so asking for
// more here can't cost a narrow terminal its preview entirely (see
// drawOverlay). Each cell is a braille glyph covering 2x4 of the pane's
// own cells (see buildThumbnail), so this shows a 112x56 pane whole.
const previewCols, previewRows = 56, 14

// pickerOverlay builds the client-facing snapshot of the picker, or nil
// when it isn't open.
func (c *Core) pickerOverlay() *proto.Overlay {
	if c.mode != ModePicker {
		return nil
	}
	items := make([]string, len(c.picker.filtered))
	for i, idx := range c.picker.filtered {
		items[i] = c.picker.items[idx].label
	}
	ov := &proto.Overlay{
		Title:      "jump to window/pane — type to filter, ↑↓ select, enter jump, esc cancel",
		ShowQuery:  true,
		Query:      string(c.picker.query),
		Selectable: true,
		Items:      items,
		Selected:   c.picker.sel,
	}
	if c.picker.sel < len(c.picker.filtered) {
		it := c.picker.items[c.picker.filtered[c.picker.sel]]
		ov.PreviewCells = c.buildThumbnail(it.paneID, previewCols, previewRows)
	}
	return ov
}

// brailleDots maps an (x, y) position inside a braille cell's 2x4 pixel
// grid to the bit it occupies in the U+2800 block. The column-major,
// non-obvious ordering is the Unicode standard's, not ours: the first
// six dots follow the original 6-dot braille layout and the bottom row
// was appended later, which is why it jumps to bits 6 and 7.
var brailleDots = [4][2]rune{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// buildPreview renders a maxW x maxH window of paneID's real characters,
// used where the box is big enough for text to actually be readable —
// the Ctrl-B g overview's tiles (see overview.go). The jump picker's
// much smaller box uses buildThumbnail instead.
//
// The window is anchored on the cursor rather than the bottom of the
// grid. Anchoring on the bottom is equivalent for a pane that's already
// scrolled, but renders an entirely blank preview for the most common
// case there is: a freshly split shell, whose prompt sits at the *top* of
// an otherwise empty grid.
//
// Always exactly maxH x maxW, blank-padded for a pane smaller than the
// box, so a tile never comes back ragged.
func (c *Core) buildPreview(paneID int, maxW, maxH int) [][]proto.Cell {
	if maxW <= 0 || maxH <= 0 {
		return nil
	}
	p, ok := c.panes[paneID]
	if !ok {
		return nil
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	if cols <= 0 || rows <= 0 {
		return nil
	}
	// Scroll just far enough to keep the cursor row in view, clamped to
	// the grid: prompt-at-the-top shows the top, a scrolled pane still
	// shows its most recent rows.
	yOff := t.Cursor().Y - maxH + 1
	if yOff < 0 {
		yOff = 0
	}
	if maxOff := rows - maxH; yOff > maxOff {
		yOff = maxOff
	}
	cells := make([][]proto.Cell, maxH)
	for y := 0; y < maxH; y++ {
		row := make([]proto.Cell, maxW)
		for x := 0; x < maxW; x++ {
			if x < cols && yOff+y < rows {
				row[x] = glyphToCell(t.Cell(x, yOff+y))
				continue
			}
			row[x] = proto.Cell{Ch: ' '}
		}
		cells[y] = row
	}
	return cells
}

// buildThumbnail renders paneID's entire screen as a maxW x maxH minimap
// — the whole pane shrunk, not a crop of it. Used for the jump picker's
// preview box (see pickerOverlay), which is far too small to show a
// useful amount of real text.
//
// A terminal can't shrink its font, so "the whole pane, smaller" has to
// come out of resolution instead: each output cell is a braille glyph
// whose 2x4 dot grid is used as pixels, giving 8 samples per cell and
// letting a 112x56 pane fit into 56x14 while keeping its actual shape —
// where the text sits, how long the lines run, where the blank regions
// are. A crop of real characters could only ever show one corner, so
// every pane's preview looked alike (a prompt, then nothing), which is
// the opposite of what a preview is for.
//
// The result is always exactly maxH x maxW, blank-padded for a pane
// smaller than the thumbnail. Returning fewer rows for a shorter pane
// made the client's centered overlay resize and jump around the screen
// as the selection moved between panes of different heights.
func (c *Core) buildThumbnail(paneID int, maxW, maxH int) [][]proto.Cell {
	if maxW <= 0 || maxH <= 0 {
		return nil
	}
	p, ok := c.panes[paneID]
	if !ok {
		return nil
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	if cols <= 0 || rows <= 0 {
		return nil
	}

	// Never magnify: a pane smaller than the thumbnail's pixel grid is
	// drawn at 1 cell per pixel-pair and simply leaves the rest blank,
	// rather than being stretched into a blocky mess.
	pixW, pixH := maxW*2, maxH*4
	scaleX, scaleY := float64(cols)/float64(pixW), float64(rows)/float64(pixH)
	if scaleX < 1 {
		scaleX = 1
	}
	if scaleY < 1 {
		scaleY = 1
	}

	cells := make([][]proto.Cell, maxH)
	for cy := 0; cy < maxH; cy++ {
		row := make([]proto.Cell, maxW)
		for cx := 0; cx < maxW; cx++ {
			var dots rune
			var fg uint32
			var haveFG bool
			for dy := 0; dy < 4; dy++ {
				for dx := 0; dx < 2; dx++ {
					sx := int(float64(cx*2+dx) * scaleX)
					sy := int(float64(cy*4+dy) * scaleY)
					if sx >= cols || sy >= rows {
						continue
					}
					g := t.Cell(sx, sy)
					if g.Char == 0 || g.Char == ' ' {
						continue
					}
					dots |= brailleDots[dy][dx]
					if !haveFG {
						fg, haveFG = glyphToCell(g).FG, true
					}
				}
			}
			cell := proto.Cell{Ch: ' '}
			if dots != 0 {
				// Colored from the first bit of real text in the block, so
				// a green prompt still reads as green at thumbnail scale.
				cell = proto.Cell{Ch: 0x2800 | dots, FG: fg}
			}
			row[cx] = cell
		}
		cells[cy] = row
	}
	return cells
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
