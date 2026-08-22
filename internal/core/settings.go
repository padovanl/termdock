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
	statusIcons = cfg.StatusIcons
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

// settingsState backs the settings screen: a scrollable list of every
// key and its current value, and — when you're changing one — the text
// being typed into that row.
//
// Editing happens in the row itself rather than by handing off to the
// command prompt at the bottom of the screen. The list is where you can
// see what a setting is currently set to, so it's where changing it
// belongs; bouncing down to a ":" line meant reading the value in one
// place and retyping it in another.
type settingsState struct {
	sel     int
	editing bool
	buffer  []rune
}

func (c *Core) enterSettings() {
	c.mode = ModeSettings
	c.settings = settingsState{}
}

func (c *Core) handleSettingsKey(key tcell.Key, r rune) {
	if c.settings.editing {
		c.handleSettingsEditKey(key, r)
		return
	}
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
	case key == tcell.KeyLeft || r == 'h':
		c.stepSelectedSetting(-1)
	case key == tcell.KeyRight || r == 'l':
		c.stepSelectedSetting(1)
	case key == tcell.KeyEnter || r == ' ':
		c.startEditingSetting()
	case r == 'S':
		c.saveSelectedSetting()
	}
}

// stepSelectedSetting is what ←/→ do: move a setting with a known set of
// values to the next one and apply it at once. Arrowing along "theme"
// repaints the session on every press, which is the only way to choose a
// palette that doesn't involve knowing all eleven names by heart.
func (c *Core) stepSelectedSetting(delta int) {
	s, ok := c.selectedSetting()
	if !ok {
		return
	}
	if len(s.Choices()) == 0 {
		c.statusMsg = fmt.Sprintf("%s has no fixed set of values — press enter to type one", s.Key)
		return
	}
	updated := c.cfg
	value, ok := config.Step(&updated, s.Key, delta)
	if !ok {
		return
	}
	c.applySettingChange(s, updated, value)
}

// startEditingSetting turns the selected row into a text field holding
// its current value.
func (c *Core) startEditingSetting() {
	s, ok := c.selectedSetting()
	if !ok {
		return
	}
	// A setting with a known set of values is never typed. Offering a
	// free-text field for "mouse" invites "yes", "1", "ON" and anything
	// else, all of which the parser has to reject silently — and it let
	// popup-command be set to a single letter, after which the popup
	// opened and vanished and looked broken. Enter steps to the next
	// value instead, which is the same thing the arrows do and the only
	// thing that can be meant here.
	if len(s.Choices()) > 0 {
		c.stepSelectedSetting(1)
		return
	}
	current := config.Get(&c.cfg, s.Key)
	if strings.HasPrefix(current, "(") {
		current = "" // a description of "unset", not a value to edit
	}
	c.settings.editing = true
	c.settings.buffer = []rune(current)
	c.statusMsg = ""
}

// handleSettingsEditKey drives the row being typed into. Deliberately the
// same small vocabulary as every other text entry in termdock (see
// input.go): enter commits, esc abandons, Ctrl-U clears.
func (c *Core) handleSettingsEditKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc:
		c.settings.editing = false
		c.settings.buffer = nil
	case key == tcell.KeyEnter:
		c.commitEditedSetting()
	case key == tcell.KeyBackspace || key == tcell.KeyBackspace2:
		if n := len(c.settings.buffer); n > 0 {
			c.settings.buffer = c.settings.buffer[:n-1]
		}
	case key == tcell.KeyCtrlU:
		c.settings.buffer = c.settings.buffer[:0]
	case r != 0 && key == tcell.KeyRune:
		c.settings.buffer = append(c.settings.buffer, r)
	}
}

func (c *Core) commitEditedSetting() {
	s, ok := c.selectedSetting()
	if !ok {
		return
	}
	value := strings.TrimSpace(string(c.settings.buffer))
	c.settings.editing = false
	c.settings.buffer = nil

	updated := c.cfg
	if err := config.Set(&updated, s.Key, value); err != nil {
		c.statusMsg = s.Key + ": " + err.Error()
		return
	}
	if err := config.CheckSetting(&updated, s.Key); err != nil {
		c.statusMsg = s.Key + ": " + err.Error()
		return
	}
	c.applySettingChange(s, updated, config.Get(&updated, s.Key))
}

// saveSelectedSetting writes whatever the selected setting is currently
// on to the config file, rewriting only its line. Separate from changing
// it on purpose: arrowing through themes shouldn't rewrite a file eleven
// times on the way past.
func (c *Core) saveSelectedSetting() {
	s, ok := c.selectedSetting()
	if !ok {
		return
	}
	value := config.Get(&c.cfg, s.Key)
	if strings.HasPrefix(value, "(") {
		c.statusMsg = fmt.Sprintf("%s isn't set to anything to save", s.Key)
		return
	}
	if err := c.persistSetting(s.Key, value); err != nil {
		c.statusMsg = fmt.Sprintf("could not save %s: %v", s.Key, err)
		return
	}
	c.statusMsg = fmt.Sprintf("%s = %s — saved to %s", s.Key, value, config.Path())
}

// applySettingChange is the one place a change made on this screen takes
// effect, so stepping with the arrows and typing a value behave
// identically — including starting to push colours to every attached
// client once a look-and-feel setting has been changed here.
func (c *Core) applySettingChange(s config.Setting, updated config.Config, value string) {
	if s.Scope == config.ScopeClient {
		c.clientCfgOverridden = true
	}
	c.applyConfigLocked(updated)
	msg := fmt.Sprintf("%s = %s", s.Key, value)
	if s.NewPanesOnly {
		msg += " (applies to new panes)"
	}
	c.statusMsg = msg
}

func (c *Core) selectedSetting() (config.Setting, bool) {
	all := config.Settings()
	if c.settings.sel < 0 || c.settings.sel >= len(all) {
		return config.Setting{}, false
	}
	return all[c.settings.sel], true
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
	// Every column is sized for the widest thing that could ever land in
	// it, not for what happens to be on screen right now, and each row is
	// padded to that. The overlay sizes itself to its longest line, so a
	// row that grows when you select it takes the whole box with it —
	// which is exactly what "theme" did: arrowing onto it added the
	// position indicator, and stepping from "nord" to "tokyo-night" grew
	// the value column by seven more. The box jumping sideways as the
	// selection moves is far more distracting than a little unused width.
	keyW, valW, noteW := 0, 0, 0
	for _, s := range all {
		if l := len([]rune(s.Key)); l > keyW {
			keyW = l
		}
		// Both what it is now and anything the arrows could step it to.
		values := append([]string{config.Get(&c.cfg, s.Key)}, s.Choices()...)
		for _, v := range values {
			if l := len([]rune(v)); l > valW {
				valW = l
			}
		}
		if l := len([]rune(worstCaseNote(s))); l > noteW {
			noteW = l
		}
	}
	valW++ // room for the cursor the row being edited grows

	items := make([]string, len(all))
	for i, s := range all {
		value := config.Get(&c.cfg, s.Key)
		note := settingDoc(s)
		switch {
		case i == c.settings.sel && c.settings.editing:
			// Scrolled to the cursor rather than shown whole: a long path
			// typed into "shell" grew the row, and the overlay is sized to
			// its widest row, so the whole box widened under the cursor as
			// you typed. Keeping the end in view is what matters while
			// typing — that is where the cursor is.
			value = editWindow(string(c.settings.buffer), valW)
			note = editingHelp(s)
		case i == c.settings.sel && len(s.Choices()) > 0:
			// Only shown on the row it applies to: most settings are typed,
			// and a permanent "←→" against every one of them would be an
			// invitation that mostly does nothing. A position rather than
			// the list itself — spelling out twelve theme names would make
			// this row several times the width of any other. You learn the
			// names by arrowing through them, or from "termdock themes".
			note += choiceIndicator(s, value)
		}
		items[i] = fmt.Sprintf("%-*s  %-*s  %-*s", keyW, s.Key, valW, value, noteW, note)
	}
	return &proto.Overlay{
		Title:      c.settingsTitle(),
		Selectable: true,
		Items:      items,
		Selected:   c.settings.sel,
	}
}

// editingNote replaces a row's description while its value is being
// typed, for the settings that have nothing more specific to say.
const editingNote = "enter to apply, esc to cancel"

// editingHelp is what a row says while you type into it: the shape of a
// value it will accept, if the setting knows one.
//
// The keys to press are deliberately not repeated here — the status bar
// already spells those out for as long as the edit is open (see
// settingsHint). What was missing is the other half: a free-text row
// gave no clue what it wanted, which is how "popup-command" ended up
// looking like a field you could write a sentence into.
func editingHelp(s config.Setting) string {
	if s.Hint != "" {
		return s.Hint
	}
	return editingNote
}

// settingDoc is a row's ordinary description.
func settingDoc(s config.Setting) string {
	if s.NewPanesOnly {
		return s.Doc + " [new panes]"
	}
	return s.Doc
}

// choiceIndicator is the "3/11" position shown against the selected row
// of a setting with a fixed set of values.
func choiceIndicator(s config.Setting, value string) string {
	return fmt.Sprintf("  ←→ %d/%d", choiceIndex(s, value)+1, len(s.Choices()))
}

// worstCaseNote is the longest note a row can ever show, so the column
// can be sized once instead of following the selection around.
func worstCaseNote(s config.Setting) string {
	longest := settingDoc(s)
	if ch := s.Choices(); len(ch) > 0 {
		// The widest position is the *last* one ("11/11", not "1/11"),
		// so measure against that rather than the first.
		if withPos := longest + choiceIndicator(s, ch[len(ch)-1]); len([]rune(withPos)) > len([]rune(longest)) {
			longest = withPos
		}
	}
	// The hint is shown in this same column while the row is being
	// edited, so it has to be measured here too — otherwise the box
	// widens the moment you press enter on a setting whose hint is longer
	// than its description.
	if h := editingHelp(s); len([]rune(h)) > len([]rune(longest)) {
		longest = h
	}
	return longest
}

// editWindow renders the value being typed inside a fixed width: the
// tail of it, with a leading ellipsis when there is more to the left,
// and the cursor at the end.
func editWindow(text string, width int) string {
	cursor := "_"
	room := width - len([]rune(cursor))
	if room < 1 {
		return cursor
	}
	r := []rune(text)
	if len(r) <= room {
		return text + cursor
	}
	return "…" + string(r[len(r)-room+1:]) + cursor
}

// choiceIndex is where value sits in a setting's list, or 0 when it
// isn't in there at all.
func choiceIndex(s config.Setting, value string) int {
	for i, v := range s.Choices() {
		if v == value {
			return i
		}
	}
	return 0
}

// settingsHint is the status bar line while the settings screen is up.
// It tracks the row you're on, same as the overlay's own title: what the
// arrows do depends on whether the selected setting has a list of values.
func (c *Core) settingsHint() string {
	if c.settings.editing {
		return "typing a value — enter applies it, esc cancels"
	}
	if s, ok := c.selectedSetting(); ok && len(s.Choices()) > 0 {
		return "↑↓ move, ←→ choose a value, enter to type one, S save to file, esc close"
	}
	return "↑↓ move, enter to type a value, S save to file, esc close"
}

// settingsTitle is deliberately constant. It varied with the selected
// row, and the overlay is sized to the longer of its title and its
// widest item — so the title alone made the box change width as you
// moved. What the arrows do on *this* row is in the status bar
// instead (see settingsHint), which has no such effect.
func (c *Core) settingsTitle() string {
	return "settings — ↑↓ move, ←→ choose, enter type, S save to file, esc close"
}
