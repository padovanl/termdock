package core

import "testing"

func TestBellFiresOnlyOnActivityFalseToTrueEdge(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow() // window 1, active
	c.selectWindowIndex(0)
	bgID := c.windows[1].active.ID
	c.mu.Unlock()

	// First bit of output in the *background* window (1) while window 0
	// is active: should ring the bell exactly once.
	c.paneOutput(bgID)
	select {
	case <-c.Bell():
	default:
		t.Fatal("expected a bell on the first activity in a background window")
	}

	// A second burst of output from the same still-flagged window must
	// NOT ring again — otherwise a chatty background pane would ring
	// continuously instead of just once per "you haven't looked yet."
	c.paneOutput(bgID)
	select {
	case <-c.Bell():
		t.Fatal("expected no second bell while the window is already flagged")
	default:
	}

	// Switching to the window (clearing its activity flag) and getting
	// new output again should ring once more.
	c.mu.Lock()
	c.selectWindowIndex(1)
	c.selectWindowIndex(0)
	c.mu.Unlock()
	c.paneOutput(bgID)
	select {
	case <-c.Bell():
	default:
		t.Fatal("expected a bell again after the activity flag was cleared and re-tripped")
	}
}

func TestBellDoesNotFireForTheActiveWindowsOwnPane(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	fgID := c.win().active.ID
	c.mu.Unlock()

	c.paneOutput(fgID)
	select {
	case <-c.Bell():
		t.Fatal("output in the window you're already looking at shouldn't ring a bell")
	default:
	}
}
