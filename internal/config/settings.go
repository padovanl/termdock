package config

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// This file is the settings *vocabulary*, shared by the two places
// settings come from: termdock.conf (see Load, which stays deliberately
// lenient — a bad line is skipped, never fatal) and the running session's
// own "set" command (see internal/core/settings.go, which needs the
// opposite: telling the user exactly why a value was refused). Both go
// through Set below, so a key can't be understood in a file and rejected
// interactively, or vice versa.

// Scope says who reads a setting, which decides when a change to it can
// take effect.
type Scope int

const (
	// ScopeServer settings live in the session daemon: they affect the
	// session itself, so a change reaches every attached client at once.
	ScopeServer Scope = iota
	// ScopeClient settings are the look of the terminal you're sitting
	// at. Each client reads its own from its own config file, so two
	// people attached to one session can theme it differently — but a
	// runtime change is pushed to all of them, since it was made *to the
	// session* rather than to anyone's file.
	ScopeClient
)

// Setting describes one key: what it means, who reads it, and how to
// render its current value back as text a user could have typed.
type Setting struct {
	Key   string
	Doc   string
	Scope Scope
	// NewPanesOnly marks settings that are read when a pane is created,
	// so changing one leaves existing panes as they are. Worth saying out
	// loud in the UI: otherwise "I set it and nothing happened" looks
	// like a bug.
	NewPanesOnly bool
	get          func(*Config) string
	set          func(*Config, string) error
	// Hint is what a valid value looks like, shown on the row while one
	// is being typed. Settings with choices don't need it — you step
	// through those and read them. The ones that are typed had nothing
	// saying what they would accept, so "popup-command" looked like it
	// took a sentence, and any sentence was duly accepted.
	Hint string
	// choices, when set, lists every value this setting can take, in the
	// order to step through them. It's what lets the settings screen
	// offer left/right to pick one rather than requiring you to know the
	// vocabulary and type it — which for "theme" means arrowing through
	// the palettes and watching each one apply. Settings whose value is a
	// path, a number or free text have no such list and are typed.
	choices func() []string
}

// Choices lists every value the setting can be stepped through, or nil
// if it has no fixed set.
func (s Setting) Choices() []string {
	if s.choices == nil {
		return nil
	}
	return s.choices()
}

var settings = []Setting{
	{
		Key: "prefix", Doc: "prefix key, any Ctrl+letter", Scope: ScopeServer,
		Hint: "a Ctrl+letter chord, e.g. C-a or C-b",
		get:  func(c *Config) string { return keyChordString(c.Prefix) },
		set: func(c *Config, v string) error {
			k, ok := parseKeyChord(v)
			if !ok {
				return fmt.Errorf("expected a Ctrl+letter chord like C-a, got %q", v)
			}
			c.Prefix = k
			return nil
		},
	},
	{
		Key: "mouse", Doc: "mouse support (click, drag, wheel)", Scope: ScopeClient,
		choices: onOffChoices,
		get:     func(c *Config) string { return onOff(c.Mouse) },
		set:     func(c *Config, v string) error { return setBool(&c.Mouse, v) },
	},
	{
		Key: "history-limit", Doc: "scrollback lines kept per pane", Scope: ScopeServer, NewPanesOnly: true,
		Hint: "a whole number of lines, e.g. 2000",
		get:  func(c *Config) string { return strconv.Itoa(c.HistoryLimit) },
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("expected a positive number of lines, got %q", v)
			}
			c.HistoryLimit = n
			return nil
		},
	},
	{
		Key: "shell", Doc: "shell to launch in new panes", Scope: ScopeServer, NewPanesOnly: true,
		Hint: "a path to a shell, e.g. /bin/bash — or default for $SHELL",
		get: func(c *Config) string {
			if c.Shell == "" {
				return "(your $SHELL)"
			}
			return c.Shell
		},
		// Deliberately not checked here: whether the path exists is a
		// question about this machine, not about the value's shape, and
		// Set is also how the config file is read (see applySetting),
		// where silently discarding an unusable shell would swap in
		// $SHELL and hide the mistake completely. The daemon reports it
		// loudly at startup instead (see CheckShell), and the
		// interactive path asks CheckSetting so a typo is caught while
		// you're still looking at it.
		set: func(c *Config, v string) error {
			if v == "default" {
				v = ""
			}
			c.Shell = v
			return nil
		},
	},
	{
		Key: "popup-command", Doc: "what the floating popup runs instead of a shell", Scope: ScopeServer,
		Hint: "a command to run, e.g. htop — or default for a shell",
		get: func(c *Config) string {
			if c.PopupCommand == "" {
				return "(a shell)"
			}
			return c.PopupCommand
		},
		set: func(c *Config, v string) error {
			if v == "default" {
				v = ""
			}
			c.PopupCommand = v
			return nil
		},
	},
	{
		Key: "focus-events", Doc: "forward synthetic pane focus-in/out", Scope: ScopeServer,
		choices: onOffChoices,
		get:     func(c *Config) string { return onOff(c.FocusEvents) },
		set:     func(c *Config, v string) error { return setBool(&c.FocusEvents, v) },
	},
	{
		Key: "repeat-time", Doc: "ms a bare arrow keeps moving focus (0 disables)", Scope: ScopeServer,
		Hint: "milliseconds, e.g. 500 — or 0 to disable",
		get:  func(c *Config) string { return strconv.Itoa(c.RepeatTime) },
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				return fmt.Errorf("expected a number of milliseconds (0 disables), got %q", v)
			}
			c.RepeatTime = n
			return nil
		},
	},
	{
		Key: "theme", Doc: "bundled color preset (sets the five colors below)", Scope: ScopeClient,
		choices: func() []string { return append([]string{"none"}, ThemeNames()...) },
		// "none" rather than the parenthesised "(none)" the other unset
		// settings use: those have no value you could type, while this
		// one does — "set theme none" is a real thing to write, it's the
		// first entry in the list ←→ steps through, and it's what saving
		// an un-themed session to the config file writes.
		get: func(c *Config) string {
			if c.Theme == "" {
				return "none"
			}
			return c.Theme
		},
		set: func(c *Config, v string) error {
			if v == "none" || v == "default" {
				*c = withColorsOf(*c, Default())
				c.Theme = ""
				return nil
			}
			if !applyTheme(c, v, nil) {
				return fmt.Errorf("no bundled theme called %q — try one of: %s", v, strings.Join(ThemeNames(), ", "))
			}
			c.Theme = v
			return nil
		},
	},
	colorSetting("status-bg", "status bar background", func(c *Config) *tcell.Color { return &c.StatusBG }),
	colorSetting("status-fg", "status bar foreground", func(c *Config) *tcell.Color { return &c.StatusFG }),
	colorSetting("pane-active-bg", "active pane's border and title", func(c *Config) *tcell.Color { return &c.PaneActiveBG }),
	colorSetting("pane-bg", "background behind unstyled pane content", func(c *Config) *tcell.Color { return &c.PaneBG }),
	colorSetting("pane-fg", "foreground for unstyled pane content", func(c *Config) *tcell.Color { return &c.PaneFG }),
	{
		// The values are not listed here the way other settings list
		// theirs: this one is stepped with ←→, so they read themselves out
		// as you go. What stepping cannot tell you is that one of them
		// needs something installed, so the row spends its width on that
		// instead — same length, strictly more than you could find out on
		// your own.
		Key: "status-icons", Doc: "icons before status segments (nerd needs a font)", Scope: ScopeClient,
		get: func(c *Config) string {
			if c.StatusIcons == "" {
				return "off"
			}
			return c.StatusIcons
		},
		set: func(c *Config, v string) error {
			// "on" kept working: it is what this setting took when it was
			// a bool, so it is in config files already written.
			if v == "on" {
				v = "nerd"
			}
			switch v {
			case "off", "unicode", "nerd":
				c.StatusIcons = v
				return nil
			}
			return fmt.Errorf("expected off, unicode or nerd")
		},
		choices: statusIconChoices,
	},
	{
		Key: "status-segments", Doc: "extra status-bar segments (git,battery,cpu,mem)", Scope: ScopeServer,
		Hint: "any of git,battery,cpu,mem — or none",
		get: func(c *Config) string {
			if len(c.StatusSegments) == 0 {
				return "(none)"
			}
			return strings.Join(c.StatusSegments, ",")
		},
		set: func(c *Config, v string) error {
			if v == "none" {
				c.StatusSegments = nil
				return nil
			}
			var segs []string
			for _, s := range strings.Split(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					if !validSegments[s] {
						return fmt.Errorf("no status segment called %q — try one of: %s", s, strings.Join(segmentNames(), ", "))
					}
					segs = append(segs, s)
				}
			}
			c.StatusSegments = segs
			return nil
		},
	},
}

var validSegments = map[string]bool{"git": true, "battery": true, "cpu": true, "mem": true}

func segmentNames() []string {
	names := make([]string, 0, len(validSegments))
	for s := range validSegments {
		names = append(names, s)
	}
	sort.Strings(names)
	return names
}

func colorSetting(key, doc string, field func(*Config) *tcell.Color) Setting {
	return Setting{
		Key: key, Doc: doc, Scope: ScopeClient,
		Hint: "a #rrggbb hex colour or a name, e.g. #1e1e2e or blue",
		get:  func(c *Config) string { return colorString(*field(c)) },
		set: func(c *Config, v string) error {
			col, ok := parseColor(v)
			if !ok {
				return fmt.Errorf("expected a color name or #rrggbb, got %q", v)
			}
			*field(c) = col
			// A hand-picked color is no longer "the theme's" — say so, so
			// the theme line doesn't claim credit for a look it no longer
			// fully describes.
			c.Theme = ""
			return nil
		},
	}
}

// Settings lists every settable key, in the order the config file's own
// documentation introduces them.
func Settings() []Setting { return settings }

// Lookup finds one setting by key.
func Lookup(key string) (Setting, bool) {
	for _, s := range settings {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}

// Keys lists every settable key.
func Keys() []string {
	out := make([]string, len(settings))
	for i, s := range settings {
		out[i] = s.Key
	}
	return out
}

// Set applies one "key value" pair to cfg, reporting why if it can't.
// This is the same path the config file takes (see Load), so the two can
// never understand a key differently — Load just discards the error,
// keeping a bad line in a file non-fatal.
func Set(cfg *Config, key, value string) error {
	s, ok := Lookup(key)
	if !ok {
		return fmt.Errorf("no setting called %q — try one of: %s", key, strings.Join(Keys(), ", "))
	}
	return s.set(cfg, value)
}

func onOffChoices() []string { return []string{"on", "off"} }

// statusIconChoices orders the sets so that stepping ←→ from the default
// tries the one every font can draw before the one that needs a patched
// font — see Config.StatusIcons.
func statusIconChoices() []string { return []string{"off", "unicode", "nerd"} }

// Step moves a setting to the next value in its own list, delta places
// along and wrapping at both ends, returning what it landed on. Reports
// false for a setting with no fixed set of values — those get typed.
//
// Where it currently sits is found by asking Get, so a setting resting
// on something outside its list (a colour picked by hand leaves "theme"
// reading "(none)") starts from one end rather than refusing to move.
func Step(cfg *Config, key string, delta int) (string, bool) {
	s, ok := Lookup(key)
	if !ok {
		return "", false
	}
	choices := s.Choices()
	if len(choices) == 0 {
		return "", false
	}
	current := s.get(cfg)
	idx := -1
	for i, v := range choices {
		if v == current {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Sitting on nothing in the list: step forward onto the first
		// entry, back onto the last.
		idx = -1
		if delta < 0 {
			idx = 0
		}
	}
	n := len(choices)
	next := choices[((idx+delta)%n+n)%n]
	if err := s.set(cfg, next); err != nil {
		return "", false
	}
	return next, true
}

// CheckSetting reports whether a setting that's already been accepted
// will actually work on this machine — a question separate from whether
// the value had the right shape, and one only the interactive path asks.
// Reading the config file deliberately skips it: a "shell" that isn't
// installed has to stay a loud failure at session startup (see
// CheckShell) rather than being quietly discarded in favour of $SHELL,
// which would leave the user with no error and the wrong shell.
func CheckSetting(cfg *Config, key string) error {
	switch key {
	case "shell":
		return CheckShell(cfg.Shell)
	case "popup-command":
		return CheckPopupCommand(cfg.PopupCommand)
	}
	return nil
}

// CheckPopupCommand reports whether the popup would actually have
// something to run. The popup opens, the command fails to exec, and the
// popup closes again — from the outside it flashes for a moment and
// vanishes, with nowhere for the error to be shown, so anything typed
// here that isn't a command is indistinguishable from the feature being
// broken. Checked while the value is being set, where there is still a
// screen to complain on.
//
// Only the program is checked, not the arguments: whether `htop -u root`
// likes its flags is htop's business, and guessing at it would refuse
// perfectly good command lines.
func CheckPopupCommand(cmd string) error {
	if cmd == "" {
		return nil // means "a shell", which CheckShell already covers
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return nil
	}
	prog := fields[0]
	// A path is only a command if it is one; a bare name has to be
	// somewhere on PATH.
	if strings.ContainsRune(prog, os.PathSeparator) {
		info, err := os.Stat(prog)
		switch {
		case err != nil:
			return fmt.Errorf("popup-command starts with %s, which cannot be run: %v", prog, err)
		case info.IsDir():
			return fmt.Errorf("popup-command starts with %s, which is a directory, not a program", prog)
		case info.Mode()&0111 == 0:
			return fmt.Errorf("popup-command starts with %s, which is not executable", prog)
		}
		return nil
	}
	if _, err := exec.LookPath(prog); err != nil {
		return fmt.Errorf("popup-command starts with %q, which is not a program on your PATH", prog)
	}
	return nil
}

// Get renders a setting's current value as text the user could have
// typed. Parenthesised answers ("(none)", "(a shell)") describe an unset
// setting rather than naming a literal value.
func Get(cfg *Config, key string) string {
	s, ok := Lookup(key)
	if !ok {
		return ""
	}
	return s.get(cfg)
}

func setBool(field *bool, v string) error {
	b, ok := parseBool(v)
	if !ok {
		return fmt.Errorf("expected on or off, got %q", v)
	}
	*field = b
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// colorString renders a color the way it would be written in the config
// file. tcell keeps named colors and RGB ones apart internally, but hex
// round-trips through GetColor identically, so it's the one spelling that
// always means what it says.
func colorString(c tcell.Color) string {
	if c == tcell.ColorDefault {
		return "default"
	}
	return fmt.Sprintf("#%06x", c.Hex())
}

// keyChordString is parseKeyChord's inverse, for showing the prefix key
// back as something that could be typed into the config file.
func keyChordString(k tcell.Key) string {
	if k >= tcell.KeyCtrlA && k <= tcell.KeyCtrlZ {
		return "C-" + string(rune('a'+int(k-tcell.KeyCtrlA)))
	}
	return fmt.Sprintf("key-%d", int(k))
}

// withColorsOf copies src's five theme colors onto dst, for undoing a
// theme without disturbing anything else that's been set.
func withColorsOf(dst, src Config) Config {
	dst.StatusBG, dst.StatusFG = src.StatusBG, src.StatusFG
	dst.PaneActiveBG, dst.PaneBG, dst.PaneFG = src.PaneActiveBG, src.PaneBG, src.PaneFG
	return dst
}
