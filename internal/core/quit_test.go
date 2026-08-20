package core

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
)

func TestConfirmQuitAsksBeforeQuitting(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.newWindow()
	c.confirmQuit()
	mode := c.mode
	msg := c.statusMsg
	closed := c.closed
	c.mu.Unlock()

	if mode != ModeConfirm {
		t.Fatalf("confirmQuit should enter ModeConfirm, got %v", mode)
	}
	if !strings.Contains(msg, "quit") {
		t.Fatalf("statusMsg should explain what's about to happen, got %q", msg)
	}
	if closed {
		t.Fatal("confirmQuit should not have closed anything yet — that's what the prompt is for")
	}
}

func TestConfirmQuitCancelledLeavesSessionRunning(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	windowsBefore := len(c.windows)
	panesBefore := len(c.panes)
	c.confirmQuit()
	c.handleConfirmKey('n')
	mode := c.mode
	windowsAfter := len(c.windows)
	panesAfter := len(c.panes)
	closed := c.closed
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("cancelling should return to ModeNormal, got %v", mode)
	}
	if windowsAfter != windowsBefore || panesAfter != panesBefore {
		t.Fatalf("'n' must not touch anything: windows %d->%d, panes %d->%d", windowsBefore, windowsAfter, panesBefore, panesAfter)
	}
	if closed {
		t.Fatal("'n' should not close the session")
	}
}

func TestConfirmQuitConfirmedActuallyQuits(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.confirmQuit()
	c.handleConfirmKey('y')
	closed := c.closed
	panesLeft := len(c.panes)
	c.mu.Unlock()

	if !closed {
		t.Fatal("'y' should have run requestQuit, marking the session closed")
	}
	if panesLeft != 0 {
		t.Fatalf("requestQuit should have emptied c.panes, %d left", panesLeft)
	}
	select {
	case <-c.Exited():
	default:
		t.Fatal("Exited() should be signaled once requestQuit has run")
	}
}

// TestQuitActionRoutesThroughConfirmQuit checks the 'q' keybinding
// itself (dispatchAction's actQuit case) asks first, rather than
// quitting immediately the way it used to before this — the same
// inconsistency-with-'&' fix that motivated adding confirmQuit at all.
func TestQuitActionRoutesThroughConfirmQuit(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	res := c.dispatchAction(actQuit)
	mode := c.mode
	closed := c.closed
	c.mu.Unlock()

	if closed {
		t.Fatal("pressing q should ask for confirmation first, not quit immediately")
	}
	if mode != ModeConfirm {
		t.Fatalf("dispatchAction(actQuit) should enter ModeConfirm, got %v", mode)
	}
	if res.Detach {
		t.Fatal("quit should not also request a detach")
	}
}

// TestRebindingQuitStillAsksForConfirmation checks the new rebinding
// system and the quit-confirmation fix compose correctly: quit asking
// first isn't special-cased to the literal 'q' key, it's on
// dispatchAction's actQuit case, so a config "bind" moving quit to a
// different key must still go through confirmQuit exactly the same way.
func TestRebindingQuitStillAsksForConfirmation(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'K': "quit"})

	pressPrefixThen(c, tcell.KeyRune, 'K')

	c.mu.Lock()
	mode := c.mode
	closed := c.closed
	c.mu.Unlock()

	if closed {
		t.Fatal("a rebound quit key should still ask for confirmation, not quit immediately")
	}
	if mode != ModeConfirm {
		t.Fatalf("mode after pressing the rebound quit key = %v, want ModeConfirm", mode)
	}
}

// TestKillWindowConfirmationStillWorks is a regression check that
// generalizing handleConfirmKey to run whatever pendingConfirm holds
// (instead of always calling killWindow directly) didn't break the
// original confirmKillWindow flow it was built for.
func TestKillWindowConfirmationStillWorks(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.newWindow()
	windowsBefore := len(c.windows)
	c.confirmKillWindow()
	c.handleConfirmKey('y')
	windowsAfter := len(c.windows)
	c.mu.Unlock()

	if windowsAfter != windowsBefore-1 {
		t.Fatalf("confirmKillWindow + 'y' should still kill the window: had %d, now %d", windowsBefore, windowsAfter)
	}
}
