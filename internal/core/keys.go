package core

import (
	"time"

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
	case ModeSettings:
		c.handleSettingsKey(key, r)
		c.markDirty()
		return Result{}
	}

	if !c.prefix {
		if key == c.prefixKey {
			c.prefix = true
			return Result{}
		}
		// Repeatable focus moves: right after a prefixed arrow, a bare
		// arrow keeps moving, so crossing three panes is Ctrl-B ←←←
		// instead of pressing the prefix again for every single step.
		// tmux's `bind -r` / repeat-time, restricted here to the arrow
		// keys and never to hjkl: h/j/k/l are ordinary text, and eating
		// the "h" of something you started typing a moment after
		// switching panes would be a far worse bug than the extra
		// keystroke this saves.
		if act, ok := repeatableArrow(key); ok && c.repeatActive() {
			res := c.dispatchAction(act)
			c.markDirty()
			return res
		}
		c.repeatUntil = time.Time{} // any other key ends the repeat window
		c.forwardKey(key, r)
		return Result{}
	}

	c.prefix = false
	c.statusMsg = ""
	var res Result

	switch {
	case key == c.prefixKey:
		c.forwardKey(key, r) // double prefix-key press: send it through literally
	// Arrow keys and Tab are always-available alternates for their
	// default rune bindings (hjkl, o) regardless of any "bind"
	// override — see bindings.go's package doc for why rebinding is
	// scoped to runes only.
	case key == tcell.KeyLeft:
		res = c.dispatchAction(actFocusLeft)
	case key == tcell.KeyRight:
		res = c.dispatchAction(actFocusRight)
	case key == tcell.KeyUp:
		res = c.dispatchAction(actFocusUp)
	case key == tcell.KeyDown:
		res = c.dispatchAction(actFocusDown)
	case key == tcell.KeyTab:
		res = c.dispatchAction(actCycleFocus)
	// Digits jump straight to window N unless the config deliberately
	// rebound that digit, in which case the explicit "bind" wins — this
	// case is checked before the bindings map below, so without the
	// override test a `bind 5 …` line could never fire at all.
	case r >= '0' && r <= '9' && !c.bindOverridden[r]:
		c.selectWindowIndex(int(r - '0'))
	default:
		if act, ok := c.bindings[r]; ok {
			res = c.dispatchAction(act)
		}
	}
	c.markDirty()
	return res
}

// repeatableArrow maps an arrow key to the focus move it repeats, for
// the no-prefix-needed repeat window (see handleKey). Only focus moves
// repeat: they're the commands you genuinely run several times in a row,
// and unlike, say, a repeated split or kill, doing one more than you
// meant to costs nothing.
func repeatableArrow(key tcell.Key) (action, bool) {
	switch key {
	case tcell.KeyLeft:
		return actFocusLeft, true
	case tcell.KeyRight:
		return actFocusRight, true
	case tcell.KeyUp:
		return actFocusUp, true
	case tcell.KeyDown:
		return actFocusDown, true
	}
	return "", false
}

// repeatActive reports whether a bare arrow should still be taken as a
// focus move rather than passed to the pane.
func (c *Core) repeatActive() bool {
	return c.repeatTime > 0 && !c.repeatUntil.IsZero() && time.Now().Before(c.repeatUntil)
}

// armRepeat (re)opens the repeat window, extending it on every repeated
// move so a steady walk across panes doesn't expire mid-way.
func (c *Core) armRepeat() {
	if c.repeatTime > 0 {
		c.repeatUntil = time.Now().Add(c.repeatTime)
	}
}

// armRepeatIfMoved opens the repeat window only for a focus move that
// actually went somewhere. A move with nowhere to go — every arrow in a
// single-pane window, or the one direction with no neighbour — used to
// arm it anyway, which meant termdock swallowed every arrow key for the
// next second on behalf of a command that had done nothing. Arrows are
// how shell history works, so that is expensive.
//
// A failed move deliberately doesn't *close* an open window either:
// walking across panes and overshooting the last one shouldn't drop you
// out of navigation just as you reach for the arrow that comes back.
func (c *Core) armRepeatIfMoved(moved bool) {
	if moved {
		c.armRepeat()
	}
}

// dispatchAction runs act — the single place every prefix-key command
// is actually invoked from, whether reached via its default rune, a
// config "bind" override, or one of the fixed arrow/Tab alternates
// above. c.mu is already held (handleKey's caller).
func (c *Core) dispatchAction(act action) Result {
	// Anything that isn't a focus move closes the window in which a bare
	// arrow repeats one, so an unrelated command can't leave arrows
	// hijacked afterwards. Focus moves themselves are handled below,
	// where whether the move actually went anywhere is known.
	switch act {
	case actFocusLeft, actFocusRight, actFocusUp, actFocusDown:
	default:
		c.repeatUntil = time.Time{}
	}
	var res Result
	switch act {
	case actVSplit:
		c.doSplit(layout.Vertical)
	case actHSplit:
		c.doSplit(layout.Horizontal)
	case actFocusLeft:
		c.armRepeatIfMoved(c.moveFocus(-1, 0))
	case actFocusRight:
		c.armRepeatIfMoved(c.moveFocus(1, 0))
	case actFocusUp:
		c.armRepeatIfMoved(c.moveFocus(0, -1))
	case actFocusDown:
		c.armRepeatIfMoved(c.moveFocus(0, 1))
	case actCycleFocus:
		c.cycleFocus()
	case actClosePane:
		c.killActive()
	case actZoom:
		c.toggleZoom()
	case actResizeMode:
		c.mode = ModeResize
		c.statusMsg = "RESIZE: arrows/hjkl to resize, any other key to exit"
	case actCopyMode:
		c.enterCopyMode()
	case actSyncPanes:
		w := c.win()
		w.syncPanes = !w.syncPanes
		if w.syncPanes {
			c.statusMsg = "synchronized input to all panes: ON"
		} else {
			c.statusMsg = "synchronized input to all panes: OFF"
		}
	case actNewWindow:
		c.newWindow()
	case actNextWindow:
		c.switchWindow(1)
	case actPrevWindow:
		c.switchWindow(-1)
	case actJumpPicker:
		c.enterPicker()
	case actLastWindow:
		c.toggleLastWindow()
	case actLastPane:
		c.toggleLastPane()
	case actOverview:
		c.enterOverview()
	case actGlobalSearch:
		c.enterGlobalSearch()
	case actSwitchSession:
		c.enterSessionPicker()
	case actPopup:
		c.togglePopup()
	case actOpener:
		c.enterOpener()
	case actBreakPane:
		c.breakPaneToNewWindow()
	case actQuickJump:
		c.enterQuickJump()
	case actCommandPrompt:
		c.enterCommandPrompt()
	case actCycleLayout:
		c.cycleLayout()
	case actRespawnPane:
		c.respawnActivePane()
	case actToggleLogging:
		c.toggleLogging()
	case actRenameWindow:
		c.startInput("rename", "Rename window: ", c.windowDisplayName(c.win()), ModeNormal)
	case actRenameSession:
		c.renameSessionPrompt()
	case actKillWindow:
		c.confirmKillWindow()
	case actPaste:
		c.pasteRegister()
	case actPastePicker:
		c.enterRegisterPicker()
	case actDetach:
		res.Detach = true
	case actQuit:
		c.confirmQuit()
	case actSettings:
		c.enterSettings()
	case actHelp:
		c.enterHelp()
	}
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

// handleConfirmKey answers a pending confirm prompt (see
// confirmKillWindow/confirmQuit, whichever set c.pendingConfirm most
// recently): 'y'/'Y' runs it, anything else — including Esc — cancels
// with no action, the safer default for a "did you mean to destroy
// this" prompt.
func (c *Core) handleConfirmKey(r rune) {
	c.mode = ModeNormal
	c.statusMsg = ""
	fn := c.pendingConfirm
	c.pendingConfirm = nil
	if (r == 'y' || r == 'Y') && fn != nil {
		fn()
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
