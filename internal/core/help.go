package core

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// The help screen (Ctrl-B ?) is a scrollable reference for every
// keybinding, reusing the same Overlay machinery as the jump picker (see
// picker.go) but with ShowQuery/Selectable off: it's a fixed list, not a
// filter-and-jump. Replaces cramming the whole binding list into a single
// status bar line (still there, transiently, while the prefix is held —
// see helpText in frame.go), which just gets clipped on a narrow
// terminal and is gone the moment you release the prefix key.
type helpEntry struct{ key, desc string }

var helpEntries = []helpEntry{
	{"v / %", "split vertically (side by side)"},
	{"s / \"", "split horizontally (stacked)"},
	{"h/j/k/l or arrows", "move focus to the adjacent pane"},
	{"o / Tab", "cycle to the next pane"},
	{"z", "zoom the active pane full-screen (again to undo)"},
	{"r", "resize-mode: arrows/hjkl resize, any other key exits"},
	{"[", "enter copy-mode (scroll/select the scrollback)"},
	{"]", "paste the last yank"},
	{"=", "paste register picker: fuzzy-pick an older yank to paste"},
	{"y", "toggle sync-panes (type into every pane at once)"},
	{"c", "create a new window"},
	{"n / p", "switch to the next / previous window"},
	{"w", "jump picker: fuzzy-jump to any window/pane"},
	{"g", "overview: a live-thumbnail grid of every pane"},
	{"/", "search every pane's scrollback at once"},
	{"S", "switch to another session without detaching"},
	{"P", "toggle the floating scratch terminal"},
	{"u", "open picker: fuzzy-pick a URL/path spotted on screen"},
	{"!", "break the active pane out into its own new window"},
	{"Q", "quick-jump: press a pane's number to jump straight to it"},
	{":", "command prompt: type a command (new-window, split-window, ...)"},
	{"Space", "cycle the active window through preset layouts"},
	{"R", "respawn-pane: restart the shell in the active pane, in place"},
	{"L", "toggle logging the active pane's output to a file"},
	{"0-9", "jump straight to window N"},
	{",", "rename the current window"},
	{"&", "close the current window and every pane in it (asks first)"},
	{"x", "close the active pane"},
	{"d", "detach (session keeps running)"},
	{"q", "quit (ends the whole session)"},
	{"?", "toggle this help screen"},
	{"Ctrl-B Ctrl-B", "send a literal Ctrl-B to the active pane"},
}

type helpState struct {
	scroll int
}

func (c *Core) enterHelp() {
	c.mode = ModeHelp
	c.help = helpState{}
}

func (c *Core) handleHelpKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyUp || r == 'k':
		c.help.scroll = maxi(0, c.help.scroll-1)
	case key == tcell.KeyDown || r == 'j':
		c.help.scroll = minInt(len(helpEntries)-1, c.help.scroll+1)
	case key == tcell.KeyPgUp:
		c.help.scroll = maxi(0, c.help.scroll-10)
	case key == tcell.KeyPgDn:
		c.help.scroll = minInt(len(helpEntries)-1, c.help.scroll+10)
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
	maxKey := 0
	for _, e := range helpEntries {
		if l := len(e.key); l > maxKey {
			maxKey = l
		}
	}
	items := make([]string, len(helpEntries))
	for i, e := range helpEntries {
		items[i] = fmt.Sprintf("%-*s  %s", maxKey, e.key, e.desc)
	}
	return &proto.Overlay{
		Title:    "keybindings (after Ctrl-B) — any key closes, ↑↓/jk/PgUp/PgDn scroll",
		Items:    items,
		Selected: c.help.scroll,
	}
}
