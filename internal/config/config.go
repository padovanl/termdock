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
//	theme <name>           bundled color preset — see ThemeNames — applied
//	                       before status-bg/status-fg/pane-active-bg below,
//	                       so any of those three still overrides it
//	                       regardless of which comes first in the file
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
	PopupCommand   string // command to run in the popup instead of an interactive shell; see internal/core/popup.go
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
	path := findPath()
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
		applySetting(&cfg, key, val)
		if key == "status-bg" || key == "status-fg" || key == "pane-active-bg" {
			overridden[key] = true
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
		cfg.Mouse = val == "on" || val == "true" || val == "yes"
	case "history-limit":
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			cfg.HistoryLimit = n
		}
	case "shell":
		cfg.Shell = val
	case "popup-command":
		cfg.PopupCommand = val
	case "status-bg":
		cfg.StatusBG = tcell.GetColor(val)
	case "status-fg":
		cfg.StatusFG = tcell.GetColor(val)
	case "pane-active-bg":
		cfg.PaneActiveBG = tcell.GetColor(val)
	case "status-segments":
		cfg.StatusSegments = nil
		for _, s := range strings.Split(val, ",") {
			if s = strings.TrimSpace(s); s != "" {
				cfg.StatusSegments = append(cfg.StatusSegments, s)
			}
		}
	}
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

func findPath() string {
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
