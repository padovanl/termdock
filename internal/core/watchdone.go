package core

import (
	"fmt"

	"github.com/padovanl/termdock/internal/layout"
)

// "Tell me when this finishes" (Ctrl-B m) marks a pane as being watched;
// the moment whatever it is running exits and it drops back to a bare
// shell prompt, termdock rings the terminal bell and says which pane it
// was. It's for the twenty-minute build, the test run, the deploy — the
// jobs you start and then go and do something else during, which is
// exactly when you stop noticing a pane you can't see.
//
// It needs no shell configuration at all, which is the interesting part.
// termdock already asks the pty which process group is in the foreground
// (TIOCGPGRP, then /proc/<pgid>/comm — the same reading that keeps pane
// titles up to date, see pane.ForegroundTitle). "Busy" is simply that
// name being something other than the shell's, so "finished" is the
// transition back. No hooks, no wrapper, no `; notify-send` remembered
// in advance — and it can be armed *after* the command is already
// running, which a wrapper fundamentally cannot.
//
// tmux has nothing equivalent: its monitor-silence watches for output
// going quiet, which fires on a build that pauses to think and stays
// silent on one that finishes without a final line.

// watchDone marks/unmarks the active pane as one to report on. Toggling
// it is deliberate: arming it on the wrong pane is easy, and there needs
// to be an obvious way to take it back.
func (c *Core) watchDone() {
	id := c.win().active.ID
	if c.doneWatch == nil {
		c.doneWatch = map[int]bool{}
	}
	if _, watching := c.doneWatch[id]; watching {
		delete(c.doneWatch, id)
		c.statusMsg = "no longer watching this pane"
		return
	}
	// Remember whether it's busy *now*, so a pane armed while already
	// sitting at a prompt doesn't fire immediately — it waits for the
	// next command to run and finish, which is what arming it there
	// clearly means.
	c.doneWatch[id] = c.paneIsBusy(id)
	if c.doneWatch[id] {
		c.statusMsg = "will tell you when this pane's command finishes"
	} else {
		c.statusMsg = "will tell you when this pane's next command finishes"
	}
}

// paneIsBusy reports whether something other than the shell itself holds
// the pane's foreground. Reading it costs an ioctl and a small /proc
// read, which is already paid once per pane per frame for its title.
func (c *Core) paneIsBusy(id int) bool {
	p, ok := c.panes[id]
	if !ok {
		return false
	}
	fg := p.ForegroundTitle()
	return fg != "" && fg != c.shellName
}

// checkDoneWatches fires for every watched pane that has gone from busy
// to idle since the last frame. Called from Frame, which is already
// sampling each pane's foreground process to title it — so this adds no
// polling of its own and inherits the same cadence.
//
// c.mu is held by the caller.
func (c *Core) checkDoneWatches() {
	for id, wasBusy := range c.doneWatch {
		if _, alive := c.panes[id]; !alive {
			delete(c.doneWatch, id) // the pane closed; nothing left to report
			continue
		}
		busy := c.paneIsBusy(id)
		switch {
		case busy:
			c.doneWatch[id] = true
		case wasBusy:
			// Busy -> idle: it finished. One shot, then disarmed, so a
			// pane you keep working in doesn't ring on every command.
			delete(c.doneWatch, id)
			c.statusMsg = fmt.Sprintf("✅ %s finished", c.paneLabel(id))
			c.ringBell()
		}
	}
}

// paneLabel names a pane the way the picker does ("2:npm"), so the
// message points at something the user can actually find.
func (c *Core) paneLabel(id int) string {
	for wi, w := range c.windows {
		for pi, l := range layout.Leaves(w.root) {
			if l.ID == id {
				return fmt.Sprintf("%d:%s › %d:%s", wi, c.windowDisplayName(w), pi+1, c.pickerPaneTitle(id))
			}
		}
	}
	return c.pickerPaneTitle(id)
}

// watchedPaneMarker is the tag put on a watched pane's title, so an
// armed pane is visible rather than something you have to remember.
func (c *Core) watchedPaneMarker(id int) string {
	if _, watching := c.doneWatch[id]; watching {
		return " [⏳]"
	}
	return ""
}
