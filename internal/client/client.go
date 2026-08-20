// Package client is the thin, disposable half of termdock: it owns the
// real terminal (via tcell) and a socket to a server session, forwards
// input, and paints whatever proto.Frame the server sends. It keeps no
// session state of its own, which is what makes closing it (detach,
// closing the terminal window, a dropped SSH link) harmless to the panes
// running in the server.
package client

import (
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/server"
)

// Run attaches to the session listening on sockPath and drives it until
// the user detaches, the session ends, or the connection drops.
// readOnly attaches as a view-only observer (Ctrl-B S is server-driven,
// so switching sessions works the same either way; every other keypress
// and mouse event is simply dropped server-side — see internal/server).
//
// A session switch (Ctrl-B S) reconnects to the new session's socket
// without tearing down and reinitializing the tcell screen — a client
// mid-multi-session-switch shouldn't have to eat a full terminal
// flash-and-redraw on every hop the way exiting and relaunching would
// cause.
func Run(sockPath string, cfg config.Config, readOnly bool) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	screen.SetStyle(tcell.StyleDefault)
	if cfg.Mouse {
		screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
	}
	screen.HideCursor()
	finished := false
	finish := func() {
		if !finished {
			finished = true
			screen.Fini()
		}
	}
	defer finish()

	events := make(chan tcell.Event, 16)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				close(events)
				return
			}
			events <- ev
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	for {
		bye, switchTo, err := attachOnce(screen, sockPath, cfg, readOnly, events, sigCh)
		if err != nil {
			return err
		}
		if switchTo == "" {
			finish()
			if bye != "" {
				fmt.Fprintln(os.Stderr, "termdock:", bye)
			}
			return nil
		}
		next, err := server.SocketPath(switchTo)
		if err != nil {
			finish()
			fmt.Fprintln(os.Stderr, "termdock:", err)
			return nil
		}
		sockPath = next
		screen.Clear() // the new session's first frame hasn't arrived yet; don't sit on the old one's
		screen.Show()
	}
}

// attachOnce drives a single socket connection until it ends, for
// whatever reason: a real detach/quit (bye != ""), a session switch
// (switchTo != ""), the connection dropping, or a signal. The screen and
// its event stream are owned by the caller and outlive any one
// connection, since a session switch reuses them rather than
// reinitializing the terminal.
func attachOnce(screen tcell.Screen, sockPath string, cfg config.Config, readOnly bool, events <-chan tcell.Event, sigCh <-chan os.Signal) (bye, switchTo string, err error) {
	conn, dialErr := net.Dial("unix", sockPath)
	if dialErr != nil {
		return "", "", fmt.Errorf("could not connect to session: %w", dialErr)
	}
	defer conn.Close()

	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)

	w, h := screen.Size()
	if err := enc.Encode(proto.ClientMsg{Kind: "hello", Cols: w, Rows: h, ReadOnly: readOnly}); err != nil {
		return "", "", err
	}

	type serverEvent struct {
		msg proto.ServerMsg
		err error
	}
	serverCh := make(chan serverEvent, 8)
	go func() {
		for {
			var m proto.ServerMsg
			decErr := dec.Decode(&m)
			serverCh <- serverEvent{m, decErr}
			if decErr != nil {
				return
			}
		}
	}()

	for {
		select {
		case se := <-serverCh:
			if se.err != nil {
				return "connection to session lost", "", nil
			}
			switch se.msg.Kind {
			case "frame":
				draw(screen, *se.msg.Frame, cfg)
			case "clipboard":
				writeClipboard(se.msg.Clipboard)
			case "bell":
				ringBell()
			case "switch":
				return "", se.msg.SwitchTo, nil
			case "bye":
				return se.msg.Bye, "", nil
			}

		case ev, ok := <-events:
			if !ok {
				return "", "", nil
			}
			if !forwardEvent(enc, ev) {
				return "connection to session lost", "", nil
			}

		case <-sigCh:
			return "", "", nil
		}
	}
}

func forwardEvent(enc *gob.Encoder, ev tcell.Event) bool {
	var m proto.ClientMsg
	switch e := ev.(type) {
	case *tcell.EventResize:
		w, h := e.Size()
		m = proto.ClientMsg{Kind: "resize", Cols: w, Rows: h}
	case *tcell.EventKey:
		m = proto.ClientMsg{Kind: "key", KeyRune: e.Rune(), KeyCode: int32(e.Key()), KeyMod: int32(e.Modifiers())}
	case *tcell.EventMouse:
		x, y := e.Position()
		m = proto.ClientMsg{Kind: "mouse", MouseX: x, MouseY: y, MouseButtons: int32(e.Buttons()), MouseMod: int32(e.Modifiers())}
	default:
		return true
	}
	return enc.Encode(m) == nil
}

// writeClipboard pushes text to the real terminal's system clipboard via
// OSC52, bypassing tcell (which has no API for arbitrary escape
// sequences) by writing straight to stdout.
func writeClipboard(text string) {
	os.Stdout.WriteString("\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a")
}

// ringBell passes a background window's new activity on as a real
// terminal BEL, straight to the real terminal rather than through tcell
// (which has no API for it) — whether that's audible, a visual flash, or
// nothing at all is entirely the user's own terminal bell setting, same
// as it would be for any other program.
func ringBell() {
	os.Stdout.WriteString("\a")
}
