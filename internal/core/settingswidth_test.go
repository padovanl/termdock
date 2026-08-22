package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/config"
)

// settingsOverlayWidth is what the client will size the box to: the
// longer of the title and the widest item (see drawOverlay).
func settingsOverlayWidth(c *Core) int {
	ov := c.settingsOverlay()
	w := len([]rune(ov.Title))
	for _, it := range ov.Items {
		if l := len([]rune(it)); l > w {
			w = l
		}
	}
	return w
}

// TestSettingsOverlayWidthNeverChanges pins the settings screen to one
// width. The overlay is centred and sized to its longest line, so
// anything that grows with the selection makes the whole box jump
// sideways as you move — which it did on "theme" three separate ways:
// the position indicator appearing on the selected row, the value
// column having been sized to the current values only (so stepping from
// "nord" to "tokyo-night" widened it), and the title itself changing
// between two lengths.
func TestSettingsOverlayWidthNeverChanges(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enterSettings()

	all := config.Settings()
	want := settingsOverlayWidth(c)

	for i, s := range all {
		c.settings.sel = i
		if got := settingsOverlayWidth(c); got != want {
			t.Errorf("selecting %q: width %d, want a constant %d", s.Key, got, want)
		}

		// Every value the arrows can step this setting to.
		for range s.Choices() {
			c.stepSelectedSetting(1)
			if got := settingsOverlayWidth(c); got != want {
				t.Errorf("%s = %q: width %d, want a constant %d",
					s.Key, config.Get(&c.cfg, s.Key), got, want)
			}
		}

		// ...and while its value is being typed.
		c.settings.editing = true
		c.settings.buffer = []rune(config.Get(&c.cfg, s.Key))
		if got := settingsOverlayWidth(c); got != want {
			t.Errorf("editing %q: width %d, want a constant %d", s.Key, got, want)
		}
		c.settings.editing = false
	}
}

// A setting with a known set of values is never typed into. Offering a
// free-text field for "mouse" invites "yes", "1" and "ON", all of which
// the parser silently rejects — and it let popup-command be set to a
// single letter, after which the popup opened, the command exited, and
// the feature looked broken.
func TestSettingsWithFixedValuesCannotBeTypedInto(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enterSettings()

	for i, s := range config.Settings() {
		if len(s.Choices()) == 0 {
			continue
		}
		c.settings.sel = i
		before := config.Get(&c.cfg, s.Key)
		c.startEditingSetting()

		if c.settings.editing {
			t.Errorf("%q has a fixed set of values but opened a text field", s.Key)
			c.settings.editing = false
			continue
		}
		// Enter did what the arrows do: moved to another valid value.
		if after := config.Get(&c.cfg, s.Key); after == before && len(s.Choices()) > 1 {
			t.Errorf("%q: enter left it at %q instead of stepping to the next value", s.Key, after)
		}
	}
}

// Typing a long value must not widen the box under the cursor.
func TestTypingALongValueDoesNotWidenTheBox(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enterSettings()
	want := settingsOverlayWidth(c)

	for i, s := range config.Settings() {
		if len(s.Choices()) > 0 {
			continue // those cannot be typed into at all
		}
		c.settings.sel = i
		c.startEditingSetting()
		buf := make([]rune, 200)
		for j := range buf {
			buf[j] = 'x'
		}
		c.settings.buffer = buf
		if got := settingsOverlayWidth(c); got != want {
			t.Fatalf("typing into %q widened the box to %d, want a constant %d", s.Key, got, want)
		}
		c.settings.editing = false
	}
}

// The tail is what must stay visible: that is where the cursor is.
func TestEditWindowKeepsTheCursorEndVisible(t *testing.T) {
	got := editWindow("/a/very/long/path/to/some/shell", 12)
	if len([]rune(got)) > 12 {
		t.Fatalf("%q is %d wide, want at most 12", got, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "shell_") {
		t.Errorf("%q should end with the text being typed and the cursor", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Errorf("%q should show it has been scrolled", got)
	}
}
