package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/vt10x"
)

// Ctrl-B H is a fuzzy picker over every command run in this session —
// any pane, any window — with how it exited and how long it took, newest
// first. Enter types the chosen one back into the current pane.
//
// Your shell's own history cannot be this. It is per-shell, so a command
// run in another pane is invisible; it records what you typed and not
// what happened, so "the one that worked" is indistinguishable from the
// three attempts before it; and it is written at exit, so a pane still
// open has not contributed to it yet. Here the session already knows,
// because the marks say where each command was and how it ended (see
// internal/vt10x/marks.go).
//
// It is read out of the panes' own scrollback rather than accumulated as
// commands run: the buffer is the record, so there is no second copy to
// keep in step, and what the picker offers is exactly what is still
// scrolled back far enough to look at.

// historyEntry is one command that ran somewhere in this session.
type historyEntry struct {
	command string
	pane    string // where it ran, as the picker's label
	exit    int    // -1 when the shell reported no status
	dur     time.Duration
	at      time.Time
}

type historyPickerState struct {
	query    []rune
	items    []historyEntry
	filtered []int
	sel      int
}

// collectHistory reads every pane's marks and reconstructs the commands.
// A command's text is the part of the prompt line after the prompt
// itself — which is exactly what the B mark delimits, and the reason
// termdock can show the command rather than just "something ran here".
func (c *Core) collectHistory() []historyEntry {
	var out []historyEntry
	for wi, w := range c.windows {
		for pi, leaf := range layout.Leaves(w.root) {
			p, ok := c.panes[leaf.ID]
			if !ok {
				continue
			}
			label := fmt.Sprintf("%d:%s › %d", wi, c.windowDisplayName(w), pi+1)
			out = append(out, c.paneHistory(p, label)...)
		}
	}
	// Newest first: the thing you want is nearly always something you ran
	// a moment ago, in this pane or the one next to it.
	sort.SliceStable(out, func(a, b int) bool { return out[a].at.After(out[b].at) })
	return out
}

// paneHistory pulls the commands out of one pane's buffer.
func (c *Core) paneHistory(p paneReader, label string) []historyEntry {
	t := p.Term()
	marks := t.Marks()
	if len(marks) == 0 {
		return nil
	}
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	total := hl + rows

	var out []historyEntry
	for i, m := range marks {
		if m.Kind != vt10x.MarkInput {
			continue
		}
		// The command is what was typed after the prompt: from this mark's
		// line to the next mark, which is the C that starts its output.
		var ran, done *vt10x.Mark
		for j := i + 1; j < len(marks); j++ {
			switch marks[j].Kind {
			case vt10x.MarkOutput:
				if ran == nil {
					ran = &marks[j]
				}
			case vt10x.MarkDone:
				if done == nil {
					done = &marks[j]
				}
			case vt10x.MarkPrompt:
				j = len(marks) // a new prompt ends this command's story
			}
			if ran != nil && done != nil {
				break
			}
		}
		if m.Line < 0 || m.Line >= total {
			continue
		}
		// From the mark's own column: everything left of it is the
		// prompt, everything right of it is what was typed.
		text := strings.TrimSpace(lineTextFrom(t, hl, m.Line, m.Col, cols))
		if text == "" {
			continue
		}
		e := historyEntry{command: text, pane: label, exit: -1, at: m.At}
		if done != nil {
			e.exit = done.Exit
			if ran != nil {
				e.dur = done.At.Sub(ran.At)
			}
			e.at = done.At
		}
		out = append(out, e)
	}
	return out
}

// paneReader is the slice of *pane.Pane this file needs, named so the
// history logic can be exercised without a live pty.
type paneReader interface {
	Term() vt10x.Terminal
}

// lineTextFrom renders one absolute row from column start onwards.
func lineTextFrom(t vt10x.Terminal, historyLen, y, start, cols int) string {
	var sb strings.Builder
	if start < 0 {
		start = 0
	}
	for x := start; x < cols; x++ {
		ch := cellAt(t, historyLen, y, x).Char
		if ch == 0 {
			ch = ' '
		}
		sb.WriteRune(ch)
	}
	return strings.TrimRight(sb.String(), " ")
}

func (c *Core) enterHistoryPicker() {
	items := c.collectHistory()
	if len(items) == 0 {
		c.statusMsg = noMarksHint
		return
	}
	c.mode = ModeHistory
	c.history = historyPickerState{items: items}
	c.refilterHistory()
}

func (c *Core) handleHistoryKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.mode = ModeNormal
		c.history = historyPickerState{}
	case key == tcell.KeyEnter:
		c.confirmHistory()
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.history.query); n > 0 {
			c.history.query = c.history.query[:n-1]
			c.refilterHistory()
		}
	case key == tcell.KeyCtrlU:
		c.history.query = c.history.query[:0]
		c.refilterHistory()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.moveHistorySel(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.moveHistorySel(1)
	case r != 0 && key == tcell.KeyRune:
		c.history.query = append(c.history.query, r)
		c.refilterHistory()
	}
}

// confirmHistory types the command into the active pane without running
// it. Deliberately: the picker is a list of things that have already
// happened, some of which failed, and running one straight off a fuzzy
// match is how you delete the wrong directory. It lands on the command
// line ready to edit or confirm.
func (c *Core) confirmHistory() {
	if c.history.sel < len(c.history.filtered) {
		e := c.history.items[c.history.filtered[c.history.sel]]
		c.writeToActive(e.command)
		c.statusMsg = "typed it into this pane — press enter to run it"
	}
	c.mode = ModeNormal
	c.history = historyPickerState{}
}

func (c *Core) moveHistorySel(delta int) {
	n := len(c.history.filtered)
	if n == 0 {
		return
	}
	c.history.sel = ((c.history.sel+delta)%n + n) % n
}

func (c *Core) refilterHistory() {
	query := string(c.history.query)
	var filtered []int
	seen := map[string]bool{}
	for i, e := range c.history.items {
		// The same command run five times is one entry: a history you
		// scroll through is useless if the last thing you did fills it.
		if seen[e.command] {
			continue
		}
		if ok, _ := fuzzyMatch(query, e.command+" "+e.pane); !ok {
			continue
		}
		seen[e.command] = true
		filtered = append(filtered, i)
	}
	c.history.filtered = filtered
	c.history.sel = clampi(c.history.sel, 0, maxi(0, len(filtered)-1))
}

func (c *Core) historyOverlay() *proto.Overlay {
	if c.mode != ModeHistory {
		return nil
	}
	items := make([]string, len(c.history.filtered))
	for i, idx := range c.history.filtered {
		e := c.history.items[idx]
		var note []string
		if e.exit > 0 {
			note = append(note, fmt.Sprintf("✗%d", e.exit))
		}
		if e.dur >= slowCommand {
			note = append(note, e.dur.Round(time.Second).String())
		}
		note = append(note, e.pane)
		items[i] = fmt.Sprintf("%-50s  %s", truncate(e.command, 50), strings.Join(note, " "))
	}
	return &proto.Overlay{
		Title:      "command history — type to filter, ↑↓ select, enter types it into this pane, esc cancel",
		ShowQuery:  true,
		Query:      string(c.history.query),
		Selectable: true,
		Items:      items,
		Selected:   c.history.sel,
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
