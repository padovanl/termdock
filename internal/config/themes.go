package config

import (
	"sort"

	"github.com/gdamore/tcell/v2"
)

// theme bundles the handful of colors termdock itself draws (the status
// bar and the active pane's accent border) into one named preset — the
// equivalent of what tmux users reach for tmux-themepack/Catppuccin/
// Dracula/Nord plugins for, minus the plugin: pick a name instead of
// hand-picking three hex values. Pane *content* is always whatever the
// program running inside it prints — a theme has no say over that, the
// same way it wouldn't for a real terminal.
type theme struct {
	statusBG, statusFG, paneActiveBG tcell.Color
}

// themes are deliberately drawn from each project's own well-known,
// widely published palette, not guessed — so a "dracula" pane border
// actually looks like Dracula.
var themes = map[string]theme{
	"dracula": {
		statusBG:     tcell.NewHexColor(0x282a36), // Dracula background
		statusFG:     tcell.NewHexColor(0xf8f8f2), // Dracula foreground
		paneActiveBG: tcell.NewHexColor(0xbd93f9), // Dracula purple
	},
	"nord": {
		statusBG:     tcell.NewHexColor(0x2e3440), // nord0
		statusFG:     tcell.NewHexColor(0xd8dee9), // nord4
		paneActiveBG: tcell.NewHexColor(0x88c0d0), // nord8 (frost)
	},
	"gruvbox": {
		statusBG:     tcell.NewHexColor(0x282828), // gruvbox dark bg0
		statusFG:     tcell.NewHexColor(0xebdbb2), // gruvbox dark fg1
		paneActiveBG: tcell.NewHexColor(0xfe8019), // gruvbox bright orange
	},
	"catppuccin": {
		statusBG:     tcell.NewHexColor(0x1e1e2e), // Catppuccin Mocha base
		statusFG:     tcell.NewHexColor(0xcdd6f4), // Catppuccin Mocha text
		paneActiveBG: tcell.NewHexColor(0xcba6f7), // Catppuccin Mocha mauve
	},
	"solarized": {
		statusBG:     tcell.NewHexColor(0x002b36), // Solarized base03
		statusFG:     tcell.NewHexColor(0x839496), // Solarized base0
		paneActiveBG: tcell.NewHexColor(0x268bd2), // Solarized blue
	},
	"tokyo-night": {
		statusBG:     tcell.NewHexColor(0x1a1b26), // Tokyo Night background
		statusFG:     tcell.NewHexColor(0xc0caf5), // Tokyo Night foreground
		paneActiveBG: tcell.NewHexColor(0x7aa2f7), // Tokyo Night blue
	},
}

// ThemeNames lists every built-in theme, sorted — what "termdock
// themes" prints, and the only way to discover the valid spellings
// from the program itself, since a misspelled "theme" line is
// deliberately ignored in silence (see applyTheme).
//
// Derived from the themes map rather than written out again beside
// it: as a second hardcoded list it could silently disagree with the
// themes actually available — which is exactly what it did, listing
// six names while nothing checked the two stayed in step except a
// test policing the duplication.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// applyTheme fills in cfg's colors from the named theme, skipping any
// field the config file already set explicitly via its own
// status-bg/status-fg/pane-active-bg line (tracked in overridden) — so a
// theme is always just a baseline, and an explicit color line always
// wins regardless of which of the two comes first in the file. Unknown
// theme names are silently ignored, the same "bad setting, keep the
// default" leniency every other setting in this package already has.
func applyTheme(cfg *Config, name string, overridden map[string]bool) {
	t, ok := themes[name]
	if !ok {
		return
	}
	if !overridden["status-bg"] {
		cfg.StatusBG = t.statusBG
	}
	if !overridden["status-fg"] {
		cfg.StatusFG = t.statusFG
	}
	if !overridden["pane-active-bg"] {
		cfg.PaneActiveBG = t.paneActiveBG
	}
}
