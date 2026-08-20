package core

import (
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/pane"
	"termdock/internal/proto"
)

// TestMain points internal/persist's $XDG_STATE_HOME at a throwaway
// temp directory for the whole test binary run, so every test in this
// package — via persistStateLocked, triggered by nearly every structural
// change (see persist.go) — reads and writes session snapshots there
// instead of the real ~/.local/state/termdock. That matters beyond tidy
// cleanup: pane.Close() detaches asynchronously (the pump goroutine
// notices EOF and calls handlePaneExit -> ... -> persistStateLocked on
// its own schedule), so a snapshot write can land *after* a test's own
// cleanup already ran — isolating the whole run to one directory that
// gets removed wholesale at the end catches that regardless of exactly
// when it happens, which per-test cleanup on its own can't guarantee.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "termdock-test-state-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run() // os.Exit below would skip a deferred cleanup, so run it explicitly first
	os.RemoveAll(dir)
	os.Exit(code)
}

// newTestCore creates a Core under a session name unique to the calling
// test and registers cleanup for its panes. The unique name still
// matters even with TestMain's isolation above: two tests sharing a
// session name within the same run would have the later one silently
// *restore* the earlier one's leftover windows/panes instead of starting
// fresh — an order-dependent flake that's easy to introduce by accident
// once a test creates more than one window or pane.
func newTestCore(t *testing.T) *Core {
	t.Helper()
	name := "test-" + t.Name()
	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { closeAllPanes(c) })
	return c
}

// closeAllPanes snapshots the panes to close while holding c.mu, then
// closes them outside the lock. It must not iterate c.panes unlocked: a
// pane's shell exiting on its own drives Pump -> handlePaneExit ->
// detachLeafIn -> ... -> c.mu.Lock() concurrently, off a pump goroutine,
// which can mutate c.panes at the same moment a cleanup func here ranges
// over it — an intermittent "concurrent map iteration and map write"
// crash caught by running the suite repeatedly. Closing happens outside
// the lock so it can't deadlock with handlePaneExit's own locking.
func closeAllPanes(c *Core) {
	c.mu.Lock()
	panes := make([]*pane.Pane, 0, len(c.panes))
	for _, p := range c.panes {
		panes = append(panes, p)
	}
	popup := c.popup
	c.mu.Unlock()

	for _, p := range panes {
		p.Close()
	}
	if popup != nil {
		popup.Close()
	}
}

// waitFor polls cond (which must not itself try to acquire c.mu — call
// it only from outside a locked section) until it's true or a few
// seconds pass, for asserting on state that changes asynchronously off a
// pane's pump goroutine (e.g. its shell exiting).
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func mouseMsg(x, y int) proto.ClientMsg {
	return proto.ClientMsg{Kind: "mouse", MouseX: x, MouseY: y, MouseButtons: int32(tcell.Button1)}
}

// setupNamedWindows creates a Core whose windows are named exactly names,
// in order — window 0 (already created by New) is renamed to names[0],
// then one newWindowOpts call per remaining name.
func setupNamedWindows(t *testing.T, names ...string) *Core {
	t.Helper()
	c := newTestCore(t)
	c.mu.Lock()
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune(names[0])
	c.confirmInput()
	for _, n := range names[1:] {
		c.newWindowOpts(n, "")
	}
	c.mu.Unlock()
	return c
}

func namesOf(c *Core) []string {
	var out []string
	for _, w := range c.windows {
		out = append(out, c.windowDisplayName(w))
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
