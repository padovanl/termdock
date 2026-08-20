package server

import (
	"encoding/gob"
	"net"
	"os"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/config"
	"termdock/internal/proto"
)

// TestMain isolates every session this package's tests spin up to
// throwaway temp directories for both the socket directory (Dir, driven
// by $XDG_RUNTIME_DIR) and session-persistence snapshots (driven by
// $XDG_STATE_HOME, written by core.Core as a side effect of nearly any
// structural change) — these tests start real daemons with real panes,
// and neither should ever touch the actual machine's runtime/state dirs.
func TestMain(m *testing.M) {
	runtimeDir, err := os.MkdirTemp("", "termdock-test-runtime-*")
	if err != nil {
		panic(err)
	}
	stateDir, err := os.MkdirTemp("", "termdock-test-state-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	os.Setenv("XDG_STATE_HOME", stateDir)
	code := m.Run() // os.Exit below would skip deferred cleanup, so run it explicitly first
	os.RemoveAll(runtimeDir)
	os.RemoveAll(stateDir)
	os.Exit(code)
}

// startSession starts a real daemon for name in the background and
// blocks until its socket answers, returning the socket path and a
// killSession func the test should defer.
func startSession(t *testing.T, name string) (sockPath string, kill func()) {
	t.Helper()
	sock, err := SocketPath(name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	done := make(chan struct{})
	go func() {
		Run(name, sock, config.Default())
		close(done)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := Probe(sock); ok {
			return sock, func() {
				Kill(sock)
				select {
				case <-done:
				case <-time.After(2 * time.Second):
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %q never came up", name)
	return "", nil
}

// dial connects to sock and sends a "hello", returning the connection
// and decoder ready to read frames/messages, and skipping past the
// server's initial frame reply.
func dial(t *testing.T, sock string, readOnly bool) (net.Conn, *gob.Encoder, *gob.Decoder) {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)
	if err := enc.Encode(proto.ClientMsg{Kind: "hello", Cols: 80, Rows: 24, ReadOnly: readOnly}); err != nil {
		t.Fatalf("hello: %v", err)
	}
	var first proto.ServerMsg
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("initial frame: %v", err)
	}
	if first.Kind != "frame" {
		t.Fatalf("expected an initial frame, got Kind=%q", first.Kind)
	}
	return conn, enc, dec
}

func sendKey(t *testing.T, enc *gob.Encoder, key tcell.Key, r rune) {
	t.Helper()
	if err := enc.Encode(proto.ClientMsg{Kind: "key", KeyCode: int32(key), KeyRune: r}); err != nil {
		t.Fatalf("send key: %v", err)
	}
}

// recvUntil reads messages until pred matches one, or the deadline
// passes — frames arrive continuously (the clock ticks, etc.), so a
// single Decode isn't enough to find a specific message.
func recvUntil(t *testing.T, dec *gob.Decoder, timeout time.Duration, pred func(proto.ServerMsg) bool) proto.ServerMsg {
	t.Helper()
	type result struct {
		msg proto.ServerMsg
		err error
	}
	ch := make(chan result, 1)
	go func() {
		for {
			var m proto.ServerMsg
			err := dec.Decode(&m)
			ch <- result{m, err}
			if err != nil || pred(m) {
				return
			}
		}
	}()
	deadline := time.After(timeout)
	for {
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatalf("recv: %v", r.err)
			}
			if pred(r.msg) {
				return r.msg
			}
		case <-deadline:
			t.Fatal("timed out waiting for the expected message")
		}
	}
}

func TestSwitchSessionEndToEnd(t *testing.T) {
	sockA, killA := startSession(t, "test-switch-a")
	defer killA()
	sockB, killB := startSession(t, "test-switch-b")
	defer killB()

	connA, encA, decA := dial(t, sockA, false)
	defer connA.Close()

	// Ctrl-B S opens the session picker; type enough of "test-switch-b"
	// to match only it (not test-switch-a), then Enter.
	sendKey(t, encA, tcell.KeyCtrlB, 0)
	sendKey(t, encA, tcell.KeyRune, 'S')
	for _, r := range "switch-b" {
		sendKey(t, encA, tcell.KeyRune, r)
	}
	sendKey(t, encA, tcell.KeyEnter, 0)

	msg := recvUntil(t, decA, 5*time.Second, func(m proto.ServerMsg) bool { return m.Kind == "switch" })
	if msg.SwitchTo != "test-switch-b" {
		t.Fatalf("SwitchTo = %q, want %q", msg.SwitchTo, "test-switch-b")
	}
	_ = sockB // only needed to keep session B alive for the picker to see
}

func TestReadOnlyClientInputIsDropped(t *testing.T) {
	sock, kill := startSession(t, "test-readonly-input")
	defer kill()

	conn, enc, _ := dial(t, sock, true)
	defer conn.Close()

	// Ctrl-B c would normally create a new window; a read-only client's
	// keys must never reach core.HandleClientMsg at all.
	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'c')

	// Give it a moment to (not) take effect, then check via a fresh,
	// ordinary probe that the session still has exactly its one
	// original pane.
	time.Sleep(200 * time.Millisecond)
	info, ok := Probe(sock)
	if !ok {
		t.Fatal("Probe failed")
	}
	if info.PaneCount != 1 {
		t.Fatalf("expected the read-only client's Ctrl-B c to be dropped (still 1 pane), got %d", info.PaneCount)
	}
}

func TestReadOnlyClientResizeIsDropped(t *testing.T) {
	sock, kill := startSession(t, "test-readonly-resize")
	defer kill()

	// A normal client establishes the session at 80x24 first.
	normalConn, normalEnc, _ := dial(t, sock, false)
	defer normalConn.Close()

	// A read-only observer with a *different* size must not be able to
	// resize the shared session out from under the normal client.
	roConn, roEnc, roDec := dial(t, sock, true)
	defer roConn.Close()
	if err := roEnc.Encode(proto.ClientMsg{Kind: "resize", Cols: 40, Rows: 10}); err != nil {
		t.Fatalf("resize: %v", err)
	}

	// A dropped resize doesn't itself trigger a broadcast, so nudge one
	// via the normal client (a keystroke forwarded to the pane produces
	// output, which marks the session dirty) and check the read-only
	// client's next frame still reports the session's real 80x24.
	sendKey(t, normalEnc, tcell.KeyRune, 'x')

	msg := recvUntil(t, roDec, 3*time.Second, func(m proto.ServerMsg) bool { return m.Kind == "frame" })
	if msg.Frame.Cols != 80 || msg.Frame.Rows != 24 {
		t.Fatalf("session size changed to %dx%d — a read-only client's resize should have been dropped", msg.Frame.Cols, msg.Frame.Rows)
	}
}
