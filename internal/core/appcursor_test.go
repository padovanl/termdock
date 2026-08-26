package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// An arrow key has two encodings, and which one is right depends on the
// program: CSI ("\033[A") normally, SS3 ("\033OA") once the program sets
// DECCKM, which is what ncurses does when it calls keypad(). Sending the
// wrong one is not a near miss, the program does not see an arrow key at
// all. termdock tracked the mode but never consulted it, so arrows did
// nothing in htop.
func TestArrowsFollowApplicationCursorMode(t *testing.T) {
	for _, tc := range []struct {
		key       tcell.Key
		normal    string
		appCursor string
	}{
		{tcell.KeyUp, "\x1b[A", "\x1bOA"},
		{tcell.KeyDown, "\x1b[B", "\x1bOB"},
		{tcell.KeyRight, "\x1b[C", "\x1bOC"},
		{tcell.KeyLeft, "\x1b[D", "\x1bOD"},
		{tcell.KeyHome, "\x1b[H", "\x1bOH"},
		{tcell.KeyEnd, "\x1b[F", "\x1bOF"},
	} {
		if got := string(keyBytes(tc.key, 0, false)); got != tc.normal {
			t.Errorf("key %v without DECCKM = %q, want %q", tc.key, got, tc.normal)
		}
		if got := string(keyBytes(tc.key, 0, true)); got != tc.appCursor {
			t.Errorf("key %v with DECCKM = %q, want %q", tc.key, got, tc.appCursor)
		}
	}
}

// Everything else is unaffected by the mode: only the cursor keys and
// Home/End move to SS3, and quietly changing anything else would break
// programs that are working today.
func TestOnlyCursorKeysChangeWithTheMode(t *testing.T) {
	for _, key := range []tcell.Key{
		tcell.KeyEnter, tcell.KeyTab, tcell.KeyEsc, tcell.KeyBackspace2,
		tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyDelete, tcell.KeyInsert,
		tcell.KeyF1, tcell.KeyF5,
	} {
		plain, app := keyBytes(key, 0, false), keyBytes(key, 0, true)
		if string(plain) != string(app) {
			t.Errorf("key %v changed with DECCKM: %q vs %q", key, plain, app)
		}
	}
	if got := string(keyBytes(tcell.KeyRune, 'x', true)); got != "x" {
		t.Errorf("a plain rune became %q under DECCKM", got)
	}
}

// And the mode has to be read from the pane the keys are going to, not
// assumed. This drives it the way a program does, through the emulator.
func TestAppCursorModeReadsThePanesTerminal(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	if appCursorMode(p) {
		t.Fatal("a fresh pane should not be in application cursor mode")
	}

	p.Term().Write([]byte("\x1b[?1h")) // DECCKM set, as ncurses' smkx does
	if !appCursorMode(p) {
		t.Error("after \\033[?1h the pane should be in application cursor mode")
	}

	p.Term().Write([]byte("\x1b[?1l")) // and rmkx on the way out
	if appCursorMode(p) {
		t.Error("after \\033[?1l the pane should be back in normal cursor mode")
	}

	if appCursorMode(nil) {
		t.Error("a nil pane should not report application cursor mode")
	}
}
