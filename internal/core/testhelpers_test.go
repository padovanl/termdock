package core

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
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

// testCoreSeq makes every newTestCore session name unique, including
// across repeated runs of the same test (go test -count=N) and multiple
// Cores within one test. Deriving the name from t.Name() alone is not
// enough: a Core's panes keep writing snapshots asynchronously off their
// pump goroutines (see persist.go) well after the test that made them
// has moved on, so a later Core created under the same name can find a
// half-torn-down snapshot from the previous one and silently *restore*
// it — e.g. coming up with a leftover window already named "deploy"
// instead of a single fresh "bash". That surfaced as a rare, genuinely
// confusing flake in the jump-picker filter test, where an extra
// restored window matched the query alongside the intended one.
var testCoreSeq atomic.Int64

// newTestCore creates a Core under a session name unique to this
// instance and registers cleanup for its panes.
func newTestCore(t *testing.T) *Core {
	t.Helper()
	name := fmt.Sprintf("test-%s-%d", t.Name(), testCoreSeq.Add(1))
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

// wheelMsg is a scroll event at the top-left corner — on an overlay,
// where the pointer happens to be doesn't matter.
func wheelMsg(button tcell.ButtonMask) proto.ClientMsg {
	return proto.ClientMsg{Kind: "mouse", MouseX: 0, MouseY: 0, MouseButtons: int32(button)}
}

func findPaneFrame(panes []proto.PaneFrame, id int) *proto.PaneFrame {
	for i := range panes {
		if panes[i].ID == id {
			return &panes[i]
		}
	}
	return nil
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
