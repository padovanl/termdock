package core

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
)

// Global scrollback search (Ctrl-B /): tmux has no equivalent at all —
// copy-mode's own "/" only ever looks within the one pane you're already
// in. This scans every pane's live grid *and* history in every window as
// you type, so "did I see that error anywhere?" doesn't mean checking
// panes one at a time.

// maxSearchResults bounds how much a single keystroke can cost: with
// several panes each holding a full 10000-line scrollback, an unbounded
// scan would get slower exactly when it's most tempting to keep typing.
const maxSearchResults = 200

type searchResult struct {
	windowIdx int
	paneID    int
	line      int    // absolute row index (history + live) within that pane's buffer
	text      string // the matching line's text, trimmed, for display
}

type globalSearchState struct {
	query   []rune
	results []searchResult
	sel     int
}

func (c *Core) enterGlobalSearch() {
	c.mode = ModeSearch
	c.search = globalSearchState{}
}

func (c *Core) handleSearchKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.mode = ModeNormal
		c.search = globalSearchState{}
	case key == tcell.KeyEnter:
		c.confirmSearch()
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.search.query); n > 0 {
			c.search.query = c.search.query[:n-1]
			c.refilterSearch()
		}
	case key == tcell.KeyCtrlU:
		c.search.query = c.search.query[:0]
		c.refilterSearch()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.moveSearchSel(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.moveSearchSel(1)
	case r != 0 && key == tcell.KeyRune:
		c.search.query = append(c.search.query, r)
		c.refilterSearch()
	}
}

func (c *Core) moveSearchSel(delta int) {
	n := len(c.search.results)
	if n == 0 {
		return
	}
	c.search.sel = ((c.search.sel+delta)%n + n) % n
}

// confirmSearch jumps to the selected match's window and pane, then
// enters copy-mode with the view scrolled and the cursor placed right on
// the matching line — not just "the right pane," the exact spot.
func (c *Core) confirmSearch() {
	jumped := false
	if c.search.sel < len(c.search.results) {
		res := c.search.results[c.search.sel]
		if res.windowIdx < len(c.windows) {
			w := c.windows[res.windowIdx]
			if leaf := findLeafByID(w.root, res.paneID); leaf != nil {
				w.active = leaf // before setActiveWindowIndex, so its afterWindowSwitch/touchPane stamps the pane we're jumping *to*
				c.setActiveWindowIndex(res.windowIdx)
				c.enterCopyMode() // sets c.mode = ModeCopy — must not be clobbered below
				if _, rows, total, ok := c.copyPaneDims(); ok {
					c.copy.curY = clampi(res.line, 0, maxi(0, total-1))
					c.copy.curX = 0
					c.copy.top = clampi(res.line-rows/2, 0, maxi(0, total-rows))
				}
				jumped = true
			}
		}
	}
	if !jumped {
		c.mode = ModeNormal
	}
	c.search = globalSearchState{}
	c.relayoutLocked()
}

func (c *Core) refilterSearch() {
	re := compileSearch(string(c.search.query))
	var results []searchResult
	if re != nil {
		for wi, w := range c.windows {
			if len(results) >= maxSearchResults {
				break
			}
			for _, l := range layout.Leaves(w.root) {
				if len(results) >= maxSearchResults {
					break
				}
				p, ok := c.panes[l.ID]
				if !ok {
					continue
				}
				results = append(results, searchPane(wi, l.ID, p, re, maxSearchResults-len(results))...)
			}
		}
	}
	c.search.results = results
	c.search.sel = clampi(c.search.sel, 0, maxi(0, len(results)-1))
}

func searchPane(windowIdx, paneID int, p *pane.Pane, re *regexp.Regexp, limit int) []searchResult {
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	total := hl + rows

	var out []searchResult
	for y := 0; y < total && len(out) < limit; y++ {
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			g := cellAt(t, hl, y, x)
			ch := g.Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		line := strings.TrimRight(sb.String(), " ")
		if line == "" {
			continue
		}
		if re.MatchString(line) {
			out = append(out, searchResult{windowIdx: windowIdx, paneID: paneID, line: y, text: line})
		}
	}
	return out
}

func (c *Core) searchOverlay() *proto.Overlay {
	if c.mode != ModeSearch {
		return nil
	}
	items := make([]string, len(c.search.results))
	for i, res := range c.search.results {
		wname := "?"
		if res.windowIdx < len(c.windows) {
			wname = c.windowDisplayName(c.windows[res.windowIdx])
		}
		items[i] = fmt.Sprintf("%d:%s › %s", res.windowIdx, wname, truncateRunes(res.text, 60))
	}
	title := "search every pane's scrollback (regex or text) — type to search, ↑↓ select, enter jump, esc cancel"
	if len(items) == maxSearchResults {
		title += fmt.Sprintf(" (showing first %d matches)", maxSearchResults)
	}
	return &proto.Overlay{
		Title:      title,
		ShowQuery:  true,
		Query:      string(c.search.query),
		Selectable: true,
		Items:      items,
		Selected:   c.search.sel,
	}
}
