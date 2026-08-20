package core

import (
	"regexp"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/proto"
)

// The opener (Ctrl-B u) is termdock's answer to tmux-open/extrakto —
// among the most-installed tmux plugins there are, because "I can see
// the URL/path right there but still have to select it with the mouse"
// is a constant, small annoyance. It scans the active pane's current
// screen (not the full scrollback — what's visible right now) for
// anything that looks like a URL or a filesystem path, lists them
// fuzzy-filterable, and Enter copies the pick via the exact same OSC52
// path a copy-mode yank uses. Deliberately *copies* rather than
// exec'ing a browser/editor: the daemon has no idea whether it's
// running on your laptop or a machine you SSH'd into three hops away,
// and OSC52 is the one mechanism that always lands on the terminal
// you're actually looking at regardless.

var (
	openerURLRe  = regexp.MustCompile(`https?://[^\s'"<>()\[\]]+`)
	openerPathRe = regexp.MustCompile(`(?:~|\.{1,2})?/[A-Za-z0-9_.\-/]{2,}`)
)

type openerState struct {
	query    []rune
	items    []string
	filtered []int
	sel      int
}

func (c *Core) enterOpener() {
	items := c.scanActivePaneForLinks()
	if len(items) == 0 {
		c.statusMsg = "nothing on screen looks like a URL or path"
		return
	}
	c.mode = ModeOpener
	c.opener = openerState{items: items}
	c.refilterOpener()
}

// scanActivePaneForLinks reads the active pane's current viewport (not
// its scrollback — what's on screen right now) and pulls out every
// unique URL/path-looking token, bottom-of-screen (most recent output)
// first, since that's usually what you just looked at.
func (c *Core) scanActivePaneForLinks() []string {
	p, ok := c.panes[c.win().active.ID]
	if !ok {
		return nil
	}
	t := p.Term()
	t.Lock()
	cols, rows := t.Size()
	lines := make([]string, rows)
	for y := 0; y < rows; y++ {
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			ch := t.Cell(x, y).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		lines[y] = sb.String()
	}
	t.Unlock()

	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for y := len(lines) - 1; y >= 0; y-- {
		line := lines[y]
		urlSpans := openerURLRe.FindAllStringIndex(line, -1)
		for _, span := range urlSpans {
			add(line[span[0]:span[1]])
		}
		// A URL's own "//host/path" tail also matches the path pattern
		// on its own — blank out whatever the URL regex already claimed
		// before running the path regex, so it doesn't also turn up as
		// a second, redundant entry for the same substring.
		masked := []byte(line)
		for _, span := range urlSpans {
			for i := span[0]; i < span[1]; i++ {
				masked[i] = ' '
			}
		}
		for _, m := range openerPathRe.FindAllString(string(masked), -1) {
			if m == "/" || m == "./" || m == "../" {
				continue // noise: a bare separator, not an actual path
			}
			add(m)
		}
	}
	return out
}

func (c *Core) handleOpenerKey(key tcell.Key, r rune) Result {
	switch {
	case key == tcell.KeyEsc:
		c.mode = ModeNormal
		c.opener = openerState{}
	case key == tcell.KeyEnter:
		if c.opener.sel < len(c.opener.filtered) {
			text := c.opener.items[c.opener.filtered[c.opener.sel]]
			c.mode = ModeNormal
			c.opener = openerState{}
			c.pushRegister(text)
			return Result{Clipboard: text, HasClipboard: true}
		}
		c.mode = ModeNormal
		c.opener = openerState{}
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.opener.query); n > 0 {
			c.opener.query = c.opener.query[:n-1]
			c.refilterOpener()
		}
	case key == tcell.KeyCtrlU:
		c.opener.query = c.opener.query[:0]
		c.refilterOpener()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.moveOpenerSel(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.moveOpenerSel(1)
	case r != 0 && key == tcell.KeyRune:
		c.opener.query = append(c.opener.query, r)
		c.refilterOpener()
	}
	return Result{}
}

func (c *Core) moveOpenerSel(delta int) {
	n := len(c.opener.filtered)
	if n == 0 {
		return
	}
	c.opener.sel = ((c.opener.sel+delta)%n + n) % n
}

func (c *Core) refilterOpener() {
	query := string(c.opener.query)
	type scored struct{ idx, at int }
	var matches []scored
	for i, item := range c.opener.items {
		if ok, at := fuzzyMatch(query, item); ok {
			matches = append(matches, scored{i, at})
		}
	}
	sort.SliceStable(matches, func(a, b int) bool { return matches[a].at < matches[b].at })
	filtered := make([]int, len(matches))
	for i, m := range matches {
		filtered[i] = m.idx
	}
	c.opener.filtered = filtered
	c.opener.sel = clampi(c.opener.sel, 0, maxi(0, len(filtered)-1))
}

func (c *Core) openerOverlay() *proto.Overlay {
	if c.mode != ModeOpener {
		return nil
	}
	items := make([]string, len(c.opener.filtered))
	for i, idx := range c.opener.filtered {
		items[i] = c.opener.items[idx]
	}
	return &proto.Overlay{
		Title:      "open — type to filter, ↑↓ select, enter copies to clipboard, esc cancel",
		ShowQuery:  true,
		Query:      string(c.opener.query),
		Selectable: true,
		Items:      items,
		Selected:   c.opener.sel,
	}
}
