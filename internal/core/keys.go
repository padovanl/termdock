package core

import (
	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"

	"github.com/gdamore/tcell/v2"
)

// handleKey is the top-level input dispatcher. Only tcell's numeric key
// constants are used here (for comparisons) — no actual terminal I/O
// happens in this package, so importing tcell for its constants is safe
// even though core has no real screen.
func (c *Core) handleKey(m proto.ClientMsg) Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.windows) == 0 {
		return Result{} // session mid-shutdown; a lingering connection raced us here
	}

	key := tcell.Key(m.KeyCode)
	r := m.KeyRune

	switch c.mode {
	case ModeCopy:
		res := c.handleCopyKey(key, r)
		c.markDirty()
		return res
	case ModeResize:
		c.handleResizeKey(key, r)
		c.markDirty()
		return Result{}
	case ModeInput:
		c.handleInputKey(key, r)
		c.markDirty()
		return Result{}
	case ModeConfirm:
		c.handleConfirmKey(r)
		c.markDirty()
		return Result{}
	case ModePicker:
		c.handlePickerKey(key, r)
		c.markDirty()
		return Result{}
	case ModeHelp:
		c.handleHelpKey(key, r)
		c.markDirty()
		return Result{}
	case ModeRegisters:
		c.handleRegisterKey(key, r)
		c.markDirty()
		return Result{}
	case ModeSessions:
		res := c.handleSessionsKey(key, r)
		c.markDirty()
		return res
	case ModeSearch:
		c.handleSearchKey(key, r)
		c.markDirty()
		return Result{}
	case ModeOverview:
		c.handleOverviewKey(key, r)
		c.markDirty()
		return Result{}
	case ModePopup:
		res := c.handlePopupKey(key, r)
		c.markDirty()
		return res
	case ModeOpener:
		res := c.handleOpenerKey(key, r)
		c.markDirty()
		return res
	case ModeQuickJump:
		c.handleQuickJumpKey(key, r)
		c.markDirty()
		return Result{}
	}

	if !c.prefix {
		if key == c.prefixKey {
			c.prefix = true
			return Result{}
		}
		c.forwardKey(key, r)
		return Result{}
	}

	c.prefix = false
	c.statusMsg = ""
	var res Result

	switch {
	case key == c.prefixKey:
		c.forwardKey(key, r) // double prefix-key press: send it through literally
	case r == 'v' || r == '%':
		c.doSplit(layout.Vertical)
	case r == 's' || r == '"':
		c.doSplit(layout.Horizontal)
	case key == tcell.KeyLeft || r == 'h':
		c.moveFocus(-1, 0)
	case key == tcell.KeyRight || r == 'l':
		c.moveFocus(1, 0)
	case key == tcell.KeyUp || r == 'k':
		c.moveFocus(0, -1)
	case key == tcell.KeyDown || r == 'j':
		c.moveFocus(0, 1)
	case r == 'o' || key == tcell.KeyTab:
		c.cycleFocus()
	case r == 'x':
		c.killActive()
	case r == 'z':
		c.toggleZoom()
	case r == 'r':
		c.mode = ModeResize
		c.statusMsg = "RESIZE: arrows/hjkl to resize, any other key to exit"
	case r == '[':
		c.enterCopyMode()
	case r == 'y':
		w := c.win()
		w.syncPanes = !w.syncPanes
		if w.syncPanes {
			c.statusMsg = "synchronized input to all panes: ON"
		} else {
			c.statusMsg = "synchronized input to all panes: OFF"
		}
	case r == 'c':
		c.newWindow()
	case r == 'n':
		c.switchWindow(1)
	case r == 'p':
		c.switchWindow(-1)
	case r == 'w':
		c.enterPicker()
	case r == 'W':
		// tmux's own last-window binding is lowercase 'l', but that's
		// already "move focus right" here (the vim-style hjkl pane
		// navigation above) — 'W' instead, next to 'w' (jump picker),
		// which this is a faster one-key version of for the single most
		// common case ("go back to the window I was just on").
		c.toggleLastWindow()
	case r == ';':
		c.toggleLastPane()
	case r >= '0' && r <= '9':
		c.selectWindowIndex(int(r - '0'))
	case r == ',':
		c.startInput("rename", "Rename window: ", c.windowDisplayName(c.win()), ModeNormal)
	case r == '&':
		c.confirmKillWindow()
	case r == ']':
		c.pasteRegister()
	case r == '=':
		c.enterRegisterPicker()
	case r == 'S':
		c.enterSessionPicker()
	case r == '/':
		c.enterGlobalSearch()
	case r == 'g':
		c.enterOverview()
	case r == 'P':
		c.togglePopup()
	case r == 'u':
		c.enterOpener()
	case r == '!':
		c.breakPaneToNewWindow()
	case r == 'Q':
		c.enterQuickJump()
	case r == ':':
		c.enterCommandPrompt()
	case r == ' ':
		c.cycleLayout()
	case r == 'R':
		c.respawnActivePane()
	case r == 'L':
		c.toggleLogging()
	case r == 'd':
		res.Detach = true
	case r == 'q':
		c.requestQuit()
	case r == '?':
		c.enterHelp()
	}
	c.markDirty()
	return res
}

func (c *Core) requestQuit() {
	for _, p := range c.panes {
		p.Close()
	}
	c.panes = map[int]*pane.Pane{}
	if !c.closed {
		c.closed = true
		close(c.exitCh)
	}
}


func (c *Core) forwardKey(key tcell.Key, r rune) {
	b := keyBytes(key, r)
	if b == nil {
		return
	}
	w := c.win()
	if w.syncPanes {
		for _, l := range layout.Leaves(w.root) {
			if p, ok := c.panes[l.ID]; ok {
				p.Write(b)
			}
		}
		return
	}
	if p, ok := c.panes[w.active.ID]; ok {
		p.Write(b)
	}
}

// handleConfirmKey answers a pending confirmKillWindow prompt: 'y'/'Y'
// carries it out, anything else — including Esc — cancels with no
// action, the safer default for a "did you mean to destroy this" prompt.
func (c *Core) handleConfirmKey(r rune) {
	c.mode = ModeNormal
	c.statusMsg = ""
	if r == 'y' || r == 'Y' {
		c.killWindow()
	}
}

func (c *Core) handleResizeKey(key tcell.Key, r rune) {
	active := c.win().active
	switch {
	case key == tcell.KeyLeft || r == 'h':
		layout.Resize(active, layout.Vertical, -resizeStep)
	case key == tcell.KeyRight || r == 'l':
		layout.Resize(active, layout.Vertical, resizeStep)
	case key == tcell.KeyUp || r == 'k':
		layout.Resize(active, layout.Horizontal, -resizeStep)
	case key == tcell.KeyDown || r == 'j':
		layout.Resize(active, layout.Horizontal, resizeStep)
	default:
		// Any non-direction key (Enter, Esc, q, ...) leaves resize-mode.
		c.mode = ModeNormal
		c.statusMsg = ""
		return
	}
	c.relayoutLocked()
}

func keyBytes(key tcell.Key, r rune) []byte {
	switch key {
	case tcell.KeyRune:
		return []byte(string(r))
	case tcell.KeyEnter:
		return []byte{'\r'}
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return []byte{0x7f}
	case tcell.KeyTab:
		return []byte{'\t'}
	case tcell.KeyBacktab:
		return []byte("\x1b[Z")
	case tcell.KeyEsc:
		return []byte{0x1b}
	case tcell.KeyUp:
		return []byte("\x1b[A")
	case tcell.KeyDown:
		return []byte("\x1b[B")
	case tcell.KeyRight:
		return []byte("\x1b[C")
	case tcell.KeyLeft:
		return []byte("\x1b[D")
	case tcell.KeyHome:
		return []byte("\x1b[H")
	case tcell.KeyEnd:
		return []byte("\x1b[F")
	case tcell.KeyPgUp:
		return []byte("\x1b[5~")
	case tcell.KeyPgDn:
		return []byte("\x1b[6~")
	case tcell.KeyDelete:
		return []byte("\x1b[3~")
	case tcell.KeyInsert:
		return []byte("\x1b[2~")
	case tcell.KeyF1:
		return []byte("\x1bOP")
	case tcell.KeyF2:
		return []byte("\x1bOQ")
	case tcell.KeyF3:
		return []byte("\x1bOR")
	case tcell.KeyF4:
		return []byte("\x1bOS")
	case tcell.KeyF5:
		return []byte("\x1b[15~")
	case tcell.KeyF6:
		return []byte("\x1b[17~")
	case tcell.KeyF7:
		return []byte("\x1b[18~")
	case tcell.KeyF8:
		return []byte("\x1b[19~")
	case tcell.KeyF9:
		return []byte("\x1b[20~")
	case tcell.KeyF10:
		return []byte("\x1b[21~")
	case tcell.KeyF11:
		return []byte("\x1b[23~")
	case tcell.KeyF12:
		return []byte("\x1b[24~")
	}
	if key >= tcell.KeyCtrlA && key <= tcell.KeyCtrlZ {
		// tcell numbers these Key constants from 65 (KeyCtrlSpace+1), not
		// from the ASCII control code itself — byte(key) would send the
		// *letter* (e.g. 'J' for Ctrl-J) instead of the control byte (10).
		return []byte{byte(key - tcell.KeyCtrlA + 1)}
	}
	if r != 0 {
		return []byte(string(r))
	}
	return nil
}
