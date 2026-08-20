package core

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// The help screen (Ctrl-B ?) is a scrollable reference for every
// keybinding, reusing the same Overlay machinery as the jump picker (see
// picker.go) but with ShowQuery/Selectable off: it's a fixed list, not a
// filter-and-jump. Replaces cramming the whole binding list into a single
// status bar line (still there, transiently, while the prefix is held —
// see cheatSheet in frame.go), which just gets clipped on a narrow
// terminal and is gone the moment you release the prefix key.
type helpEntry struct{ key, desc string }

type helpState struct {
	scroll  int
	entries []helpEntry // snapshotted from live bindings when help opened; see enterHelp/liveHelpEntries
}

func (c *Core) enterHelp() {
	c.mode = ModeHelp
	c.help = helpState{entries: c.liveHelpEntries()}
}

func (c *Core) handleHelpKey(key tcell.Key, r rune) {
	n := len(c.help.entries)
	switch {
	case key == tcell.KeyUp || r == 'k':
		c.help.scroll = maxi(0, c.help.scroll-1)
	case key == tcell.KeyDown || r == 'j':
		c.help.scroll = minInt(n-1, c.help.scroll+1)
	case key == tcell.KeyPgUp:
		c.help.scroll = maxi(0, c.help.scroll-10)
	case key == tcell.KeyPgDn:
		c.help.scroll = minInt(n-1, c.help.scroll+10)
	default:
		c.mode = ModeNormal
	}
}

// helpOverlay builds the client-facing snapshot of the help screen, or
// nil when it isn't open.
func (c *Core) helpOverlay() *proto.Overlay {
	if c.mode != ModeHelp {
		return nil
	}
	entries := c.help.entries
	maxKey := 0
	for _, e := range entries {
		if l := len([]rune(e.key)); l > maxKey {
			maxKey = l
		}
	}
	items := make([]string, len(entries))
	for i, e := range entries {
		items[i] = fmt.Sprintf("%-*s  %s", maxKey, e.key, e.desc)
	}
	return &proto.Overlay{
		Title:    "keybindings (after Ctrl-B) — any key closes, ↑↓/jk/PgUp/PgDn scroll",
		Items:    items,
		Selected: c.help.scroll,
	}
}

// liveHelpEntries builds the keybinding reference from the session's
// actual current bindings (defaults, overridden per-key by config's
// "bind" setting — see bindings.go) rather than a hardcoded list, so a
// rebound key shows up here correctly instead of the help screen
// quietly going stale the moment someone rebinds something. A handful
// of fixed, non-rebindable entries (arrows, Tab, digits, the literal
// double-Ctrl-B passthrough) are appended after the bindings-derived
// ones since they aren't part of c.bindings at all.
func (c *Core) liveHelpEntries() []helpEntry {
	entries := make([]helpEntry, 0, len(actionOrder)+4)
	for _, act := range actionOrder {
		keys := keysForAction(c.bindings, act)
		if len(keys) == 0 {
			continue // every key that used to trigger it has been rebound elsewhere
		}
		labels := make([]string, len(keys))
		for i, r := range keys {
			labels[i] = keyLabel(r)
		}
		entries = append(entries, helpEntry{strings.Join(labels, " / "), actionDescriptions[act]})
	}
	entries = append(entries,
		helpEntry{"←/→/↑/↓", "always alternates for focus-left/right/up/down, even if those are rebound"},
		helpEntry{"Tab", "always an alternate for cycle-focus, even if that's rebound"},
		helpEntry{"0-9", "jump straight to window N"},
		helpEntry{"Ctrl-B Ctrl-B", "send a literal Ctrl-B to the active pane"},
	)
	return entries
}
