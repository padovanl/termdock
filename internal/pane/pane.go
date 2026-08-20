// Package pane wraps a shell process running inside a pseudo-terminal
// together with the VT10x screen buffer that interprets its output.
package pane

import (
	"bufio"
	"os"
	"os/exec"
	"sync/atomic"

	"github.com/creack/pty"

	"termdock/internal/vt10x"
)

// These mirror vt10x's unexported glyph attribute bits (see its state.go).
// They're stable (the library mirrors suckless st's attribute layout) but
// unexported there, so we redeclare them to interpret Glyph.Mode ourselves.
const (
	AttrReverse = 1 << iota
	AttrUnderline
	AttrBold
	AttrGfx
	AttrItalic
	AttrBlink
	AttrWrap
)

// Pane is one shell session: a pty, the child process attached to it, and
// the terminal emulator that turns its output into a grid of cells.
type Pane struct {
	ID int

	pty  *os.File
	cmd  *exec.Cmd
	term vt10x.Terminal

	closed int32
}

var nextID int32

// NextID returns a fresh, process-unique pane id for display purposes.
func NextID() int {
	return int(atomic.AddInt32(&nextID, 1))
}

// defaultShell and defaultHistoryLimit are session-wide settings (the
// config file's "shell" and "history-limit"), applied to every pane
// this process creates. SetDefaults is meant to be called once, from the
// daemon's startup before any pane exists — not a per-pane knob, so a
// package-level var is simpler than threading it through every New call.
var (
	defaultShell        string
	defaultHistoryLimit int
)

// SetDefaults overrides the shell and scrollback size new panes are
// created with. shell == "" keeps the $SHELL/ ShellPath() default;
// historyLimit <= 0 keeps vt10x's own default.
func SetDefaults(shell string, historyLimit int) {
	defaultShell = shell
	defaultHistoryLimit = historyLimit
}

// ShellPath returns the interactive shell to launch for new panes.
func ShellPath() string {
	if defaultShell != "" {
		return defaultShell
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// New spawns the user's interactive login shell inside a new pty sized
// cols x rows and wraps it with a VT10x terminal emulator.
func New(id, cols, rows int) (*Pane, error) {
	return newPaneOpts(id, cols, rows, "", "")
}

// NewWithCommand is New, but runs command (via the shell's -c) instead of
// an interactive login shell — used by the new-window/split-window CLI
// commands to launch a specific program. command == "" behaves exactly
// like New. The pane closes when the command exits, same as any other.
func NewWithCommand(id, cols, rows int, command string) (*Pane, error) {
	return newPaneOpts(id, cols, rows, command, "")
}

// NewInDir is New, but starts the shell in dir instead of the process's
// own working directory — used to restore a session snapshot's panes
// where they were last seen (see internal/persist and Cwd). dir == ""
// behaves exactly like New.
func NewInDir(id, cols, rows int, dir string) (*Pane, error) {
	return newPaneOpts(id, cols, rows, "", dir)
}

func newPaneOpts(id, cols, rows int, command, dir string) (*Pane, error) {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	shell := ShellPath()
	var cmd *exec.Cmd
	if command == "" {
		cmd = exec.Command(shell, "-l")
	} else {
		cmd = exec.Command(shell, "-c", command)
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if dir != "" {
		cmd.Dir = dir
	}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		return nil, err
	}

	term := vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(f), vt10x.WithHistoryLimit(defaultHistoryLimit))

	return &Pane{ID: id, pty: f, cmd: cmd, term: term}, nil
}

// Term exposes the underlying emulator for reading cell/cursor state.
// Callers must hold Lock/Unlock around reads, same as during Pump.
func (p *Pane) Term() vt10x.Terminal { return p.term }

// ForegroundTitle returns the name of the process currently running in the
// foreground of this pane (e.g. "vim"), or "" if unknown/idle-at-shell.
func (p *Pane) ForegroundTitle() string {
	return foregroundName(p.pty)
}

// Cwd returns this pane's shell process's current working directory
// (Linux only; "" elsewhere), tracking plain `cd` commands the same way
// a real terminal's "new tab in this directory" does. Used for session
// snapshots (see internal/persist) — not the true state of whatever's
// actually running (nothing can resurrect that after a crash), just
// where to put you back.
func (p *Pane) Cwd() string {
	if p.cmd == nil || p.cmd.Process == nil {
		return ""
	}
	return processCwd(p.cmd.Process.Pid)
}

// Pump blocks reading and parsing pty output, calling onUpdate after every
// batch of escape sequences it applies, until the pty is closed or the
// child exits. It then calls onExit exactly once. Meant to run in its own
// goroutine, one per pane.
func (p *Pane) Pump(onUpdate, onExit func()) {
	br := bufio.NewReader(p.pty)
	for {
		if err := p.term.Parse(br); err != nil {
			break
		}
		if atomic.LoadInt32(&p.closed) != 0 {
			break
		}
		onUpdate()
	}
	onExit()
}

// Resize adapts both the emulator's grid and the pty's kernel-side window
// size. Implements layout.PaneHost.
func (p *Pane) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	// vt10x's Resize locks internally; wrapping it in our own Lock/Unlock
	// (as we do for Cell/Cursor reads, which don't self-lock) would
	// deadlock on its non-reentrant mutex.
	p.term.Resize(cols, rows)
	_ = pty.Setsize(p.pty, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Write sends raw input bytes (already translated from key events) to the
// child process.
func (p *Pane) Write(b []byte) {
	if atomic.LoadInt32(&p.closed) != 0 {
		return
	}
	_, _ = p.pty.Write(b)
}

// Close terminates the child process and releases the pty. Safe to call
// more than once.
func (p *Pane) Close() {
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	_ = p.pty.Close()
}
