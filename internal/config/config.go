// Package config reads termdock's optional config file. It's a plain
// "key value" text format (like tmux.conf, minus the command language) so
// there's no extra dependency for parsing it.
//
// Location: $TERMDOCK_CONFIG if set, otherwise
// $XDG_CONFIG_HOME/termdock/termdock.conf, falling back to
// ~/.config/termdock/termdock.conf. A missing file just means defaults —
// it's optional, not an error.
//
// Recognized settings:
//
//	prefix <C-x>          prefix key, e.g. "C-a" (default C-b)
//	mouse <on|off>         enable mouse support (default on)
//	history-limit <n>      scrollback lines kept per pane (default 10000)
//	shell <path>           shell to launch in new panes (default $SHELL)
//	popup-command <cmd>    command to run in the floating popup (Ctrl-B P)
//	                       instead of an interactive shell, e.g. "lazygit"
//	                       (default: the shell, same as a new pane)
//	focus-events <on|off>  forward synthetic terminal focus-in/focus-out
//	                       to a pane when you switch to/away from it
//	                       (default off) — see internal/core/focusevents.go
//	repeat-time <ms>       how long a bare arrow keeps repeating a focus
//	                       move after a prefixed one, in milliseconds
//	                       (default 1000; 0 disables) — see
//	                       internal/core/keys.go
//	bind <key> <action>    rebind one prefix-key command to a different
//	                       key, e.g. "bind M jump-picker" — repeatable,
//	                       one key per line; <key> is a single character
//	                       or "Space"; see internal/core/bindings.go for
//	                       the full list of action names
//	theme <name>           bundled color preset: catppuccin, dracula,
//	                       everforest, gruvbox, monokai, nord, one-dark,
//	                       rose-pine, solarized, tokyo-night or ubuntu
//	                       (run "termdock themes" for the live list).
//	                       Applied before status-bg/status-fg/pane-active-bg
//	                       below, so any of those three still overrides
//	                       it regardless of which comes first in the file.
//	                       An unrecognized name is ignored, like every
//	                       other setting here
//	status-bg <color>      status bar background (default black)
//	status-fg <color>      status bar foreground (default silver)
//	pane-active-bg <color> active pane's border/title color (default teal)
//	status-segments <list> comma-separated optional status-bar segments,
//	                       e.g. "git,battery,cpu,mem" (default: none)
//
// Colors accept any W3C name tcell understands ("black", "teal", ...) or
// a "#rrggbb" hex value.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Config is every setting termdock reads from the config file, already
// merged with defaults.
type Config struct {
	Prefix         tcell.Key
	Mouse          bool
	HistoryLimit   int
	Shell          string
	PopupCommand   string          // command to run in the popup instead of an interactive shell; see internal/core/popup.go
	FocusEvents    bool            // forward synthetic pane focus-in/out; see internal/core/focusevents.go
	RepeatTime     int             // ms a bare arrow keeps repeating a focus move after the prefixed one; 0 disables. See internal/core/keys.go
	BindOverrides  map[rune]string // "bind" lines: key -> action name; see internal/core/bindings.go
	StatusBG       tcell.Color
	StatusFG       tcell.Color
	PaneActiveBG   tcell.Color
	StatusSegments []string // optional status-bar segments; see internal/core/segments.go
}

// Default returns the built-in settings, used for anything the config
// file doesn't mention (or when there is no config file at all).
func Default() Config {
	return Config{
		Prefix:       tcell.KeyCtrlB,
		Mouse:        true,
		HistoryLimit: 10000,
		RepeatTime:   1000,
		StatusBG:     tcell.ColorBlack,
		StatusFG:     tcell.ColorSilver,
		PaneActiveBG: tcell.ColorTeal,
	}
}

// Load reads the config file, if any, and returns the effective settings.
// It never fails: a missing or unreadable file, or a bad line in it,
// just falls back to the default for that setting.
func Load() Config {
	cfg := Default()
	path := Path()
	if path == "" {
		return cfg
	}
	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()

	themeName := ""
	overridden := map[string]bool{} // color keys the file set explicitly, so a theme (applied below) can't clobber them
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key, val := fields[0], strings.Join(fields[1:], " ")
		if key == "theme" {
			themeName = val
			continue
		}
		if key == "bind" {
			applyBindLine(&cfg, fields[1:])
			continue
		}
		applySetting(&cfg, key, val)
		switch key {
		case "status-bg", "status-fg", "pane-active-bg":
			// Only a color that actually parsed counts as "the user set
			// this deliberately"; a typo'd one has to leave the door open
			// for a "theme" line to fill it in, the same as if the line
			// weren't there at all.
			if _, ok := parseColor(val); ok {
				overridden[key] = true
			}
		}
	}
	if themeName != "" {
		applyTheme(&cfg, themeName, overridden)
	}
	return cfg
}

func applySetting(cfg *Config, key, val string) {
	switch key {
	case "prefix":
		if k, ok := parseKeyChord(val); ok {
			cfg.Prefix = k
		}
	case "mouse":
		if b, ok := parseBool(val); ok {
			cfg.Mouse = b
		}
	case "focus-events":
		if b, ok := parseBool(val); ok {
			cfg.FocusEvents = b
		}
	case "history-limit":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			cfg.HistoryLimit = n
		}
	case "repeat-time":
		// 0 is meaningful here (disables repeating), unlike history-limit.
		if n, err := strconv.Atoi(val); err == nil && n >= 0 {
			cfg.RepeatTime = n
		}
	case "shell":
		cfg.Shell = val
	case "popup-command":
		cfg.PopupCommand = val
	case "status-bg":
		if c, ok := parseColor(val); ok {
			cfg.StatusBG = c
		}
	case "status-fg":
		if c, ok := parseColor(val); ok {
			cfg.StatusFG = c
		}
	case "pane-active-bg":
		if c, ok := parseColor(val); ok {
			cfg.PaneActiveBG = c
		}
	case "status-segments":
		cfg.StatusSegments = nil
		for _, s := range strings.Split(val, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.StatusSegments = append(cfg.StatusSegments, s)
			}
		}
	}
}

// parseBool reads an on/off setting, case-insensitively, reporting
// whether it recognized the value at all. That second return is the whole
// point: "cfg.Mouse = val == \"on\"" quietly turned the mouse *off* for
// "mouse ON", "mouse 1", or any typo, when this package's contract (see
// Load) is that an unrecognized value leaves the default alone. Silently
// disabling a feature because of a capital letter is exactly the kind of
// thing nobody thinks to suspect the config file for.
func parseBool(val string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "on", "true", "yes", "1":
		return true, true
	case "off", "false", "no", "0":
		return false, true
	}
	return false, false
}

// parseColor resolves a color name or "#rrggbb", reporting whether it
// meant anything. tcell.GetColor answers ColorDefault for a name it
// doesn't know, so assigning its result blindly turned a typo'd color
// into "whatever the terminal does by default" *and* marked the setting
// as explicitly overridden, which then stopped a "theme" line from
// filling it in. "default" itself stays a legitimate thing to ask for.
func parseColor(val string) (tcell.Color, bool) {
	val = strings.TrimSpace(val)
	c := tcell.GetColor(val)
	if c == tcell.ColorDefault && !strings.EqualFold(val, "default") {
		return c, false
	}
	return c, true
}

// applyBindLine handles one "bind <key> <action>" line — fields is
// everything after "bind" itself. Silently ignored (same leniency as
// every other bad setting) if it isn't exactly a 2-token "<key>
// <action>" pair, or <key> isn't a single character or "Space"; the
// action name itself isn't validated here (core doesn't get imported by
// this package — see BindOverrides' doc comment), only when
// core.Core.SetBindOverrides applies it.
func applyBindLine(cfg *Config, fields []string) {
	if len(fields) != 2 {
		return
	}
	r, ok := parseBindKey(fields[0])
	if !ok {
		return
	}
	if cfg.BindOverrides == nil {
		cfg.BindOverrides = map[rune]string{}
	}
	cfg.BindOverrides[r] = fields[1]
}

// parseBindKey understands a single character, or "Space" (a literal
// space can't survive being split as a whitespace-delimited config
// token, so it needs a name instead — the same reason termdock.conf's
// key/value format doesn't have a way to spell a space as a bare
// character anywhere else either).
func parseBindKey(s string) (rune, bool) {
	if s == "Space" {
		return ' ', true
	}
	r := []rune(s)
	if len(r) != 1 {
		return 0, false
	}
	return r[0], true
}

// parseKeyChord understands "C-<letter>" (the only form the prefix key
// needs: any Ctrl+letter combination).
func parseKeyChord(s string) (tcell.Key, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 3 && (s[0] == 'C' || s[0] == 'c') && s[1] == '-' {
		l := s[2]
		switch {
		case l >= 'a' && l <= 'z':
			return tcell.KeyCtrlA + tcell.Key(l-'a'), true
		case l >= 'A' && l <= 'Z':
			return tcell.KeyCtrlA + tcell.Key(l-'A'), true
		}
	}
	return 0, false
}

// Path returns where termdock looks for its config file — $TERMDOCK_CONFIG
// if set, else $XDG_CONFIG_HOME/termdock/termdock.conf, else
// ~/.config/termdock/termdock.conf. It reports the location whether or
// not a file is actually there, since the useful thing to tell someone
// who has no config yet is where to create one.
func Path() string {
	if p := os.Getenv("TERMDOCK_CONFIG"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "termdock", "termdock.conf")
}
