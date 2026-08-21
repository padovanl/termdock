package server

import (
	"encoding/gob"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// TestConcurrentClientsStress hammers one live session from several
// clients at once — input, resizes, splits, mode changes, panes exiting
// on their own — while scripting commands arrive on their own
// connections. Everywhere else the tests drive the daemon one step at a
// time, which is exactly the shape of test that cannot catch a locking
// mistake; this is the one that can, and it earns its couple of seconds
// by being the only coverage of the concurrency the daemon actually sees.
//
// Worth running as `go test -race`: the assertions here only catch a
// deadlock or a crash, while the race detector is what turns this into a
// check on the locking itself.
func TestConcurrentClientsStress(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test; runs for a couple of seconds")
	}
	sock, kill := startSession(t, "zz-stress")
	defer kill()

	const clients = 3
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < clients; i++ {
		conn, enc, dec := dial(t, sock, i == 2) // the third attaches read-only
		wg.Add(2)
		go func() { // drain frames
			defer wg.Done()
			for {
				var m proto.ServerMsg
				if err := dec.Decode(&m); err != nil {
					return
				}
			}
		}()
		go func(seed int64) {
			defer wg.Done()
			defer conn.Close()
			rng := rand.New(rand.NewSource(seed))
			keys := []proto.ClientMsg{
				{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'v'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 's'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'o'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'z'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'g'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'C'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'w'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '['},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'x'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'c'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '!'},
				{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: ' '},
				{Kind: "key", KeyCode: int32(tcell.KeyEsc)},
				{Kind: "key", KeyCode: int32(tcell.KeyDown)},
				{Kind: "key", KeyCode: int32(tcell.KeyEnter)},
				{Kind: "mouse", MouseX: 3, MouseY: 3, MouseButtons: 1},
				{Kind: "mouse", MouseX: 9, MouseY: 5, MouseButtons: 1},
				{Kind: "mouse", MouseX: 9, MouseY: 5, MouseButtons: 0},
				{Kind: "mouse", MouseX: 4, MouseY: 4, MouseButtons: int32(tcell.WheelUp)},
			}
			for sent := 0; ; sent++ {
				select {
				case <-stop:
					return
				default:
				}
				// Periodically bail out to normal mode. A random walk can
				// otherwise park a client inside a modal that swallows
				// every key — the settings screen's in-place editor takes
				// each keystroke as text — and with all three clients
				// stuck there, no splits happen while the scripted
				// "exit"s keep closing panes, so the session ends and the
				// run measures nothing. This bounds how long any client
				// can sit in one without weakening what's being driven.
				if sent%9 == 8 {
					enc.Encode(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyEsc)})
					enc.Encode(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyEsc)})
				}
				m := keys[rng.Intn(len(keys))]
				if rng.Intn(12) == 0 {
					m = proto.ClientMsg{Kind: "resize", Cols: 10 + rng.Intn(120), Rows: 1 + rng.Intn(40)}
				}
				if enc.Encode(m) != nil {
					return
				}
				time.Sleep(time.Duration(rng.Intn(3)) * time.Millisecond)
			}
		}(int64(i + 1))
	}

	// Meanwhile, drive it from the scripting side and let panes exit.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			stressShot(sock, proto.ClientMsg{Kind: "split-window", WindowIdx: -1, CLIAxis: "v"})
			stressShot(sock, proto.ClientMsg{Kind: "list-panes", WindowIdx: -1})
			stressShot(sock, proto.ClientMsg{Kind: "send-keys", WindowIdx: -1, CLIText: "exit", CLIEnter: true})
			stressShot(sock, proto.ClientMsg{Kind: "query"})
			time.Sleep(5 * time.Millisecond)
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	alive, samples, maxPanes := 0, 0, 0
	for time.Now().Before(deadline) {
		if info, ok := Probe(sock); ok {
			alive++
			if info.PaneCount > maxPanes {
				maxPanes = info.PaneCount
			}
		}
		samples++
		time.Sleep(50 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	t.Logf("session answered %d/%d probes, peaked at %d panes", alive, samples, maxPanes)
	if alive*4 < samples {
		t.Errorf("the session was down for most of the run (%d/%d) — the stress barely exercised anything", alive, samples)
	}
	if maxPanes < 3 {
		t.Errorf("only ever saw %d panes; the stress isn't reaching the interesting states", maxPanes)
	}
}

func stressShot(sock string, msg proto.ClientMsg) {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if gob.NewEncoder(conn).Encode(msg) != nil {
		return
	}
	var reply proto.ServerMsg
	_ = gob.NewDecoder(conn).Decode(&reply)
}
