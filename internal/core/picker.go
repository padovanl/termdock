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
				w.active = leaf // before setActiveWindowIndex, so its afterWindowSwitch/touchPane stamps the pane we're jumping *to*
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

// previewCols/previewRows bound the picker's live preview pane — a peek,
// not a full pane, so it's kept fixed and small rather than sized to
// match whatever's selected.
const previewCols, previewRows = 36, 8

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
		ov.PreviewCells = c.buildPreview(it.paneID, previewCols, previewRows)
	}
	return ov
}

// buildPreview snapshots the bottom-left corner of paneID's current
// terminal content — the most recently active rows, generally more
// telling at a glance than whatever scrolled to the top — cropped to at
// most maxW x maxH, or nil if the pane's gone or there's no room at all.
// Shared by the jump picker's single preview box (see pickerOverlay) and
// the Ctrl-B g overview's whole grid of them (see overview.go).
func (c *Core) buildPreview(paneID int, maxW, maxH int) [][]proto.Cell {
	p, ok := c.panes[paneID]
	if !ok {
		return nil
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	w, h := minInt(maxW, cols), minInt(maxH, rows)
	if w <= 0 || h <= 0 {
		return nil
	}
	yOff := rows - h
	cells := make([][]proto.Cell, h)
	for y := 0; y < h; y++ {
		row := make([]proto.Cell, w)
		for x := 0; x < w; x++ {
			row[x] = glyphToCell(t.Cell(x, yOff+y))
		}
		cells[y] = row
	}
	return cells
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
