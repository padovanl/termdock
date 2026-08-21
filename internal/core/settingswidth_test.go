package core

import (
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
