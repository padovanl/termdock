package core

import (
	"strings"
	"testing"
)

// The whole feature is one transition: a watched pane that was running
// something, and now isn't, must ring and say so.
func TestWatchedPaneFiresWhenItGoesIdle(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	c.doneWatch = map[int]bool{id: true} // armed, and it was busy

	// paneIsBusy reads the live process, which for a test pane is the
	// shell itself — i.e. idle. So this is exactly the busy -> idle edge.
	c.checkDoneWatches()

	if _, still := c.doneWatch[id]; still {
		t.Error("a fired watch should disarm itself, not ring on every command after")
	}
	if !strings.Contains(c.statusMsg, "finished") {
		t.Errorf("status %q should report that it finished", c.statusMsg)
	}
}

// A pane that is still busy must stay armed and stay quiet.
func TestWatchedPaneStaysQuietWhileBusy(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	// Make "busy" true by pointing shellName at something the pane's
	// foreground can never be, so paneIsBusy reports busy.
	c.shellName = "no-such-shell"
	c.doneWatch = map[int]bool{id: true}
	c.statusMsg = ""

	c.checkDoneWatches()

	if _, still := c.doneWatch[id]; !still {
		t.Error("a busy pane's watch should stay armed")
	}
	if c.statusMsg != "" {
		t.Errorf("nothing should be reported while it is still running, got %q", c.statusMsg)
	}
}

// Arming on a pane that is already idle must wait for the *next*
// command rather than firing straight away.
func TestArmingAnIdlePaneWaitsForTheNextCommand(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	c.watchDone() // pane is at a prompt

	if busy := c.doneWatch[id]; busy {
		t.Error("an idle pane should be recorded as idle, so it does not fire immediately")
	}
	c.statusMsg = ""
	c.checkDoneWatches()
	if c.statusMsg != "" {
		t.Errorf("arming on an idle pane fired immediately: %q", c.statusMsg)
	}
	if _, still := c.doneWatch[id]; !still {
		t.Error("the watch should still be armed, waiting for the next command")
	}
}

// It toggles: arming the wrong pane has to be undoable.
func TestWatchDoneToggles(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID
	c.watchDone()
	if _, on := c.doneWatch[id]; !on {
		t.Fatal("first press should arm the watch")
	}
	c.watchDone()
	if _, on := c.doneWatch[id]; on {
		t.Fatal("second press should disarm it")
	}
	if !strings.Contains(c.statusMsg, "no longer") {
		t.Errorf("status %q should confirm it was turned off", c.statusMsg)
	}
}

// An armed pane is tagged in its title, so it is visible rather than
// something you have to remember having done.
func TestWatchedPaneIsMarkedInItsTitle(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	c.watchDone()
	c.mu.Unlock()

	f := c.Frame()
	var title string
	for _, p := range f.Panes {
		if p.ID == id {
			title = p.Title
		}
	}
	if !strings.Contains(title, "⏳") {
		t.Errorf("watched pane's title %q should be marked", title)
	}
}

// A watched pane that closes must drop off the list rather than being
// checked forever.
func TestWatchIsDroppedWhenThePaneCloses(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doneWatch = map[int]bool{99999: true} // an id no pane has
	c.checkDoneWatches()
	if len(c.doneWatch) != 0 {
		t.Errorf("watch on a closed pane should be dropped, %d left", len(c.doneWatch))
	}
}
