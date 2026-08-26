package core

import (
	"strings"
	"testing"
	"time"

	"github.com/padovanl/termdock/internal/config"
)

// Saving a setting to the config file said so in c.statusMsg, and the
// settings screen never showed it: every modal screen puts its own
// instructions in the status bar for as long as it is open, and those
// won. From the one screen where knowing the file was written is the
// point, nothing appeared to happen.
func TestSavingASettingSaysSoOverTheScreensOwnHint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TERMDOCK_CONFIG", dir+"/termdock.conf")

	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterSettings()
	if c.mode != ModeSettings {
		t.Fatal("the settings screen did not open")
	}

	// Before saving, the screen's own instructions have the bar.
	hint, _ := c.statusHint()
	if !strings.Contains(hint, "save to file") {
		t.Fatalf("with nothing flashed the bar reads %q, expected the screen's instructions", hint)
	}

	// Put the cursor on a setting that has a value to save.
	for i, s := range config.Settings() {
		if s.Key == "history-limit" {
			c.settings.sel = i
		}
	}
	c.saveSelectedSetting()

	hint, _ = c.statusHint()
	if strings.Contains(hint, "save to file") {
		t.Errorf("after saving, the bar still reads the screen's instructions: %q", hint)
	}
	if !strings.Contains(hint, "history-limit") || !strings.Contains(hint, "saved") {
		t.Errorf("after saving, the bar reads %q; it should say what was written and where", hint)
	}

	// And it gets out of the way again, leaving the instructions you need
	// for as long as the screen is open.
	c.flashUntil = time.Now().Add(-time.Second)
	hint, _ = c.statusHint()
	if !strings.Contains(hint, "save to file") {
		t.Errorf("once the flash expired the bar reads %q, expected the screen's instructions back", hint)
	}
}

// A failure has to be as visible as a success, for the same reason.
func TestAFailedSaveIsAlsoShown(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterSettings()
	// "shell" reads as "(your $SHELL)" when unset, which is not a value
	// that can be written to a file.
	for i, s := range config.Settings() {
		if s.Key == "shell" {
			c.settings.sel = i
		}
	}
	c.saveSelectedSetting()

	hint, _ := c.statusHint()
	if !strings.Contains(hint, "shell") {
		t.Errorf("the refusal was not shown: bar reads %q", hint)
	}
}

// The flash is a general mechanism, so pin its edges rather than only
// the one caller.
func TestFlashExpires(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	if got := c.activeFlash(); got != "" {
		t.Errorf("a fresh session has a flash: %q", got)
	}
	c.flash("wrote %d lines", 3)
	if got := c.activeFlash(); got != "wrote 3 lines" {
		t.Errorf("activeFlash = %q, want the message just flashed", got)
	}
	if c.statusMsg != "wrote 3 lines" {
		t.Errorf("statusMsg = %q; a flash should also leave the ordinary message behind", c.statusMsg)
	}
	c.flashUntil = time.Now().Add(-time.Millisecond)
	if got := c.activeFlash(); got != "" {
		t.Errorf("an expired flash still reports %q", got)
	}
}

// A value the settings screen refuses is the case that matters most:
// the screen stays open afterwards, so without a flash its own
// instructions cover the reason and the value looks accepted.
func TestARefusedValueSaysWhy(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterSettings()
	for i, s := range config.Settings() {
		if s.Key == "history-limit" {
			c.settings.sel = i
		}
	}
	c.settings.editing = true
	c.settings.buffer = []rune("not a number")
	c.commitEditedSetting()

	hint, _ := c.statusHint()
	if strings.Contains(hint, "save to file") {
		t.Fatalf("after a refusal the bar still reads the screen's instructions: %q", hint)
	}
	if !strings.Contains(hint, "history-limit") {
		t.Errorf("the refusal does not name the setting: %q", hint)
	}
}
