package pane

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewInDirSetsCwd(t *testing.T) {
	dir, err := os.MkdirTemp("", "termdock-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	// Resolve symlinks (e.g. /tmp -> /private/tmp on macOS, or WSL's
	// mount quirks) so the comparison below isn't fooled by a path that
	// refers to the same directory but isn't byte-identical.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	p, err := NewInDir(NextID(), 80, 24, dir)
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}
	defer p.Close()

	got := p.Cwd()
	if got == "" {
		t.Skip("Cwd() returned \"\" — likely running on a platform without /proc (see cwd_other.go)")
	}
	if got != real {
		t.Fatalf("Cwd() = %q, want %q", got, real)
	}
}

func TestLoggingCapturesOutputAndStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pane.log")

	p, err := New(NextID(), 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()
	// Nothing reads the pty (and so nothing feeds the logging tee — see
	// loggingReader in pane.go) without Pump running, the same as in
	// production (core.startPump); a no-op onUpdate/onExit is enough
	// here since this test only cares about the log file, not the
	// terminal grid.
	go p.Pump(func() {}, func() {})

	if err := p.StartLogging(path); err != nil {
		t.Fatalf("StartLogging: %v", err)
	}
	if got, ok := p.LogPath(); !ok || got != path {
		t.Fatalf("LogPath() = %q, %v; want %q, true", got, ok, path)
	}

	p.Write([]byte("echo unique-log-marker\r"))

	var data []byte
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "unique-log-marker") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(string(data), "unique-log-marker") {
		t.Fatalf("log file at %q never contained the echoed command; last read: %q", path, string(data))
	}

	stoppedPath, ok := p.StopLogging()
	if !ok || stoppedPath != path {
		t.Fatalf("StopLogging() = %q, %v; want %q, true", stoppedPath, ok, path)
	}
	if _, ok := p.LogPath(); ok {
		t.Fatal("LogPath should report not-logging after StopLogging")
	}

	// Nothing written after StopLogging returns can land in the file:
	// StartLogging/StopLogging/loggingReader.Read all serialize on the
	// same logMu, so the swap-to-nil above already happened-before any
	// write the pump goroutine could still be mid-Read on.
	baseline, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile right after stop: %v", err)
	}
	p.Write([]byte("echo after-stop-marker\r"))
	time.Sleep(300 * time.Millisecond)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile after more output: %v", err)
	}
	if len(after) != len(baseline) {
		t.Fatalf("log file grew after StopLogging: had %d bytes, now %d", len(baseline), len(after))
	}
}

func TestStopLoggingWithoutStartingIsANoop(t *testing.T) {
	p, err := New(NextID(), 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer p.Close()

	if path, ok := p.StopLogging(); ok || path != "" {
		t.Fatalf("StopLogging with nothing active: got %q, %v; want \"\", false", path, ok)
	}
}

func TestClosePaneStopsLoggingWithoutHanging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pane.log")

	p, err := New(NextID(), 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := p.StartLogging(path); err != nil {
		t.Fatalf("StartLogging: %v", err)
	}
	p.Close() // must not hang or panic while a log file is still open
}
