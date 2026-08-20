package core

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestToggleLoggingStartsWritesAndStops(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	paneID := c.win().active.ID
	c.toggleLogging()
	msg1 := c.statusMsg
	c.mu.Unlock()

	const startPrefix = "logging pane to "
	if !strings.HasPrefix(msg1, startPrefix) {
		t.Fatalf("statusMsg after starting logging = %q, want a %q prefix", msg1, startPrefix)
	}
	path := strings.TrimPrefix(msg1, startPrefix)

	c.mu.Lock()
	p := c.panes[paneID]
	got, ok := p.LogPath()
	c.mu.Unlock()
	if !ok || got != path {
		t.Fatalf("pane.LogPath() = %q, %v; want %q, true", got, ok, path)
	}

	writeAndWaitEcho(t, c, paneID, "echo unique-toggle-log-marker")

	var data []byte
	found := waitFor(t, func() bool {
		var err error
		data, err = os.ReadFile(path)
		return err == nil && strings.Contains(string(data), "unique-toggle-log-marker")
	})
	if !found {
		t.Fatalf("log file at %q never contained the echoed command; last read: %q", path, string(data))
	}

	c.mu.Lock()
	c.toggleLogging()
	msg2 := c.statusMsg
	stillLogging, _ := p.LogPath()
	c.mu.Unlock()

	if want := "stopped logging (saved to " + path + ")"; msg2 != want {
		t.Fatalf("statusMsg after stopping logging = %q, want %q", msg2, want)
	}
	if stillLogging != "" {
		t.Fatalf("pane.LogPath() should be empty after stopping, got %q", stillLogging)
	}

	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile right after stop: %v", err)
	}
	writeAndWaitEcho(t, c, paneID, "echo after-stop-marker")
	time.Sleep(300 * time.Millisecond)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after more output: %v", err)
	}
	if len(after) != len(baseline) {
		t.Fatalf("log file grew after logging was stopped: had %d bytes, now %d", len(baseline), len(after))
	}
}

func TestLoggingTagsPaneTitleWithREC(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.toggleLogging()
	c.mu.Unlock()

	f := c.Frame()
	if len(f.Panes) != 1 || !strings.Contains(f.Panes[0].Title, "[REC]") {
		t.Fatalf("active pane's title should contain [REC] while logging, got %+v", f.Panes)
	}

	c.mu.Lock()
	c.toggleLogging()
	c.mu.Unlock()

	f2 := c.Frame()
	if len(f2.Panes) != 1 || strings.Contains(f2.Panes[0].Title, "[REC]") {
		t.Fatalf("active pane's title should NOT contain [REC] once logging stopped, got %+v", f2.Panes)
	}
}
