// Package server is the persistent daemon side of termdock: it owns one
// session's core.Core and lets any number of clients attach, detach and
// reattach to it over a unix socket, which is what keeps panes alive
// across terminal disconnects.
package server

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/core"
	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/persist"
	"github.com/padovanl/termdock/internal/proto"
)

// Run starts the daemon for a session and blocks until it shuts down
// (either the last pane exits, or a client sends a kill message).
func Run(name, sockPath string, cfg config.Config) error {
	// Checked up front, because getting it wrong takes the whole session
	// down: the shell is what every pane runs, so a path that isn't there
	// means core.New can't create even the first one and the daemon exits
	// before it ever listens. Left to fail on its own it surfaces as
	// "fork/exec /bin/zsh: no such file or directory", which says nothing
	// about where that path came from or what to do about it.
	if err := config.CheckShell(cfg.Shell); err != nil {
		return err
	}
	// Applies to every pane this process creates; must happen before
	// core.New spawns the first one.
	pane.SetDefaults(cfg.Shell, cfg.HistoryLimit)

	c, err := core.New(name, 80, 24)
	if err != nil {
		return err
	}
	// One call rather than a setter each: the session keeps the whole
	// config so ":set" can change any of it later (see internal/core's
	// settings.go). Bind overrides stay separate — they're additive, one
	// line per key, rather than a single value to replace.
	c.ApplyConfig(cfg)
	c.SetBindOverrides(cfg.BindOverrides)
	// core deliberately doesn't import server (server already imports
	// core; Go disallows the cycle), so it can't discover sibling
	// sessions itself — supplied here instead, for Ctrl-B S.
	c.ListSessions = func() []string {
		infos, err := List()
		if err != nil {
			return nil
		}
		var names []string
		for _, info := range infos {
			if info.Name != name {
				names = append(names, info.Name)
			}
		}
		return names
	}

	os.Remove(sockPath) // clear a stale socket from a previous, crashed run
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	os.Chmod(sockPath, 0600)

	// The session's name — and so its socket path — can change while it
	// runs (Ctrl-B $), so anything that touches either afterwards reads
	// them from here under liveMu rather than closing over the arguments.
	var (
		liveMu   sync.Mutex
		liveName = name
		liveSock = sockPath
	)
	c.RenameSession = func(newName string) error {
		if err := ValidateSessionName(newName); err != nil {
			return err
		}
		newSock, err := SocketPath(newName)
		if err != nil {
			return err
		}
		liveMu.Lock()
		defer liveMu.Unlock()
		if newSock == liveSock {
			return nil
		}
		// Renaming onto a live session would leave two daemons fighting
		// over one socket path, and whichever lost would be unreachable.
		if _, err := os.Stat(newSock); err == nil {
			return fmt.Errorf("a session named %q already exists", newName)
		}
		if err := os.Rename(liveSock, newSock); err != nil {
			return err
		}
		// Best-effort tidying, neither worth failing the rename over: the
		// daemon's log keeps being written through an already-open fd
		// regardless of what the file is called, and a stale snapshot is
		// only ever read by name (core writes a fresh one under the new
		// name right after this returns).
		if dir, derr := Dir(); derr == nil {
			os.Rename(filepath.Join(dir, liveName+".log"), filepath.Join(dir, newName+".log"))
		}
		persist.Delete(liveName)
		liveName, liveSock = newName, newSock
		return nil
	}

	s := &Session{
		core: c,
		ln:   ln,
	}

	shutdown := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			// Tell whoever is watching *why* the session is going away,
			// before the socket disappears under them. Without this an
			// ordinary Ctrl-B q ended with the client reporting
			// "connection to session lost" — the wording for a dropped
			// link — because a deliberate shutdown and a crashed daemon
			// looked identical from the far end: the connection simply
			// stopped answering.
			s.byeAll(fmt.Sprintf("🐳 session %q ended — all its panes are closed", s.core.Name()))
			c.Shutdown()
			ln.Close()
			liveMu.Lock()
			os.Remove(liveSock) // not sockPath: the session may have been renamed
			liveMu.Unlock()
			close(shutdown)
		})
	}

	go func() {
		<-c.Exited()
		stop()
	}()

	go s.broadcastLoop()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.handleConn(conn, stop)
		}
	}()

	<-shutdown
	return nil
}

// Session is one running daemon's state: its Core plus the set of
// currently attached clients.
type Session struct {
	core *core.Core
	ln   net.Listener

	mu      sync.Mutex
	clients map[*clientConn]struct{}
}

type clientConn struct {
	conn     net.Conn
	enc      *gob.Encoder
	mu       sync.Mutex // guards enc.Encode, called from both the broadcaster and this client's own handler
	readOnly bool // an observer: sees every frame, but its key/mouse/resize input is dropped (see handleConn)
}

func (cc *clientConn) send(m proto.ServerMsg) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	return cc.enc.Encode(m)
}

func (s *Session) addClient(cc *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clients == nil {
		s.clients = map[*clientConn]struct{}{}
	}
	s.clients[cc] = struct{}{}
}

func (s *Session) removeClient(cc *clientConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, cc)
}

func (s *Session) broadcastLoop() {
	debounce := time.NewTicker(16 * time.Millisecond)
	defer debounce.Stop()
	// Keeps the status bar's clock moving even when nothing else in the
	// session changes; frequent enough that it never looks stale, rare
	// enough it's not worth debouncing.
	clock := time.NewTicker(15 * time.Second)
	defer clock.Stop()
	// Session-structure changes (split, close, rename, ...) already
	// trigger an immediate snapshot; this periodic one exists only to
	// catch a pane's plain `cd` drifting its working directory away from
	// what was last saved, which isn't a layout change and so wouldn't
	// otherwise be noticed. See internal/persist.
	persistTick := time.NewTicker(30 * time.Second)
	defer persistTick.Stop()
	for {
		select {
		case <-s.core.Dirty():
			<-debounce.C // coalesce bursts into one frame per tick
			s.broadcast()
		case <-clock.C:
			s.broadcast()
		case <-persistTick.C:
			s.core.PersistState()
		case <-s.core.Bell():
			s.broadcastBell()
		case <-s.core.Exited():
			return
		}
	}
}

// broadcastBell passes a background window's first bit of new activity
// on to every attached client as a real terminal bell, on top of the
// tab strip's passive "!" marker — see Core.Bell.
func (s *Session) broadcastBell() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for cc := range s.clients {
		if err := cc.send(proto.ServerMsg{Kind: "bell"}); err != nil {
			go cc.conn.Close()
		}
	}
}

// byeAll sends every attached client a parting message, so each one can
// exit reporting what happened rather than inferring it from the socket
// going quiet. Best-effort by design: a client that has already gone is
// nothing to worry about at shutdown.
func (s *Session) byeAll(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for cc := range s.clients {
		_ = cc.send(proto.ServerMsg{Kind: "bye", Bye: reason})
	}
}

func (s *Session) broadcast() {
	f := s.core.Frame()
	s.mu.Lock()
	defer s.mu.Unlock()
	for cc := range s.clients {
		if err := cc.send(proto.ServerMsg{Kind: "frame", Frame: &f}); err != nil {
			go cc.conn.Close() // reader goroutine will notice and detach it
		}
	}
}

func (s *Session) handleConn(conn net.Conn, stop func()) {
	dec := gob.NewDecoder(conn)
	enc := gob.NewEncoder(conn)
	cc := &clientConn{conn: conn, enc: enc}

	var hello proto.ClientMsg
	if err := dec.Decode(&hello); err != nil {
		conn.Close()
		return
	}

	switch hello.Kind {
	case "query":
		enc.Encode(proto.ServerMsg{Kind: "info", Info: &proto.ServerInfo{
			Name:      s.core.Name(),
			PaneCount: s.core.PaneCount(),
			CreatedAt: s.core.CreatedAt.Unix(),
		}})
		conn.Close()
		return

	case "kill":
		enc.Encode(proto.ServerMsg{Kind: "bye", Bye: fmt.Sprintf("🐳 session %q terminated", s.core.Name())})
		conn.Close()
		stop()
		return

	case "send-keys":
		err := s.core.CLISendKeys(hello.WindowIdx, hello.WindowName, hello.PaneIndex, hello.CLIText, hello.CLIEnter)
		enc.Encode(cliReply(err))
		conn.Close()
		return

	case "new-window":
		idx, err := s.core.CLINewWindow(hello.CLIName, hello.CLICommand)
		reply := cliReply(err)
		reply.CLIIndex = idx
		enc.Encode(reply)
		conn.Close()
		return

	case "split-window":
		axis := layout.Vertical
		if hello.CLIAxis == "s" {
			axis = layout.Horizontal
		}
		paneID, err := s.core.CLISplitWindow(hello.WindowIdx, hello.WindowName, hello.PaneIndex, axis, hello.CLICommand)
		reply := cliReply(err)
		reply.CLIPaneID = paneID
		enc.Encode(reply)
		conn.Close()
		return

	case "select-window":
		err := s.core.CLISelectWindow(hello.WindowIdx, hello.WindowName)
		enc.Encode(cliReply(err))
		conn.Close()
		return

	case "select-pane":
		err := s.core.CLISelectPane(hello.WindowIdx, hello.WindowName, hello.CLIDirection)
		enc.Encode(cliReply(err))
		conn.Close()
		return

	case "list-windows":
		enc.Encode(proto.ServerMsg{Kind: "cli", CLIWindows: s.core.CLIListWindows()})
		conn.Close()
		return

	case "list-panes":
		panes, err := s.core.CLIListPanes(hello.WindowIdx, hello.WindowName)
		reply := cliReply(err)
		reply.CLIPanes = panes
		enc.Encode(reply)
		conn.Close()
		return

	case "hello":
		// fall through to the attach loop below
	default:
		conn.Close()
		return
	}

	cc.readOnly = hello.ReadOnly
	if !cc.readOnly {
		// An observer's own terminal size is irrelevant to (and
		// mustn't shrink or otherwise disrupt) the session everyone
		// else is actually working in.
		s.core.Resize(hello.Cols, hello.Rows)
	}
	s.addClient(cc)
	defer func() {
		s.removeClient(cc)
		conn.Close()
	}()

	if err := cc.send(proto.ServerMsg{Kind: "frame", Frame: framePtr(s.core.Frame())}); err != nil {
		return
	}

	for {
		var m proto.ClientMsg
		if err := dec.Decode(&m); err != nil {
			return // EOF/network drop: just stop attaching, session lives on
		}
		if cc.readOnly && (m.Kind == "key" || m.Kind == "mouse" || m.Kind == "resize") {
			continue // observer: watch only, input has no effect on the shared session
		}
		res := s.core.HandleClientMsg(m)
		if res.HasClipboard {
			if err := cc.send(proto.ServerMsg{Kind: "clipboard", Clipboard: res.Clipboard}); err != nil {
				return
			}
		}
		if res.SwitchSession != "" {
			cc.send(proto.ServerMsg{Kind: "switch", SwitchTo: res.SwitchSession})
			return
		}
		if res.Detach {
			cc.send(proto.ServerMsg{Kind: "bye", Bye: fmt.Sprintf(
				"🐳 detached from %q — it is still running; reattach with: termdock attach -t %q",
				s.core.Name(), s.core.Name())})
			return
		}
	}
}

func framePtr(f proto.Frame) *proto.Frame { return &f }

func cliReply(err error) proto.ServerMsg {
	m := proto.ServerMsg{Kind: "cli"}
	if err != nil {
		m.CLIError = err.Error()
	}
	return m
}
