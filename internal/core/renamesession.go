package core

// Renaming a live session (Ctrl-B $, tmux's own key) is mostly not a
// core concern: the name is what the session's unix socket file and its
// persistence snapshot are called, so the rename has to happen on disk
// or `termdock ls`, `termdock attach -t NAME` and crash recovery all
// keep answering to the old one. core can't do that itself (it
// deliberately doesn't import internal/server), so the actual move is
// injected as Core.RenameSession and this file is just the glue: prompt,
// validate what can be validated here, apply, report.

// renameSessionPrompt opens the one-line input prompt prefilled with the
// current name.
func (c *Core) renameSessionPrompt() {
	if c.RenameSession == nil {
		c.statusMsg = "renaming a session isn't available here"
		return
	}
	c.startInput("rename-session", "Rename session: ", c.SessionName, ModeNormal)
}

// applySessionRename is confirmInput's "rename-session" branch. An empty
// name, or the name it already has, is treated as a cancel rather than
// an error: both mean "never mind", and neither is worth a complaint.
func (c *Core) applySessionRename(name string) {
	if name == "" || name == c.SessionName {
		return
	}
	if c.RenameSession == nil {
		c.statusMsg = "renaming a session isn't available here"
		return
	}
	if err := c.RenameSession(name); err != nil {
		c.statusMsg = "rename failed: " + err.Error()
		return
	}
	c.SessionName = name
	c.statusMsg = "session renamed to " + name
	// The snapshot is keyed by session name, so the one just written
	// under the old name is now orphaned; write a fresh one immediately
	// rather than waiting for the next structural change, so a crash in
	// between still recovers this session's layout.
	c.persistStateLocked()
}
