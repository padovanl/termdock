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
	enableTrueColorForTheme(cfg)
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
	recolored := setTerminalColors(cfg)
	finished := false
	finish := func() {
		if !finished {
			finished = true
			screen.Fini()
			if recolored {
				resetTerminalColors()
			}
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
		bye, switchTo, err := attachOnce(screen, sockPath, cfg, readOnly, events, sigCh, &recolored)
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
func attachOnce(screen tcell.Screen, sockPath string, cfg config.Config, readOnly bool, events <-chan tcell.Event, sigCh <-chan os.Signal, recolored *bool) (bye, switchTo string, err error) {
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
				// A session where someone has run ":set" sends its own
				// look-and-feel settings with every frame; until then this
				// is nil and the client keeps rendering with its own
				// config file, so per-client themes still work.
				if s := se.msg.Frame.Settings; s != nil {
					cfg = adoptSettings(screen, cfg, *s, recolored)
				}
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

// adoptSettings folds a session's own look-and-feel settings into cfg
// and makes the ones that aren't just drawing — the terminal's own
// background/foreground, and whether the mouse is grabbed at all — take
// effect immediately. A no-op when nothing actually differs, which is the
// common case: these arrive on every single frame, and re-emitting OSC
// colour sequences dozens of times a second would make some terminals
// flicker for no reason.
func adoptSettings(screen tcell.Screen, cfg config.Config, s proto.ClientSettings, recolored *bool) config.Config {
	next := cfg
	next.Mouse = s.Mouse
	next.StatusBG = tcell.Color(s.StatusBG)
	next.StatusFG = tcell.Color(s.StatusFG)
	next.PaneActiveBG = tcell.Color(s.PaneActiveBG)
	next.PaneBG = tcell.Color(s.PaneBG)
	next.PaneFG = tcell.Color(s.PaneFG)
	if next.Mouse == cfg.Mouse && next.StatusBG == cfg.StatusBG && next.StatusFG == cfg.StatusFG &&
		next.PaneActiveBG == cfg.PaneActiveBG && next.PaneBG == cfg.PaneBG && next.PaneFG == cfg.PaneFG {
		return cfg // unchanged, and these arrive with every frame
	}
	if next.Mouse != cfg.Mouse {
		if next.Mouse {
			screen.EnableMouse(tcell.MouseButtonEvents | tcell.MouseDragEvents)
		} else {
			screen.DisableMouse()
		}
	}
	if next.PaneBG != cfg.PaneBG || next.PaneFG != cfg.PaneFG {
		if setTerminalColors(next) {
			*recolored = true
		} else if *recolored {
			// Back to "your terminal's own colours": undo ours rather
			// than leaving the last theme's painted on.
			resetTerminalColors()
			*recolored = false
		}
	}
	return next
}

// writeClipboard pushes text to the real terminal's system clipboard via
// OSC52, bypassing tcell (which has no API for arbitrary escape
// sequences) by writing straight to stdout.
// enableTrueColorForTheme opts tcell into 24-bit colour when a theme is
// in play, unless the environment has already said something about it.
//
// Without this the theme's colours arrive by two different routes that
// disagree. tcell only emits 24-bit sequences if the terminfo entry
// advertises RGB, and the stock xterm-256color entry does not — so it
// quantises every theme colour to the nearest of 256 palette slots.
// setTerminalColors, meanwhile, hands the emulator the exact hex over
// OSC 11. The result was a session whose padding was the real colour
// and whose cells were an approximation of it: two almost-but-not-quite
// matching darks, with a visible seam at the pane borders.
//
// TCELL_TRUECOLOR is tcell's own opt-in for exactly this. An existing
// TCELL_TRUECOLOR or COLORTERM is left alone: both are the user (or
// their terminal) having already decided, and "TCELL_TRUECOLOR=disable"
// stays the way to keep the old quantised behaviour.
func enableTrueColorForTheme(cfg config.Config) {
	if _, themed := oscColor(cfg.PaneBG); !themed {
		return
	}
	if os.Getenv("TCELL_TRUECOLOR") != "" || os.Getenv("COLORTERM") != "" {
		return
	}
	os.Setenv("TCELL_TRUECOLOR", "enable")
}

// setTerminalColors asks the terminal emulator itself to adopt the
// theme's pane colours, via OSC 10 (default foreground) and OSC 11
// (default background). Reports whether it changed anything, so the
// caller knows whether to undo it.
//
// Painting cells can only ever reach the character grid, and a terminal
// emulator typically draws a few pixels of padding around that grid in
// its own background colour — so a fully themed termdock still sat in a
// thin frame of the emulator's background, which is exactly what the
// theme was meant to replace. OSC 11 is the only way to reach it, being
// a request to the emulator rather than something drawn.
//
// Written straight to stdout, bypassing tcell (which has no API for
// arbitrary escape sequences), the same way writeClipboard does for
// OSC 52.
func setTerminalColors(cfg config.Config) bool {
	seq := ""
	if hex, ok := oscColor(cfg.PaneFG); ok {
		seq += "\x1b]10;" + hex + "\a"
	}
	if hex, ok := oscColor(cfg.PaneBG); ok {
		seq += "\x1b]11;" + hex + "\a"
	}
	if seq == "" {
		return false
	}
	os.Stdout.WriteString(seq)
	return true
}

// resetTerminalColors puts the emulator's own default foreground and
// background back (OSC 110/111), so quitting or detaching doesn't leave
// the terminal wearing termdock's theme. Called after screen.Fini(), so
// it lands on a terminal tcell has already restored.
func resetTerminalColors() {
	os.Stdout.WriteString("\x1b]110\a\x1b]111\a")
}

// oscColor renders a colour as the "#rrggbb" OSC 10/11 wants, reporting
// false for ColorDefault (and anything else with no RGB value), which
// means "leave the emulator's own alone" — the no-theme default.
func oscColor(c tcell.Color) (string, bool) {
	h := c.Hex()
	if h < 0 {
		return "", false
	}
	return fmt.Sprintf("#%06x", h), true
}

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
