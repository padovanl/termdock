package core

import (
	"fmt"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/vt10x"
)

// Forwarding the mouse to the program in a pane.
//
// termdock has always used the mouse for its own gestures: click to
// focus, drag a divider, drag a title onto a window tab, drag to select
// text. That is right for a shell prompt, which has no use for a click.
// It is wrong for htop, vim, less or anything else that asks the
// terminal to report mouse events, because those programs then never
// hear about a click at all: the emulator recorded that the program had
// turned reporting on (see internal/vt10x, modes 1000/1002/1003/1006)
// and nothing ever read that back. Clicking htop's tabs did nothing, in
// a pane or in the popup.
//
// So: a pane whose program asked for the mouse gets the mouse, and
// holding shift takes it back for termdock, which is the escape hatch
// tmux uses and the one people already know.

// mouseForwardState remembers what the button was doing, because a
// terminal mouse report is a state machine and a single event does not
// say enough on its own. tcell reports a release as "no buttons", naming
// no button, and SGR reports have to name the button being released; and
// a drag is only distinguishable from a press by whether the button was
// already down.
type mouseForwardState struct {
	down   bool
	button int // the X11 button code currently held
	col    int // last reported cell, to avoid repeating identical motion
	row    int
}

// wantsMouse reports whether the program in p has asked for mouse
// reporting at all.
func wantsMouse(p *pane.Pane) bool {
	if p == nil {
		return false
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	return t.Mode()&vt10x.ModeMouseMask != 0
}

// mouseModes reads the two things the encoding depends on: whether the
// program wants SGR-style reports (mode 1006) and whether it wants
// motion while a button is held (1002/1003).
func mouseModes(p *pane.Pane) (sgr, motion bool) {
	if p == nil {
		return false, false
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	m := t.Mode()
	return m&vt10x.ModeMouseSgr != 0, m&(vt10x.ModeMouseMotion|vt10x.ModeMouseMany) != 0
}

// buttonCode maps tcell's button mask to the X11 code a terminal report
// uses. The orders differ, which is an easy thing to get backwards:
// tcell's Button2 is the *right* button while X11's 2 is the right one
// and 1 is the middle, so they cross over.
func buttonCode(b tcell.ButtonMask) (int, bool) {
	switch {
	case b&tcell.Button1 != 0: // left
		return 0, true
	case b&tcell.Button3 != 0: // tcell's middle
		return 1, true
	case b&tcell.Button2 != 0: // tcell's right
		return 2, true
	case b&tcell.WheelUp != 0:
		return 64, true
	case b&tcell.WheelDown != 0:
		return 65, true
	case b&tcell.WheelLeft != 0:
		return 66, true
	case b&tcell.WheelRight != 0:
		return 67, true
	}
	return 0, false
}

// modifierBits are the modifier flags a report carries alongside the
// button. Shift is deliberately absent: it is termdock's escape hatch,
// so a shifted click never reaches the program to begin with and saying
// "shift was held" about an event the program is not receiving would be
// meaningless.
func modifierBits(mod tcell.ModMask) int {
	bits := 0
	if mod&tcell.ModAlt != 0 {
		bits |= 8
	}
	if mod&tcell.ModCtrl != 0 {
		bits |= 16
	}
	return bits
}

// maxLegacyCoord is the highest cell a pre-SGR report can address: the
// coordinate is sent as a single byte biased by 32, so 223 is the end of
// the road. Programs that care about wider terminals ask for SGR (1006),
// which has no such limit; for the ones that do not, a click past the
// limit is dropped rather than reported at a wrong cell.
const maxLegacyCoord = 223

// mouseReport encodes one event. col and row are zero-based within the
// pane; the wire format is one-based.
func mouseReport(sgr bool, code, col, row int, release bool) []byte {
	if col < 0 || row < 0 {
		return nil
	}
	if sgr {
		final := 'M'
		if release {
			final = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", code, col+1, row+1, final))
	}
	// The legacy encoding cannot name the button being released, so every
	// release is button 3. This is why SGR exists.
	if release {
		code = 3
	}
	if col > maxLegacyCoord || row > maxLegacyCoord || code > 255-32 {
		return nil
	}
	return []byte{0x1b, '[', 'M', byte(32 + code), byte(32 + col + 1), byte(32 + row + 1)}
}

// forwardMouse hands the event to the program in the pane under the
// pointer, and reports whether it did. False means termdock should treat
// the event as its own gesture, which is the answer for a plain shell
// prompt, for a shift-held click, and for everything outside a pane.
func (c *Core) forwardMouse(m proto.ClientMsg) bool {
	if c.mode != ModeNormal && c.mode != ModePopup {
		return false // termdock's own modal UI owns the mouse while it is up
	}
	mod := tcell.ModMask(m.MouseMod)
	if mod&tcell.ModShift != 0 {
		// The escape hatch: shift-click always drives termdock, so a pane
		// running a full-screen program can still be focused, selected
		// from, and resized.
		return false
	}

	target, originX, originY := c.mouseTarget(m.MouseX, m.MouseY)
	if target == nil || !wantsMouse(target) {
		return false
	}

	buttons := tcell.ButtonMask(m.MouseButtons)
	col, row := m.MouseX-originX, m.MouseY-originY
	sgr, motion := mouseModes(target)

	switch {
	case buttons == tcell.ButtonNone:
		if !c.mouseFwd.down {
			return false // a release we never saw the press for
		}
		code := c.mouseFwd.button + modifierBits(mod)
		c.mouseFwd = mouseForwardState{}
		return writeReport(target, mouseReport(sgr, code, col, row, true))

	case buttons&(tcell.WheelUp|tcell.WheelDown|tcell.WheelLeft|tcell.WheelRight) != 0:
		code, ok := buttonCode(buttons)
		if !ok {
			return false
		}
		// A wheel report is a press with no matching release, so it must
		// not disturb whatever the buttons are doing.
		return writeReport(target, mouseReport(sgr, code+modifierBits(mod), col, row, false))

	case c.mouseFwd.down:
		// Still held: this is a drag. Only programs that asked for motion
		// want to hear about it, and only when the cell actually changed,
		// since tcell repeats the event as the pointer moves within a cell.
		if !motion || (col == c.mouseFwd.col && row == c.mouseFwd.row) {
			return true // still ours to swallow: the button is down in their pane
		}
		c.mouseFwd.col, c.mouseFwd.row = col, row
		code := c.mouseFwd.button + 32 + modifierBits(mod)
		return writeReport(target, mouseReport(sgr, code, col, row, false))

	default:
		code, ok := buttonCode(buttons)
		if !ok {
			return false
		}
		// Clicking a pane still focuses it, even though the click then
		// belongs to the program: otherwise clicking into a pane running
		// htop would leave the keyboard pointed somewhere else.
		if leaf := c.leafAt(m.MouseX, m.MouseY); leaf != nil && c.mode == ModeNormal {
			c.setActive(leaf)
		}
		c.mouseFwd = mouseForwardState{down: true, button: code, col: col, row: row}
		return writeReport(target, mouseReport(sgr, code+modifierBits(mod), col, row, false))
	}
}

// writeReport sends the encoded bytes, reporting whether there were any.
// An event the encoding cannot express (a click past column 223 without
// SGR) is dropped rather than sent at the wrong cell, and dropping it is
// still "handled": the program owns that pane, and quietly running
// termdock's own gesture instead would be worse than nothing happening.
func writeReport(p *pane.Pane, b []byte) bool {
	if b != nil {
		p.Write(b)
	}
	return true
}

// mouseTarget is the pane under (x, y) and the screen position of that
// pane's top-left content cell, which is what turns an absolute click
// into the pane-relative cell a report names. The pane is nil when the
// point is on a border, on the status bar, or outside the popup while
// the popup is up.
//
// Only the origin is returned rather than a rectangle: the two rect
// types in play here (layout.Rect for a pane, proto.Rect for the popup)
// are different types, and the offset is the only part either one is
// needed for.
func (c *Core) mouseTarget(x, y int) (p *pane.Pane, originX, originY int) {
	if c.mode == ModePopup {
		if !c.popupFits() {
			return nil, 0, 0
		}
		r := c.popupRect()
		// The popup draws a border of its own, so the program inside it
		// starts one cell in from the box.
		x0, y0 := r.X+1, r.Y+1
		if x < x0 || x >= x0+r.W-2 || y < y0 || y >= y0+r.H-2 {
			return nil, 0, 0
		}
		return c.popup, x0, y0
	}
	leaf := c.leafAt(x, y)
	if leaf == nil {
		return nil, 0, 0
	}
	pane, ok := c.panes[leaf.ID]
	if !ok {
		return nil, 0, 0
	}
	return pane, leaf.Rect.X, leaf.Rect.Y
}
