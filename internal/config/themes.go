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
	// statusBG is a *surface* colour from the palette (its "current
	// line"/"highlight" shade), not its background: with paneBG below
	// painting the panes in the palette's real background, a status bar
	// in that same background would stop reading as a bar at all.
	statusBG, statusFG, paneActiveBG tcell.Color
	// paneBG/paneFG are what a cell the running program left unstyled
	// gets drawn in — i.e. the terminal background and body text colour
	// this palette is really about. See config.Config.PaneBG.
	paneBG, paneFG tcell.Color
}

// themes are deliberately drawn from each project's own well-known,
// widely published palette, not guessed — so a "dracula" pane border
// actually looks like Dracula.
var themes = map[string]theme{
	"dracula": {
		statusBG:     tcell.NewHexColor(0x44475a), // Dracula current line
		statusFG:     tcell.NewHexColor(0xf8f8f2), // Dracula foreground
		paneActiveBG: tcell.NewHexColor(0xbd93f9), // Dracula purple
		paneBG:       tcell.NewHexColor(0x282a36), // Dracula background
		paneFG:       tcell.NewHexColor(0xf8f8f2), // Dracula foreground
	},
	"nord": {
		statusBG:     tcell.NewHexColor(0x3b4252), // nord1
		statusFG:     tcell.NewHexColor(0xd8dee9), // nord4
		paneActiveBG: tcell.NewHexColor(0x88c0d0), // nord8 (frost)
		paneBG:       tcell.NewHexColor(0x2e3440), // nord0 (background)
		paneFG:       tcell.NewHexColor(0xd8dee9), // nord4
	},
	"gruvbox": {
		statusBG:     tcell.NewHexColor(0x3c3836), // gruvbox dark bg1
		statusFG:     tcell.NewHexColor(0xebdbb2), // gruvbox dark fg1
		paneActiveBG: tcell.NewHexColor(0xfe8019), // gruvbox bright orange
		paneBG:       tcell.NewHexColor(0x282828), // gruvbox dark bg0
		paneFG:       tcell.NewHexColor(0xebdbb2), // gruvbox dark fg1
	},
	"catppuccin": {
		statusBG:     tcell.NewHexColor(0x313244), // Catppuccin Mocha surface0
		statusFG:     tcell.NewHexColor(0xcdd6f4), // Catppuccin Mocha text
		paneActiveBG: tcell.NewHexColor(0xcba6f7), // Catppuccin Mocha mauve
		paneBG:       tcell.NewHexColor(0x1e1e2e), // Catppuccin Mocha base
		paneFG:       tcell.NewHexColor(0xcdd6f4), // Catppuccin Mocha text
	},
	"solarized": {
		statusBG:     tcell.NewHexColor(0x073642), // Solarized base02
		statusFG:     tcell.NewHexColor(0x839496), // Solarized base0
		paneActiveBG: tcell.NewHexColor(0x268bd2), // Solarized blue
		paneBG:       tcell.NewHexColor(0x002b36), // Solarized base03
		paneFG:       tcell.NewHexColor(0x839496), // Solarized base0
	},
	"tokyo-night": {
		statusBG:     tcell.NewHexColor(0x292e42), // Tokyo Night bg_highlight
		statusFG:     tcell.NewHexColor(0xc0caf5), // Tokyo Night foreground
		paneActiveBG: tcell.NewHexColor(0x7aa2f7), // Tokyo Night blue
		paneBG:       tcell.NewHexColor(0x1a1b26), // Tokyo Night background
		paneFG:       tcell.NewHexColor(0xc0caf5), // Tokyo Night foreground
	},
	"ubuntu": {
		statusBG:     tcell.NewHexColor(0x772953), // Ubuntu brand aubergine
		statusFG:     tcell.NewHexColor(0xeeeeec), // Ubuntu terminal foreground
		paneActiveBG: tcell.NewHexColor(0xe95420), // Ubuntu brand orange
		paneBG:       tcell.NewHexColor(0x300a24), // Ubuntu terminal aubergine
		paneFG:       tcell.NewHexColor(0xeeeeec), // Ubuntu terminal foreground
	},
	"monokai": {
		statusBG:     tcell.NewHexColor(0x3e3d32), // Monokai line highlight
		statusFG:     tcell.NewHexColor(0xf8f8f2), // Monokai foreground
		paneActiveBG: tcell.NewHexColor(0xa6e22e), // Monokai green
		paneBG:       tcell.NewHexColor(0x272822), // Monokai background
		paneFG:       tcell.NewHexColor(0xf8f8f2), // Monokai foreground
	},
	"one-dark": {
		statusBG:     tcell.NewHexColor(0x3e4451), // One Dark selection
		statusFG:     tcell.NewHexColor(0xabb2bf), // One Dark foreground
		paneActiveBG: tcell.NewHexColor(0x61afef), // One Dark blue
		paneBG:       tcell.NewHexColor(0x282c34), // One Dark background
		paneFG:       tcell.NewHexColor(0xabb2bf), // One Dark foreground
	},
	"everforest": {
		statusBG:     tcell.NewHexColor(0x343f44), // Everforest dark bg1
		statusFG:     tcell.NewHexColor(0xd3c6aa), // Everforest dark fg
		paneActiveBG: tcell.NewHexColor(0xa7c080), // Everforest green
		paneBG:       tcell.NewHexColor(0x2d353b), // Everforest dark bg0
		paneFG:       tcell.NewHexColor(0xd3c6aa), // Everforest dark fg
	},
	"rose-pine": {
		statusBG:     tcell.NewHexColor(0x26233a), // Rosé Pine overlay
		statusFG:     tcell.NewHexColor(0xe0def4), // Rosé Pine text
		paneActiveBG: tcell.NewHexColor(0xc4a7e7), // Rosé Pine iris
		paneBG:       tcell.NewHexColor(0x191724), // Rosé Pine base
		paneFG:       tcell.NewHexColor(0xe0def4), // Rosé Pine text
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
// applyTheme sets cfg's five colors from the named preset, skipping any
// the caller marked as explicitly overridden (a nil map overrides
// nothing). Reports whether the name was one it knows: the config file
// discards that answer, since an unrecognized setting there is tolerated
// in silence like any other, while "set theme ..." typed into a running
// session uses it to say so out loud.
func applyTheme(cfg *Config, name string, overridden map[string]bool) bool {
	t, ok := themes[name]
	if !ok {
		return false
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
	if !overridden["pane-bg"] {
		cfg.PaneBG = t.paneBG
	}
	if !overridden["pane-fg"] {
		cfg.PaneFG = t.paneFG
	}
	return true
}
