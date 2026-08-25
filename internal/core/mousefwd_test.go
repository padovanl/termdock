package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"

	"github.com/padovanl/termdock/internal/proto"
)

// enableMouse makes a pane's program ask for mouse reporting, the way
// htop or vim does, by driving the emulator with the real sequences.
func enableMouse(t *testing.T, c *Core, id int, seq string) {
	t.Helper()
	c.panes[id].Term().Write([]byte(seq))
}

// paneOutput drains what termdock wrote back towards the program. The
// pane's pty echoes, so reading the emulator is the wrong place to look;
// this reads the bytes the pane was asked to write.
func fwdMsg(x, y int, buttons tcell.ButtonMask, mod tcell.ModMask) proto.ClientMsg {
	return proto.ClientMsg{
		Kind: "mouse", MouseX: x, MouseY: y,
		MouseButtons: int32(buttons), MouseMod: int32(mod),
	}
}

// The encoding is the part a program actually parses, so it is worth
// pinning exactly rather than testing that "something" was sent.
func TestMouseReportEncoding(t *testing.T) {
	for _, tc := range []struct {
		name           string
		sgr            bool
		code, col, row int
		release        bool
		want           string
	}{
		{"legacy press, top-left", false, 0, 0, 0, false, "\x1b[M\x20\x21\x21"},
		{"legacy press, further in", false, 0, 9, 4, false, "\x1b[M\x20\x2a\x25"},
		// The legacy form cannot name which button was released.
		{"legacy release is always button 3", false, 2, 0, 0, true, "\x1b[M\x23\x21\x21"},
		{"sgr press keeps the button", true, 2, 9, 4, false, "\x1b[<2;10;5M"},
		{"sgr release keeps the button, lowercase m", true, 2, 9, 4, true, "\x1b[<2;10;5m"},
		{"sgr wheel up", true, 64, 0, 0, false, "\x1b[<64;1;1M"},
	} {
		got := string(mouseReport(tc.sgr, tc.code, tc.col, tc.row, tc.release))
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	// Past the byte-encodable range the legacy form must drop the event
	// rather than report a wrong cell; SGR has no such limit.
	if got := mouseReport(false, 0, maxLegacyCoord+1, 0, false); got != nil {
		t.Errorf("legacy report past column %d returned %q, want nothing", maxLegacyCoord, got)
	}
	if got := mouseReport(true, 0, 999, 999, false); string(got) != "\x1b[<0;1000;1000M" {
		t.Errorf("sgr past the legacy limit = %q, want it encoded anyway", got)
	}
}

// tcell's button order and X11's cross over, which is exactly the sort
// of mapping that gets written backwards and never noticed.
func TestButtonCodesMatchX11NotTcellOrder(t *testing.T) {
	for _, tc := range []struct {
		mask tcell.ButtonMask
		want int
		what string
	}{
		{tcell.Button1, 0, "left"},
		{tcell.Button3, 1, "middle (tcell calls it Button3, X11 calls it 1)"},
		{tcell.Button2, 2, "right (tcell calls it Button2, X11 calls it 2)"},
		{tcell.WheelUp, 64, "wheel up"},
		{tcell.WheelDown, 65, "wheel down"},
	} {
		got, ok := buttonCode(tc.mask)
		if !ok || got != tc.want {
			t.Errorf("%s: got %d (ok=%v), want %d", tc.what, got, ok, tc.want)
		}
	}
	if _, ok := buttonCode(tcell.ButtonNone); ok {
		t.Error("no buttons should not map to a button code")
	}
}

// A shell prompt has no use for a click, so termdock keeps its own
// gestures until a program says otherwise.
func TestMouseGoesToTermdockUntilAProgramAsksForIt(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	r := c.win().active.Rect
	msg := fwdMsg(r.X+1, r.Y+1, tcell.Button1, 0)

	if c.forwardMouse(msg) {
		t.Fatal("a plain pane forwarded the mouse; termdock's own gestures would stop working")
	}

	enableMouse(t, c, id, "\x1b[?1000h")
	if !c.forwardMouse(msg) {
		t.Error("after the program enabled mouse reporting the click should go to it")
	}
}

// Shift is the way back to termdock's own gestures while a full-screen
// program is running, and it has to work even mid-drag.
func TestShiftTakesTheMouseBackFromTheProgram(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	enableMouse(t, c, id, "\x1b[?1000h")
	r := c.win().active.Rect

	if c.forwardMouse(fwdMsg(r.X+1, r.Y+1, tcell.Button1, tcell.ModShift)) {
		t.Error("a shift-click was forwarded to the program instead of driving termdock")
	}
}

// A release names no button in tcell, so the state has to remember which
// one went down; and a release with no press behind it is not ours.
func TestReleaseWithoutAPressIsNotForwarded(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	enableMouse(t, c, c.win().active.ID, "\x1b[?1000h\x1b[?1006h")
	r := c.win().active.Rect

	if c.forwardMouse(fwdMsg(r.X+1, r.Y+1, tcell.ButtonNone, 0)) {
		t.Error("a release with no press before it was forwarded")
	}
	if c.mouseFwd.down {
		t.Error("no button should be recorded as held")
	}

	c.forwardMouse(fwdMsg(r.X+1, r.Y+1, tcell.Button2, 0))
	if !c.mouseFwd.down || c.mouseFwd.button != 2 {
		t.Fatalf("after a right-click press: down=%v button=%d, want true/2", c.mouseFwd.down, c.mouseFwd.button)
	}
	if !c.forwardMouse(fwdMsg(r.X+1, r.Y+1, tcell.ButtonNone, 0)) {
		t.Error("the matching release was not forwarded")
	}
	if c.mouseFwd.down {
		t.Error("the release should clear the held button")
	}
}

// Coordinates are pane-relative: a click on the second pane's first
// content cell is column 1 of that pane, not of the screen. The pane is
// offset from the screen origin, so the two genuinely differ.
func TestCoordinatesAreRelativeToThePaneNotTheScreen(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	if len(leaves) != 2 {
		t.Fatalf("expected 2 panes, got %d", len(leaves))
	}
	right := leaves[1]
	if right.Rect.X == 0 {
		t.Fatalf("a vertical split left the second pane at column 0, so this proves nothing")
	}

	enableMouse(t, c, right.ID, "\x1b[?1000h\x1b[?1006h")
	_, originX, originY := c.mouseTarget(right.Rect.X, right.Rect.Y)
	if originX != right.Rect.X || originY != right.Rect.Y {
		t.Fatalf("origin for the right pane = (%d,%d), want (%d,%d)",
			originX, originY, right.Rect.X, right.Rect.Y)
	}

	// A click three cells into that pane must report column 4, one-based,
	// regardless of where the pane sits on screen.
	x, y := right.Rect.X+3, right.Rect.Y+2
	if !c.forwardMouse(fwdMsg(x, y, tcell.Button1, 0)) {
		t.Fatal("the click was not forwarded")
	}
	want := "\x1b[<0;4;3M"
	if got := string(mouseReport(true, 0, x-originX, y-originY, false)); got != want {
		t.Errorf("report for a click 3 cells in = %q, want %q", got, want)
	}
}

// While a modal of termdock's own is up it owns the mouse, whatever the
// program underneath asked for.
func TestTermdockModesKeepTheMouse(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	enableMouse(t, c, c.win().active.ID, "\x1b[?1000h")
	r := c.win().active.Rect
	msg := fwdMsg(r.X+1, r.Y+1, tcell.Button1, 0)

	for _, m := range []Mode{ModeCopy, ModeOverview, ModeSettings, ModeHistory, ModeTimeline, ModePicker} {
		c.mode = m
		if c.forwardMouse(msg) {
			t.Errorf("mode %v forwarded the mouse to the program", m)
		}
	}
}

// The mode the program asked for decides the encoding, and asking for
// motion is separate from asking for buttons.
func TestModesAreReadBackFromTheProgram(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	if sgr, motion := mouseModes(p); sgr || motion {
		t.Fatalf("a fresh pane reported sgr=%v motion=%v, want both off", sgr, motion)
	}
	p.Term().Write([]byte("\x1b[?1002h"))
	if _, motion := mouseModes(p); !motion {
		t.Error("mode 1002 should ask for motion")
	}
	p.Term().Write([]byte("\x1b[?1006h"))
	if sgr, _ := mouseModes(p); !sgr {
		t.Error("mode 1006 should ask for SGR reports")
	}
	p.Term().Write([]byte("\x1b[?1000l\x1b[?1002l\x1b[?1003l"))
	if wantsMouse(p) {
		t.Error("with every reporting mode turned off the pane should not want the mouse")
	}
}

// The case this was reported from: htop in the floating popup, where
// clicking its tabs did nothing. The popup draws its own border, so the
// program starts one cell inside the box, and a click has to be reported
// relative to that rather than to the box or the screen.
func TestPopupForwardsTheMouseToItsProgram(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.togglePopup()
	if c.popup == nil || c.mode != ModePopup {
		t.Skip("the popup did not open in this environment")
	}
	if !c.popupFits() {
		t.Skip("the test terminal is too small for the popup")
	}

	r := c.popupRect()
	inside := fwdMsg(r.X+1, r.Y+1, tcell.Button1, 0)

	// A shell in the popup leaves the mouse to termdock.
	if c.forwardMouse(inside) {
		t.Error("a popup running a shell forwarded the mouse")
	}

	c.popup.Term().Write([]byte("\x1b[?1000h\x1b[?1006h"))
	target, originX, originY := c.mouseTarget(r.X+1, r.Y+1)
	if target != c.popup {
		t.Fatal("a point inside the popup did not resolve to the popup's pane")
	}
	if originX != r.X+1 || originY != r.Y+1 {
		t.Errorf("popup content origin = (%d,%d), want (%d,%d) — one cell in from its border",
			originX, originY, r.X+1, r.Y+1)
	}
	if !c.forwardMouse(inside) {
		t.Error("a click inside the popup was not forwarded to its program")
	}

	// Outside the box is not the program's, so clicking away still
	// dismisses the popup the way it always did.
	if out, _, _ := c.mouseTarget(r.X-1, r.Y-1); out != nil {
		t.Error("a point outside the popup resolved to the popup's pane")
	}
}
