package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
)

// Settings can be changed from inside a running session, not only by
// editing termdock.conf and starting over: ":set <key> <value>" from the
// command prompt (Ctrl-B :), or the settings screen (Ctrl-B C) that
// lists every key with its current value and opens the prompt prefilled.
// Both go through the same vocabulary the config file uses (see
// internal/config's settings.go), so there's one set of key names and one
// set of rules about what a value may be.
//
// A change applies to the whole session immediately. For the
// look-and-feel settings that clients normally read from their own file
// (colors, mouse), that means the session starts sending its own values
// with every frame — see Core.clientSettings and proto.ClientSettings —
// so every attached client follows along instead of only the one that
// typed the command.
//
// Nothing is written back to termdock.conf unless asked: ":set -p ..."
// (or S on the settings screen) persists the change, on the grounds that
// silently rewriting a file full of someone's own comments and ordering
// is not a thing to do as a side effect of trying something out.

// ApplyConfig makes cfg the session's effective settings. Called once at
// startup by the server, and again by settingsChanged after every
// interactive change.
func (c *Core) ApplyConfig(cfg config.Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.applyConfigLocked(cfg)
}

func (c *Core) applyConfigLocked(cfg config.Config) {
	c.cfg = cfg
	c.prefixKey = cfg.Prefix
	c.statusSegments = cfg.StatusSegments
	c.popupCommand = cfg.PopupCommand
	c.focusEvents = cfg.FocusEvents
	c.setRepeatTimeLocked(cfg.RepeatTime)
	// Process-global, and read when a pane is created — so this reaches
	// the next pane opened in this session, and leaves existing ones as
	// they are. runSet says so out loud rather than letting it look like
	// the setting didn't take.
	pane.SetDefaults(cfg.Shell, cfg.HistoryLimit)
	c.segCache = segmentCache{} // recompute rather than show the old segments until the TTL lapses
	c.markDirty()
}

// EffectiveConfig returns the session's current settings.
func (c *Core) EffectiveConfig() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

// clientSettings is what Frame ships to attached clients, or nil while
// nothing has changed a look-and-feel setting in this session — in which
// case each client keeps rendering with whatever its own config file
// said, which is the documented per-client-theme behaviour.
func (c *Core) clientSettings() *proto.ClientSettings {
	if !c.clientCfgOverridden {
		return nil
	}
	return &proto.ClientSettings{
		Mouse:        c.cfg.Mouse,
		StatusBG:     uint64(c.cfg.StatusBG),
		StatusFG:     uint64(c.cfg.StatusFG),
		PaneActiveBG: uint64(c.cfg.PaneActiveBG),
		PaneBG:       uint64(c.cfg.PaneBG),
		PaneFG:       uint64(c.cfg.PaneFG),
	}
}

// runSet handles ":set [-p] <key> <value>". With no arguments at all it
// opens the settings screen instead, which is the discoverable way in.
func (c *Core) runSet(args []string) {
	persist := false
	if len(args) > 0 && (args[0] == "-p" || args[0] == "--persist") {
		persist, args = true, args[1:]
	}
	if len(args) == 0 {
		c.enterSettings()
		return
	}
	key := args[0]
	if len(args) == 1 {
		// "set key" with no value reports what it currently is, rather
		// than treating the missing value as an empty one.
		if _, ok := config.Lookup(key); !ok {
			c.statusMsg = fmt.Sprintf("no setting called %q (Ctrl-B : set — with no arguments — lists them)", key)
			return
		}
		c.statusMsg = fmt.Sprintf("%s = %s", key, config.Get(&c.cfg, key))
		return
	}
	value := strings.Join(args[1:], " ")

	updated := c.cfg
	if err := config.Set(&updated, key, value); err != nil {
		c.statusMsg = "set " + key + ": " + err.Error()
		return
	}
	if err := config.CheckSetting(&updated, key); err != nil {
		c.statusMsg = "set " + key + ": " + err.Error()
		return
	}

	if s, ok := config.Lookup(key); ok && s.Scope == config.ScopeClient {
		// From here on this session tells its clients what to look like,
		// rather than each of them deciding for itself.
		c.clientCfgOverridden = true
	}
	c.applyConfigLocked(updated)

	msg := fmt.Sprintf("%s = %s", key, config.Get(&c.cfg, key))
	if s, ok := config.Lookup(key); ok && s.NewPanesOnly {
		msg += " (applies to new panes)"
	}
	if persist {
		if err := c.persistSetting(key, value); err != nil {
			msg += " — but could not be saved: " + err.Error()
		} else {
			msg += " — saved to " + config.Path()
		}
	}
	c.statusMsg = msg
}

// persistSetting rewrites just this one key in termdock.conf, leaving
// every other line — comments, ordering, settings this build doesn't
// even know about — exactly as the user wrote it. Regenerating the file
// from the effective config would be far less code and would quietly
// throw all of that away.
func (c *Core) persistSetting(key, value string) error {
	path := config.Path()
	if path == "" {
		return fmt.Errorf("no config file location could be determined")
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Whatever line ending the file already uses is the one it keeps.
	// This config is as likely to have been written from Windows as from
	// a shell, and rewriting a single line with a bare \n in a CRLF file
	// leaves it mixed — which every editor and diff then has an opinion
	// about, for a change the user only meant to be one line.
	eol := "\n"
	if strings.Contains(string(existing), "\r\n") {
		eol = "\r\n"
	}

	line := key + " " + value
	var out []string
	replaced := false
	for _, l := range strings.Split(strings.TrimRight(string(existing), "\r\n"), "\n") {
		l = strings.TrimSuffix(l, "\r")
		fields := strings.Fields(l)
		if len(fields) > 0 && fields[0] == key && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			if replaced {
				continue // a duplicate of a key we've already rewritten; drop it
			}
			out = append(out, line)
			replaced = true
			continue
		}
		out = append(out, l)
	}
	if !replaced {
		out = append(out, line)
	}
	// A file that was empty splits to one empty line; dropping it keeps a
	// brand-new config from starting with a blank line.
	for len(out) > 0 && strings.TrimSpace(out[0]) == "" {
		out = out[1:]
	}
	body := strings.Join(out, eol)
	if body != "" {
		body += eol
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	// Written via a temporary file in the same directory and renamed, so
	// an interrupted write can't leave a half-truncated config behind.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".termdock.conf-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// runBind handles ":bind <key> <action>", the one config-file setting
// that isn't a single value (it's one line per key, and repeatable), so
// it gets its own command rather than a place in the settings list.
func (c *Core) runBind(args []string) {
	if len(args) != 2 {
		c.statusMsg = "usage: bind <key> <action> — e.g. bind M jump-picker"
		return
	}
	keys := []rune(args[0])
	if args[0] == "Space" {
		keys = []rune{' '}
	}
	if len(keys) != 1 {
		c.statusMsg = fmt.Sprintf("bind: %q is not a single key (or \"Space\")", args[0])
		return
	}
	act := action(args[1])
	if !validActions[act] {
		c.statusMsg = fmt.Sprintf("bind: no action called %q — see Ctrl-B ? for the list", args[1])
		return
	}
	c.bindings[keys[0]] = act
	if c.bindOverridden == nil {
		c.bindOverridden = map[rune]bool{}
	}
	c.bindOverridden[keys[0]] = true
	c.statusMsg = fmt.Sprintf("bound %s to %s", keyLabel(keys[0]), act)
}

// settingsState backs the settings screen: a plain scrollable list of
// every key and its current value, built fresh each time it's opened.
type settingsState struct {
	sel int
}

func (c *Core) enterSettings() {
	c.mode = ModeSettings
	c.settings = settingsState{}
}

func (c *Core) handleSettingsKey(key tcell.Key, r rune) {
	n := len(config.Settings())
	switch {
	case key == tcell.KeyEsc || r == 'q':
		c.mode = ModeNormal
		c.settings = settingsState{}
	case key == tcell.KeyUp || r == 'k':
		c.scrollSettings(-1)
	case key == tcell.KeyDown || r == 'j':
		c.scrollSettings(1)
	case key == tcell.KeyHome:
		c.settings.sel = 0
	case key == tcell.KeyEnd:
		c.settings.sel = maxi(0, n-1)
	case key == tcell.KeyEnter || r == ' ':
		c.editSelectedSetting(false)
	case r == 'S':
		c.editSelectedSetting(true)
	}
}

func (c *Core) scrollSettings(delta int) {
	c.settings.sel = clampi(c.settings.sel+delta, 0, maxi(0, len(config.Settings())-1))
}

// editSelectedSetting closes the list and opens the command prompt
// already filled in with "set <key> <current value>", so the value can be
// edited in place rather than retyped from memory — the list is what
// tells you the key exists, the prompt is what changes it.
func (c *Core) editSelectedSetting(persist bool) {
	all := config.Settings()
	if c.settings.sel >= len(all) {
		return
	}
	s := all[c.settings.sel]
	c.settings = settingsState{}
	prefix := "set "
	if persist {
		prefix = "set -p "
	}
	current := config.Get(&c.cfg, s.Key)
	if strings.HasPrefix(current, "(") {
		current = "" // a description of "unset", not a value to re-submit
	}
	c.startInput("cmd", ":", prefix+s.Key+" "+current, ModeNormal)
}

func (c *Core) settingsOverlay() *proto.Overlay {
	if c.mode != ModeSettings {
		return nil
	}
	all := config.Settings()
	keyW, valW := 0, 0
	for _, s := range all {
		if l := len(s.Key); l > keyW {
			keyW = l
		}
		if l := len([]rune(config.Get(&c.cfg, s.Key))); l > valW {
			valW = l
		}
	}
	items := make([]string, len(all))
	for i, s := range all {
		note := ""
		if s.NewPanesOnly {
			note = " [new panes]"
		}
		items[i] = fmt.Sprintf("%-*s  %-*s  %s%s", keyW, s.Key, valW, config.Get(&c.cfg, s.Key), s.Doc, note)
	}
	return &proto.Overlay{
		Title:      "settings — ↑↓ move, enter edit, S edit+save to the config file, esc close",
		Selectable: true,
		Items:      items,
		Selected:   c.settings.sel,
	}
}
