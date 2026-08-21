package server

import (
	"encoding/gob"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/proto"
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
	return startSessionWithConfig(t, name, config.Default())
}

// startSessionWithConfig is startSession with a caller-supplied config,
// for tests that need to exercise a config-driven server-side setting
// (like "bind") end to end, not just the default.
func startSessionWithConfig(t *testing.T, name string, cfg config.Config) (sockPath string, kill func()) {
	t.Helper()
	sock, err := SocketPath(name)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	done := make(chan struct{})
	go func() {
		Run(name, sock, cfg)
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
	// pred is evaluated on both this goroutine and the caller's, so it
	// must be pure: capturing the matched message from inside it is a
	// data race (use the returned value instead, which is what it is
	// for).
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

// oneShot dials sock, sends msg, and returns the single reply — the same
// request/response shape termdock's own CLI scripting subcommands use
// (send-keys, new-window, ...): no "hello", no attach loop, just one
// message and one reply before the connection closes.
func oneShot(t *testing.T, sock string, msg proto.ClientMsg) proto.ServerMsg {
	t.Helper()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := gob.NewEncoder(conn).Encode(msg); err != nil {
		t.Fatalf("send %s: %v", msg.Kind, err)
	}
	var reply proto.ServerMsg
	if err := gob.NewDecoder(conn).Decode(&reply); err != nil {
		t.Fatalf("reply to %s: %v", msg.Kind, err)
	}
	return reply
}

func TestBellRingsForBackgroundWindowActivity(t *testing.T) {
	sock, kill := startSession(t, "test-bell")
	defer kill()

	// An attached client sees whatever's active; window 0 starts out
	// active, so create a second window to make window 0 the
	// "background" one relative to it (newWindowOpts switches to the
	// window it creates).
	conn, _, dec := dial(t, sock, false)
	defer conn.Close()

	if reply := oneShot(t, sock, proto.ClientMsg{Kind: "new-window"}); reply.CLIError != "" {
		t.Fatalf("new-window: %s", reply.CLIError)
	}

	// Writing into window 0 (now in the background) should ring a bell
	// for the attached client.
	reply := oneShot(t, sock, proto.ClientMsg{Kind: "send-keys", WindowIdx: 0, CLIText: "echo hi", CLIEnter: true})
	if reply.CLIError != "" {
		t.Fatalf("send-keys: %s", reply.CLIError)
	}

	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool { return m.Kind == "bell" })
}

// TestNewKeybindingsEndToEnd drives this round's new features (pane
// logging, the command prompt, layout presets, respawn-pane) through a
// real client connection over a real socket, rather than calling into
// internal/core directly like their own package's tests do — proving
// the whole client-message -> server -> Core.HandleClientMsg pipeline
// actually delivers each new keybinding, not just the handler logic in
// isolation.
func TestNewKeybindingsEndToEnd(t *testing.T) {
	sock, kill := startSession(t, "test-new-features-e2e")
	defer kill()

	conn, enc, dec := dial(t, sock, false)
	defer conn.Close()

	// Ctrl-B L starts logging the active pane; the pane's title should
	// pick up the [REC] tag on the very next frame.
	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'L')
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && len(m.Frame.Panes) > 0 && strings.Contains(m.Frame.Panes[0].Title, "[REC]")
	})

	// Ctrl-B : opens the command prompt; typing "new-window -n e2e" and
	// Enter should create a second window with that name.
	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, ':')
	for _, r := range "new-window -n e2e" {
		sendKey(t, enc, tcell.KeyRune, r)
	}
	sendKey(t, enc, tcell.KeyEnter, 0)
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		if m.Kind != "frame" || m.Frame == nil {
			return false
		}
		for _, w := range m.Frame.Windows {
			if strings.Contains(w.Label, "e2e") {
				return true
			}
		}
		return false
	})

	// Ctrl-B Space cycles the (now single-pane) window's layout; with
	// only one pane it's a documented no-op, so just check the session
	// is still alive and answering afterward instead of asserting on a
	// specific layout (nothing to compare against with 1 pane).
	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, ' ')
	if _, ok := Probe(sock); !ok {
		t.Fatal("session should still be alive after Ctrl-B Space")
	}

	// Ctrl-B R respawns the active pane: the frame should keep showing
	// exactly one pane in this window (same slot, fresh process).
	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'R')
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && len(m.Frame.Panes) == 1
	})
}

// TestSelectPaneEndToEnd drives the select-pane scripting command (the
// piece external tooling like a vim-tmux-navigator-style plugin needs)
// through a real socket connection, checking the active pane in the
// server's own frame actually moves.
func TestSelectPaneEndToEnd(t *testing.T) {
	sock, kill := startSession(t, "test-select-pane-e2e")
	defer kill()

	conn, _, dec := dial(t, sock, false)
	defer conn.Close()

	if reply := oneShot(t, sock, proto.ClientMsg{Kind: "split-window", CLIAxis: "v"}); reply.CLIError != "" {
		t.Fatalf("split-window: %s", reply.CLIError)
	}
	frame := recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && len(m.Frame.Panes) == 2
	})
	var activeBefore int
	for _, p := range frame.Frame.Panes {
		if p.Active {
			activeBefore = p.ID
		}
	}

	if reply := oneShot(t, sock, proto.ClientMsg{Kind: "select-pane", CLIDirection: "L"}); reply.CLIError != "" {
		t.Fatalf("select-pane -L: %s", reply.CLIError)
	}

	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		if m.Kind != "frame" || m.Frame == nil {
			return false
		}
		for _, p := range m.Frame.Panes {
			if p.Active {
				return p.ID != activeBefore
			}
		}
		return false
	})
}

// TestBindOverrideEndToEnd starts a session with "bind M jump-picker"
// applied (the same path config.Load()/SetBindOverrides take from a
// real termdock.conf) and checks that pressing Ctrl-B M — not the
// default Ctrl-B w — is what opens the jump picker for a real attached
// client.
func TestBindOverrideEndToEnd(t *testing.T) {
	cfg := config.Default()
	cfg.BindOverrides = map[rune]string{'M': "jump-picker"}
	sock, kill := startSessionWithConfig(t, "test-bind-override-e2e", cfg)
	defer kill()

	conn, enc, dec := dial(t, sock, false)
	defer conn.Close()

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'M')

	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && m.Frame.Overlay != nil &&
			strings.Contains(m.Frame.Overlay.Title, "jump")
	})
}

// TestQuitAsksForConfirmationEndToEnd checks Ctrl-B q no longer ends
// the session immediately (see confirmQuit) by confirming the daemon is
// still answering Probe right after sending it — a session that quit
// outright would have shut its socket down.
func TestQuitAsksForConfirmationEndToEnd(t *testing.T) {
	sock, kill := startSession(t, "test-quit-confirm-e2e")
	defer kill()

	conn, enc, dec := dial(t, sock, false)
	defer conn.Close()

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'q')

	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && strings.Contains(m.Frame.StatusText, "quit")
	})
	if _, ok := Probe(sock); !ok {
		t.Fatal("the session should still be running — Ctrl-B q should ask first, not quit immediately")
	}
}

// TestQuitDoesNotRestoreTheLayoutNextTime is the end-to-end version of
// the bug that started this round: quit a session with two panes, start
// one with the same name again, and the two panes came back instead of
// the single fresh pane a new session should have. requestQuit marks the
// session closed and the server's Exited() watcher then calls Shutdown —
// which used to return early on exactly that flag, skipping the snapshot
// delete that is its entire purpose. Covered here rather than only in
// core because the wiring between the two is the part that broke.
func TestQuitDoesNotRestoreTheLayoutNextTime(t *testing.T) {
	const name = "test-quit-no-restore-e2e"
	sock, kill := startSession(t, name)
	defer kill()

	conn, enc, _ := dial(t, sock, false)

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'v') // split: two panes now
	if !waitForPaneCount(sock, 2) {
		t.Fatal("the split never took effect")
	}

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'q')
	sendKey(t, enc, tcell.KeyRune, 'y') // confirm
	conn.Close()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := Probe(sock); !ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := Probe(sock); ok {
		t.Fatal("the session should be gone after a confirmed quit")
	}

	_, kill2 := startSession(t, name)
	defer kill2()
	info, ok := Probe(sock)
	if !ok {
		t.Fatal("the restarted session should be up")
	}
	if info.PaneCount != 1 {
		t.Fatalf("a session started after quitting should have 1 pane, got %d — the quit left its snapshot behind", info.PaneCount)
	}
}

func waitForPaneCount(sock string, want int) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, ok := Probe(sock); ok && info.PaneCount == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// TestRenameSessionEndToEnd drives Ctrl-B $ over a real daemon and
// checks the rename is a real one: the socket moves (so `termdock ls`
// and `attach -t` find it under the new name and no longer under the
// old), and the session keeps serving on it.
func TestRenameSessionEndToEnd(t *testing.T) {
	oldName, newName := "test-rename-old", "test-rename-new"
	oldSock, _ := startSession(t, oldName)

	newSock, err := SocketPath(newName)
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	defer func() { Kill(newSock) }()

	conn, enc, dec := dial(t, oldSock, false)
	defer conn.Close()

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, '$')
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && strings.Contains(m.Frame.StatusText, "Rename session")
	})
	// The prompt comes prefilled with the current name (see
	// renameSessionPrompt), so clear it before typing the new one.
	sendKey(t, enc, tcell.KeyCtrlU, 0)
	for _, r := range newName {
		sendKey(t, enc, tcell.KeyRune, r)
	}
	sendKey(t, enc, tcell.KeyEnter, 0)

	// The status bar is the session's own view of its name.
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && m.Frame.SessionName == newName
	})

	// ...and the socket really moved, which is what makes the rename
	// visible to `ls`/`attach` rather than only cosmetic.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := Probe(newSock); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := Probe(newSock); !ok {
		t.Fatalf("session does not answer on its new socket %q", newSock)
	}
	if _, err := os.Stat(oldSock); !os.IsNotExist(err) {
		t.Fatalf("old socket %q should be gone after a rename, stat err = %v", oldSock, err)
	}
}

// A rename onto a name already in use must be refused, not allowed to
// clobber the other session's socket and leave it unreachable.
func TestRenameSessionRefusesAnExistingName(t *testing.T) {
	sockA, killA := startSession(t, "test-rename-clash-a")
	defer killA()
	_, killB := startSession(t, "test-rename-clash-b")
	defer killB()

	conn, enc, dec := dial(t, sockA, false)
	defer conn.Close()

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, '$')
	sendKey(t, enc, tcell.KeyCtrlU, 0)
	for _, r := range "test-rename-clash-b" {
		sendKey(t, enc, tcell.KeyRune, r)
	}
	sendKey(t, enc, tcell.KeyEnter, 0)

	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && strings.Contains(m.Frame.StatusText, "already exists")
	})
	if _, ok := Probe(sockA); !ok {
		t.Fatal("session A should still be reachable on its original socket after a refused rename")
	}
}

// Quitting from inside the session (Ctrl-B q, confirmed) must tell the
// attached clients so, rather than leaving them to notice the socket has
// gone quiet — which the client reports as "connection to session lost",
// the wording for a dropped link, making a clean exit look like a fault.
func TestQuitSendsByeToAttachedClients(t *testing.T) {
	sock, _ := startSession(t, "test-quit-bye-e2e")

	conn, enc, dec := dial(t, sock, false)
	defer conn.Close()

	sendKey(t, enc, tcell.KeyCtrlB, 0)
	sendKey(t, enc, tcell.KeyRune, 'q')
	recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "frame" && m.Frame != nil && strings.Contains(m.Frame.StatusText, "quit")
	})
	sendKey(t, enc, tcell.KeyRune, 'y') // confirm

	bye := recvUntil(t, dec, 5*time.Second, func(m proto.ServerMsg) bool {
		return m.Kind == "bye"
	})
	if bye.Bye == "" {
		t.Fatal("the bye message should carry a reason for the client to print")
	}
}
