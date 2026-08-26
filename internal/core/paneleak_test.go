package core

import (
	"runtime"
	"testing"
	"time"

	"github.com/padovanl/termdock/internal/layout"
)

// A session is meant to stay up for weeks, so a goroutine left behind by
// every pane that closes is a slow leak in a process that never
// restarts. Each pane has a pump goroutine blocked reading its pty, and
// what unblocks it is the pty being closed.
//
// It has to close panes the way the program does, through killActive:
// detachLeafIn only takes a pane out of the layout tree and is used by
// break-pane and move-to-window, where the pane is deliberately kept
// alive. Closing them that way in a test leaks a goroutine per pane and
// looks exactly like the bug this is looking for.
func TestPanesDoNotLeakGoroutines(t *testing.T) {
	c := newTestCore(t)
	t.Cleanup(func() { closeAllPanes(c) })

	settle := func() int {
		for i := 0; i < 60; i++ {
			runtime.GC()
			time.Sleep(20 * time.Millisecond)
		}
		return runtime.NumGoroutine()
	}

	// Warm up: the first few panes create shared machinery.
	for i := 0; i < 3; i++ {
		c.mu.Lock()
		c.doSplit(layout.Vertical)
		c.mu.Unlock()
	}
	c.mu.Lock()
	for len(layout.Leaves(c.win().root)) > 1 {
		c.setActive(layout.Leaves(c.win().root)[1])
		c.killActive()
	}
	c.mu.Unlock()
	base := settle()

	for round := 0; round < 12; round++ {
		c.mu.Lock()
		c.doSplit(layout.Vertical)
		c.doSplit(layout.Horizontal)
		c.mu.Unlock()
		c.mu.Lock()
		for len(layout.Leaves(c.win().root)) > 1 {
			c.setActive(layout.Leaves(c.win().root)[1])
			c.killActive()
		}
		c.mu.Unlock()
	}

	after := settle()
	t.Logf("goroutine: %d prima, %d dopo 24 pane aperti e chiusi", base, after)
	if after > base+4 {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		t.Fatalf("goroutines went %d -> %d after opening and closing 24 panes\n%s", base, after, buf[:n])
	}
}
