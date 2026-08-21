package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
)

// setCmd runs one command-prompt line the way a user typing it would,
// and returns whatever the status bar was left saying.
func setCmd(c *Core, line string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runCommand(line)
	return c.statusMsg
}

func TestSetChangesASettingLive(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	msg := setCmd(c, "set focus-events on")
	if !strings.Contains(msg, "focus-events = on") {
		t.Errorf("status = %q, want it to confirm the new value", msg)
	}
	c.mu.Lock()
	on := c.focusEvents
	c.mu.Unlock()
	if !on {
		t.Error("the setting should have reached the live session, not just the stored config")
	}

	setCmd(c, "set focus-events off")
	c.mu.Lock()
	off := c.focusEvents
	c.mu.Unlock()
	if off {
		t.Error("setting it back off should also take effect")
	}
}

func TestSetRejectsBadValuesWithAReason(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	for _, tc := range []struct{ line, want string }{
		{"set repeat-time abc", "milliseconds"},
		{"set theme nonsuch", "no bundled theme"},
		{"set status-segments bogus", "no status segment"},
		{"set status-bg notacolor", "color name or #rrggbb"},
		{"set nonsuch-key 1", "no setting called"},
		{"set prefix nonsense", "Ctrl+letter"},
	} {
		msg := setCmd(c, tc.line)
		if !strings.Contains(msg, tc.want) {
			t.Errorf("%q reported %q, want it to mention %q", tc.line, msg, tc.want)
		}
	}

	// A refused value must leave the session exactly as it was.
	c.mu.Lock()
	rt := c.repeatTime
	c.mu.Unlock()
	if want := time.Duration(config.Default().RepeatTime) * time.Millisecond; rt != want {
		t.Errorf("repeat-time = %v after a refused change, want the default %v untouched", rt, want)
	}
}

// TestSetThemeStartsPushingColorsToClients: colours are normally each
// client's own business (per-attach themes are a documented feature), but
// a change made *in the session* has to reach everyone attached — so the
// frame starts carrying them from that point on, and not before.
func TestSetThemeStartsPushingColorsToClients(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	if f := c.Frame(); f.Settings != nil {
		t.Fatal("an untouched session should send no settings, leaving each client its own config file")
	}

	setCmd(c, "set theme dracula")

	f := c.Frame()
	if f.Settings == nil {
		t.Fatal("after a theme change the session should send its colors to every client")
	}
	if f.Settings.PaneActiveBG == uint64(config.Default().PaneActiveBG) {
		t.Error("the pushed accent color is still the default — the theme didn't reach the frame")
	}
}

// TestSetServerSideSettingDoesNotPushColors: changing something that
// isn't about how the terminal looks must not take per-client theming
// away from everyone attached.
func TestSetServerSideSettingDoesNotPushColors(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	setCmd(c, "set repeat-time 250")

	if f := c.Frame(); f.Settings != nil {
		t.Error("a server-side setting shouldn't make the session start dictating colors")
	}
}

func TestSetWithNoValueReportsTheCurrentOne(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	if msg := setCmd(c, "set history-limit"); !strings.Contains(msg, "history-limit = 10000") {
		t.Errorf("status = %q, want the current value", msg)
	}
	if msg := setCmd(c, "set nonsuch"); !strings.Contains(msg, "no setting called") {
		t.Errorf("status = %q, want it to say the key is unknown", msg)
	}
}

func TestSetNewPanesOnlySaysSo(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	msg := setCmd(c, "set history-limit 500")
	if !strings.Contains(msg, "new panes") {
		t.Errorf("status = %q — a setting read only when a pane is created should say so, or it looks like it didn't work", msg)
	}
}

// TestPersistRewritesOnlyThatLine: the config file is someone's own
// document — comments, ordering, keys a future build might add. Saving a
// setting has to edit its line and leave the rest alone.
func TestPersistRewritesOnlyThatLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	original := "# my config\nmouse on        # keep this comment\ntheme nord\nfuture-setting 42\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)

	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	msg := setCmd(c, "set -p theme gruvbox")
	if !strings.Contains(msg, "saved to") {
		t.Fatalf("status = %q, want it to confirm the save", msg)
	}

	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(saved)
	for _, keep := range []string{"# my config", "mouse on        # keep this comment", "future-setting 42"} {
		if !strings.Contains(got, keep) {
			t.Errorf("saving dropped %q:\n%s", keep, got)
		}
	}
	if !strings.Contains(got, "theme gruvbox") {
		t.Errorf("the theme line wasn't updated:\n%s", got)
	}
	if strings.Contains(got, "theme nord") {
		t.Errorf("the old theme line is still there:\n%s", got)
	}
}

func TestPersistAppendsWhenTheKeyIsAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	os.WriteFile(path, []byte("# just a comment\n"), 0644)
	t.Setenv("TERMDOCK_CONFIG", path)

	c := newTestCore(t)
	c.ApplyConfig(config.Default())
	setCmd(c, "set -p repeat-time 250")

	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "repeat-time 250") {
		t.Errorf("a key not already in the file should be appended:\n%s", saved)
	}
	if !strings.Contains(string(saved), "# just a comment") {
		t.Errorf("appending shouldn't disturb what was there:\n%s", saved)
	}
}

// TestSetWithoutPersistLeavesTheFileAlone: trying something out must not
// rewrite the user's file behind their back.
func TestSetWithoutPersistLeavesTheFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	original := "theme nord\n"
	os.WriteFile(path, []byte(original), 0644)
	t.Setenv("TERMDOCK_CONFIG", path)

	c := newTestCore(t)
	c.ApplyConfig(config.Default())
	setCmd(c, "set theme gruvbox")

	saved, _ := os.ReadFile(path)
	if string(saved) != original {
		t.Errorf("the config file changed without -p:\n%s", saved)
	}
}

func TestSettingsScreenListsEveryKeyWithItsValue(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	c.mu.Lock()
	c.enterSettings()
	ov := c.settingsOverlay()
	c.mu.Unlock()

	if ov == nil {
		t.Fatal("the settings screen should produce an overlay")
	}
	if len(ov.Items) != len(config.Settings()) {
		t.Fatalf("listed %d settings, want %d", len(ov.Items), len(config.Settings()))
	}
	joined := strings.Join(ov.Items, "\n")
	for _, key := range config.Keys() {
		if !strings.Contains(joined, key) {
			t.Errorf("the list is missing %q", key)
		}
	}
	if !strings.Contains(joined, "10000") {
		t.Error("the list should show each setting's current value, not just its name")
	}
}

// TestSettingsScreenEnterPrefillsThePrompt: the list is how you find a
// key; the prompt is how you change it. Enter has to carry the key and
// its current value across, or you're retyping both from memory.
func TestSettingsScreenEnterPrefillsThePrompt(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	c.mu.Lock()
	c.enterSettings()
	for c.settings.sel < len(config.Settings()) && config.Settings()[c.settings.sel].Key != "history-limit" {
		c.settings.sel++
	}
	c.handleSettingsKey(tcell.KeyEnter, 0)
	mode := c.mode
	buffer := string(c.input.buffer)
	c.mu.Unlock()

	if mode != ModeInput {
		t.Fatalf("Enter should open the command prompt, mode=%v", mode)
	}
	if buffer != "set history-limit 10000" {
		t.Errorf("prompt prefilled with %q, want %q", buffer, "set history-limit 10000")
	}
}

func TestSettingsScreenEscCloses(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enterSettings()
	c.handleSettingsKey(tcell.KeyEsc, 0)
	if c.mode != ModeNormal {
		t.Fatalf("Esc should close the settings screen, mode=%v", c.mode)
	}
	if c.settingsOverlay() != nil {
		t.Error("no overlay once it's closed")
	}
}

func TestBindCommandRebindsLive(t *testing.T) {
	c := newTestCore(t)
	c.ApplyConfig(config.Default())

	if msg := setCmd(c, "bind M jump-picker"); !strings.Contains(msg, "jump-picker") {
		t.Errorf("status = %q, want confirmation", msg)
	}
	c.mu.Lock()
	act := c.bindings['M']
	c.mu.Unlock()
	if act != actJumpPicker {
		t.Errorf("M is bound to %q, want jump-picker", act)
	}

	if msg := setCmd(c, "bind M nonsense"); !strings.Contains(msg, "no action called") {
		t.Errorf("status = %q, want it to reject an unknown action", msg)
	}
}
