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

	"termdock/internal/config"
	"termdock/internal/proto"
)

// Run attaches to the session listening on sockPath and drives it until
// the user detaches, the session ends, or the connection drops.
func Run(sockPath string, cfg config.Config) error {
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("could not connect to session: %w", err)
	}
	defer conn.Close()

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

	enc := gob.NewEncoder(conn)
	dec := gob.NewDecoder(conn)

	w, h := screen.Size()
	if err := enc.Encode(proto.ClientMsg{Kind: "hello", Cols: w, Rows: h}); err != nil {
		return err
	}

	type serverEvent struct {
		msg proto.ServerMsg
		err error
	}
	serverCh := make(chan serverEvent, 8)
	go func() {
		for {
			var m proto.ServerMsg
			err := dec.Decode(&m)
			serverCh <- serverEvent{m, err}
			if err != nil {
				return
			}
		}
	}()

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

	var bye string
loop:
	for {
		select {
		case se := <-serverCh:
			if se.err != nil {
				bye = "connection to session lost"
				break loop
			}
			switch se.msg.Kind {
			case "frame":
				draw(screen, *se.msg.Frame, cfg)
			case "clipboard":
				writeClipboard(se.msg.Clipboard)
			case "bye":
				bye = se.msg.Bye
				break loop
			}

		case ev, ok := <-events:
			if !ok {
				break loop
			}
			if !forwardEvent(enc, ev) {
				bye = "connection to session lost"
				break loop
			}

		case <-sigCh:
			break loop
		}
	}

	finish()
	if bye != "" {
		fmt.Fprintln(os.Stderr, "termdock:", bye)
	}
	return nil
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
