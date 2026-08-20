package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/proto"
)

// Named/numbered paste registers (Ctrl-B =) — tmux calls the equivalent
// choose-buffer, bound to the same key. Ctrl-B ] alone still pastes the
// single most recent yank with no picker involved, unchanged from
// before; = is for reaching further back.

const maxRegisters = 20

type registerEntry struct {
	text string
}

type registerPickerState struct {
	query    []rune
	filtered []int // indices into c.registers
	sel      int
}

// pushRegister records a fresh yank as the new most-recent register,
// evicting the oldest once there are more than maxRegisters — a ring of
// recent copies, not an archive.
func (c *Core) pushRegister(text string) {
	if text == "" {
		return
	}
	c.registers = append([]registerEntry{{text: text}}, c.registers...)
	if len(c.registers) > maxRegisters {
		c.registers = c.registers[:maxRegisters]
	}
}

// pasteRegister writes the most recent register straight into the active
// pane, the counterpart to y/Enter in copy-mode. Sent as plain bytes, not
// wrapped in a bracketed-paste escape: vt10x doesn't track whether the
// foreground app asked for one, and guessing wrong would leak literal
// escape codes into whatever's running.
func (c *Core) pasteRegister() {
	if len(c.registers) == 0 {
		return
	}
	c.writeToActive(c.registers[0].text)
}

func (c *Core) writeToActive(text string) {
	if p, ok := c.panes[c.win().active.ID]; ok {
		p.Write([]byte(text))
	}
}

func (c *Core) enterRegisterPicker() {
	if len(c.registers) == 0 {
		c.statusMsg = "no yanks yet"
		return
	}
	c.mode = ModeRegisters
	c.regPicker = registerPickerState{}
	c.refilterRegisters()
}

func (c *Core) handleRegisterKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.mode = ModeNormal
		c.regPicker = registerPickerState{}
	case key == tcell.KeyEnter:
		if c.regPicker.sel < len(c.regPicker.filtered) {
			idx := c.regPicker.filtered[c.regPicker.sel]
			c.writeToActive(c.registers[idx].text)
		}
		c.mode = ModeNormal
		c.regPicker = registerPickerState{}
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.regPicker.query); n > 0 {
			c.regPicker.query = c.regPicker.query[:n-1]
			c.refilterRegisters()
		}
	case key == tcell.KeyCtrlU:
		c.regPicker.query = c.regPicker.query[:0]
		c.refilterRegisters()
	case key == tcell.KeyUp || key == tcell.KeyCtrlP:
		c.moveRegisterSel(-1)
	case key == tcell.KeyDown || key == tcell.KeyCtrlN || key == tcell.KeyTab:
		c.moveRegisterSel(1)
	case r != 0 && key == tcell.KeyRune:
		c.regPicker.query = append(c.regPicker.query, r)
		c.refilterRegisters()
	}
}

func (c *Core) moveRegisterSel(delta int) {
	n := len(c.regPicker.filtered)
	if n == 0 {
		return
	}
	c.regPicker.sel = ((c.regPicker.sel+delta)%n + n) % n
}

func (c *Core) refilterRegisters() {
	query := string(c.regPicker.query)
	type scored struct{ idx, at int }
	var matches []scored
	for i, reg := range c.registers {
		if ok, at := fuzzyMatch(query, reg.text); ok {
			matches = append(matches, scored{i, at})
		}
	}
	sort.SliceStable(matches, func(a, b int) bool { return matches[a].at < matches[b].at })
	filtered := make([]int, len(matches))
	for i, m := range matches {
		filtered[i] = m.idx
	}
	c.regPicker.filtered = filtered
	c.regPicker.sel = clampi(c.regPicker.sel, 0, maxi(0, len(filtered)-1))
}

// registerLabel renders a register's text as a single display line: the
// first non-blank line, with a "(+N more)" tag if it held more than one.
func registerLabel(text string) string {
	lines := strings.Split(text, "\n")
	first := strings.TrimSpace(lines[0])
	if first == "" {
		for _, l := range lines[1:] {
			if t := strings.TrimSpace(l); t != "" {
				first = t
				break
			}
		}
	}
	if len(first) > 60 {
		first = first[:60] + "…"
	}
	if len(lines) > 1 {
		suffix := "s"
		if len(lines) == 2 {
			suffix = ""
		}
		first += fmt.Sprintf(" (+%d line%s)", len(lines)-1, suffix)
	}
	return first
}

func (c *Core) registersOverlay() *proto.Overlay {
	if c.mode != ModeRegisters {
		return nil
	}
	items := make([]string, len(c.regPicker.filtered))
	for i, idx := range c.regPicker.filtered {
		items[i] = registerLabel(c.registers[idx].text)
	}
	return &proto.Overlay{
		Title:      "paste register — type to filter, ↑↓ select, enter paste, esc cancel",
		ShowQuery:  true,
		Query:      string(c.regPicker.query),
		Selectable: true,
		Items:      items,
		Selected:   c.regPicker.sel,
	}
}
