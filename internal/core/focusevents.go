package core

// updateFocusEvents sends synthetic terminal focus-out/focus-in escape
// sequences (\x1b[O / \x1b[I, DEC's private "focus reporting" mode) to
// whichever pane is no longer / newly the one you're looking at, when
// focus-events is enabled (config's "focus-events on") — the same
// signal a real terminal sends an application on Alt-Tab, so apps that
// react to it (neovim's `:checktime`-on-FocusGained autoread, for one)
// notice you've switched back to their pane.
//
// This covers termdock's own internal pane/window switching only — not
// the *real* terminal losing OS-level focus (you Alt-Tabbing away from
// your terminal emulator entirely), which would need the attached
// client to detect that (tcell supports it) and forward it across the
// wire to the server, a bigger change than this session's scope covers.
// tmux's own focus-events does both; this is deliberately the narrower,
// self-contained half of it — the one that doesn't need any client or
// protocol changes, and the one that matters for the common case of
// switching panes/windows within a single, always-focused terminal.
func (c *Core) updateFocusEvents(newActiveID int) {
	if !c.focusEvents || newActiveID == c.focusedPaneID {
		return
	}
	if p, ok := c.panes[c.focusedPaneID]; ok {
		p.Write([]byte("\x1b[O"))
	}
	if p, ok := c.panes[newActiveID]; ok {
		p.Write([]byte("\x1b[I"))
	}
	c.focusedPaneID = newActiveID
}
