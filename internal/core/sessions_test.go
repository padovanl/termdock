package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestSessionPickerFiltersAndReturnsSwitchResult(t *testing.T) {
	c := newTestCore(t)
	c.ListSessions = func() []string { return []string{"main", "deploy", "logs"} }

	c.mu.Lock()
	c.enterSessionPicker()
	if c.mode != ModeSessions {
		t.Fatalf("enterSessionPicker should set ModeSessions, got %v", c.mode)
	}
	for _, r := range "depl" {
		c.handleSessionsKey(tcell.KeyRune, r)
	}
	filtered := len(c.sessions.filtered)
	c.mu.Unlock()

	if filtered != 1 {
		t.Fatalf("query 'depl' should match exactly 'deploy', matched %d", filtered)
	}

	c.mu.Lock()
	res := c.handleSessionsKey(tcell.KeyEnter, 0)
	mode := c.mode
	c.mu.Unlock()

	if res.SwitchSession != "deploy" {
		t.Fatalf("expected Result.SwitchSession = %q, got %q", "deploy", res.SwitchSession)
	}
	if mode != ModeNormal {
		t.Fatalf("confirming should return to ModeNormal, got %v", mode)
	}
}

func TestSessionPickerEscCancelsWithoutSwitching(t *testing.T) {
	c := newTestCore(t)
	c.ListSessions = func() []string { return []string{"main", "deploy"} }

	c.mu.Lock()
	c.enterSessionPicker()
	res := c.handleSessionsKey(tcell.KeyEsc, 0)
	mode := c.mode
	c.mu.Unlock()

	if res.SwitchSession != "" {
		t.Fatalf("Esc must not request a switch, got %q", res.SwitchSession)
	}
	if mode != ModeNormal {
		t.Fatalf("Esc should return to ModeNormal, got %v", mode)
	}
}

func TestSessionPickerWithNoOtherSessionsIsANoop(t *testing.T) {
	c := newTestCore(t)
	c.ListSessions = func() []string { return nil }

	c.mu.Lock()
	c.enterSessionPicker()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("opening the session picker with nothing to switch to should stay in ModeNormal, got %v", mode)
	}
}

func TestSessionPickerUnavailableWithoutListSessions(t *testing.T) {
	c := newTestCore(t)
	// c.ListSessions left nil, as it is outside of server.Run wiring it up.

	c.mu.Lock()
	c.enterSessionPicker()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("without ListSessions wired up, entering the picker should be a no-op, got mode=%v", mode)
	}
}
