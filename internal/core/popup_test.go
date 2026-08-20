package core

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// waitForPopupText polls the popup pane's own buffer for text, the
// popup's equivalent of writeAndWaitEcho (which looks panes up in
// c.panes — the popup is deliberately not tracked there; see popup.go).
func waitForPopupText(t *testing.T, c *Core, text string) {
	t.Helper()
	ok := waitFor(t, func() bool {
		c.mu.Lock()
		p := c.popup
		c.mu.Unlock()
		if p == nil {
			return false
		}
		term := p.Term()
		term.Lock()
		defer term.Unlock()
		cols, rows := term.Size()
		hl := term.HistoryLen()
		for y := 0; y < hl+rows; y++ {
			var sb strings.Builder
			for x := 0; x < cols; x++ {
				ch := cellAt(term, hl, y, x).Char
				if ch == 0 {
					ch = ' '
				}
				sb.WriteRune(ch)
			}
			if strings.Contains(sb.String(), text) {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("text %q never showed up in the popup's buffer", text)
	}
}

func TestPopupTogglesAndPersistsAcrossHides(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	mode1, visible1 := c.mode, c.popupVisible
	p1 := c.popup
	c.mu.Unlock()

	if mode1 != ModePopup || !visible1 || p1 == nil {
		t.Fatalf("first toggle should open the popup: mode=%v visible=%v popup=%v", mode1, visible1, p1)
	}

	c.mu.Lock()
	c.togglePopup()
	mode2, visible2 := c.mode, c.popupVisible
	c.mu.Unlock()

	if mode2 != ModeNormal || visible2 {
		t.Fatalf("second toggle should hide the popup (not close it): mode=%v visible=%v", mode2, visible2)
	}

	c.mu.Lock()
	c.togglePopup()
	p3 := c.popup
	c.mu.Unlock()

	if p3 != p1 {
		t.Fatal("re-showing the popup should reuse the same pane/process, not create a new one")
	}
}

func TestPopupKeysForwardToItsOwnPane(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	c.mu.Unlock()

	// A window's active pane must NOT receive this — it should go
	// straight into the popup instead.
	activeBefore := c.win().active.ID

	c.mu.Lock()
	c.handlePopupKey(tcell.KeyRune, 'x')
	c.popup.Write([]byte("echo unique-popup-marker\r"))
	c.mu.Unlock()

	waitForPopupText(t, c, "unique-popup-marker")

	c.mu.Lock()
	activeAfter := c.win().active.ID
	c.mu.Unlock()
	if activeAfter != activeBefore {
		t.Fatal("typing into the popup should not change window focus")
	}
}

func TestPopupPrefixDetachAndQuitStillWork(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	c.handlePopupKey(tcell.Key(c.prefixKey), 0)
	res := c.handlePopupKey(tcell.KeyRune, 'd')
	c.mu.Unlock()

	if !res.Detach {
		t.Fatal("Ctrl-B d while the popup is focused should still request a detach")
	}
}

// TestPopupQuitAsksForConfirmationTooNow guards against the popup's own
// narrow key handler becoming a confirmation-free back door for 'q' now
// that the normal dispatch path (dispatchAction's actQuit) asks first —
// see confirmQuit.
func TestPopupQuitAsksForConfirmationTooNow(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	c.handlePopupKey(tcell.Key(c.prefixKey), 0)
	c.handlePopupKey(tcell.KeyRune, 'q')
	mode := c.mode
	closed := c.closed
	c.mu.Unlock()

	if closed {
		t.Fatal("Ctrl-B q from the popup should ask for confirmation, not quit immediately")
	}
	if mode != ModeConfirm {
		t.Fatalf("mode after Ctrl-B q from the popup = %v, want ModeConfirm", mode)
	}
}

func TestPopupClickOutsideCloses(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	r := c.popupRect()
	c.handlePopupMouse(true, false, r.X-1, r.Y-1) // just outside the top-left corner
	visible := c.popupVisible
	mode := c.mode
	c.mu.Unlock()

	if visible || mode != ModeNormal {
		t.Fatalf("clicking outside the popup should close it: visible=%v mode=%v", visible, mode)
	}
}

func TestPopupClickInsideStaysOpen(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	r := c.popupRect()
	c.handlePopupMouse(true, false, r.X+1, r.Y+1)
	visible := c.popupVisible
	c.mu.Unlock()

	if !visible {
		t.Fatal("clicking inside the popup must not close it")
	}
}

func TestPopupExitClosesItAndReturnsToNormal(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.togglePopup()
	popupID := c.popup.ID
	c.popup.Write([]byte("exit\r"))
	c.mu.Unlock()

	ok := waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.popup == nil
	})
	if !ok {
		t.Fatal("the popup's shell exiting should clear c.popup")
	}
	c.mu.Lock()
	visible, mode := c.popupVisible, c.mode
	c.mu.Unlock()
	if visible || mode != ModeNormal {
		t.Fatalf("popup pane %d exiting should also hide it and return to ModeNormal: visible=%v mode=%v", popupID, visible, mode)
	}
}

// TestPopupUsesConfiguredCommand: with popup-command set (see
// SetPopupCommand/config.go), the popup should run that instead of an
// interactive shell — checked here the same way TestPopupKeysForwardToItsOwnPane
// confirms the popup is a real, live pane: write something recognizable
// and wait for it to show up in the popup's own buffer.
func TestPopupUsesConfiguredCommand(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.popupCommand = "echo popup-command-marker; sleep 2"
	c.togglePopup()
	c.mu.Unlock()

	waitForPopupText(t, c, "popup-command-marker")
}

// TestPopupCommandExitingClosesItAutomatically: unlike the persistent
// scratch-shell default, a one-shot popup-command process finishing
// should close the popup by itself — the same onExit path an
// interactive shell's own "exit" already takes (see
// TestPopupExitClosesItAndReturnsToNormal above), just triggered by the
// configured command returning instead of a typed "exit".
func TestPopupCommandExitingClosesItAutomatically(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.popupCommand = "true" // exits immediately, zero output
	c.togglePopup()
	c.mu.Unlock()

	ok := waitFor(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.popup == nil
	})
	if !ok {
		t.Fatal("the popup-command process exiting should clear c.popup")
	}
	c.mu.Lock()
	visible, mode := c.popupVisible, c.mode
	c.mu.Unlock()
	if visible || mode != ModeNormal {
		t.Fatalf("popup-command exiting should also hide the popup and return to ModeNormal: visible=%v mode=%v", visible, mode)
	}
}

// TestPopupKeysFollowRebinding: the popup's own narrow prefix handler
// used to compare against the literal 'P'/'d'/'q' instead of consulting
// c.bindings, so a config "bind" line reassigning one of those keys
// changed what it did everywhere except here. The sharp case is 'q':
// binding it to something harmless left the popup still quitting the
// whole session on it, a confirmation the user thought they'd moved away.
func TestPopupKeysFollowRebinding(t *testing.T) {
	c := newTestCore(t)
	// 'q' now means detach, and quit has moved to 'K'.
	c.SetBindOverrides(map[rune]string{'q': "detach", 'K': "quit"})

	c.mu.Lock()
	c.togglePopup()

	c.handlePopupKey(tcell.Key(c.prefixKey), 0)
	reassigned := c.handlePopupKey(tcell.KeyRune, 'q')
	modeAfterQ := c.mode

	c.handlePopupKey(tcell.Key(c.prefixKey), 0)
	c.handlePopupKey(tcell.KeyRune, 'K')
	modeAfterK := c.mode
	c.mu.Unlock()

	if !reassigned.Detach {
		t.Error("'q' was rebound to detach, so it should detach from the popup too")
	}
	if modeAfterQ == ModeConfirm {
		t.Error("'q' no longer means quit, so it must not raise the quit confirmation from the popup")
	}
	if modeAfterK != ModeConfirm {
		t.Errorf("the rebound quit key should reach confirmQuit from the popup, mode=%v", modeAfterK)
	}
}
