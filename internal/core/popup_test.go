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
