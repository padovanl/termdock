// Package core is the tcell-free brain of a termdock session: windows,
// pane lifecycle, the split-layout tree, copy-mode/scrollback, mouse
// handling and resize-mode. It owns no real terminal — it runs inside the
// long-lived server process and exposes a Frame snapshot that any number
// of attached clients render, which is what makes detach/reattach
// possible.
package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"

	"termdock/internal/layout"
	"termdock/internal/pane"
	"termdock/internal/proto"
)

// Mode is the session's current input mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeCopy
	ModeResize
	ModeInput // typing a line for rename or search; see input.go
)

const resizeStep = 2

type dragState struct {
	node *layout.Node // Vertical split node whose divider is being dragged
}

// Core is one session's live state: its windows (each with their own pane
// tree) and everything needed to interpret input and render a frame.
// Safe for concurrent use.
type Core struct {
	SessionName string
	CreatedAt   time.Time

	mu sync.Mutex

	windows      []*Window
	activeWindow int
	nextWindowID int

	panes map[int]*pane.Pane // session-wide, keyed by pane ID

	mode      Mode
	prefix    bool
	prefixKey tcell.Key
	statusMsg string
	shellName string
	hostname  string

	copy      copyState
	input     inputState
	drag      *dragState
	lastPaste string // most recent copy-mode yank, for Ctrl-B ]

	mouseDown              bool
	mouseDownX, mouseDownY int

	cols, rows int

	dirty  chan struct{}
	exitCh chan struct{}
	closed bool
}

// New creates a session with one window and one pane already running,
// sized cols x rows (resized again once a client actually attaches and
// reports its size).
func New(sessionName string, cols, rows int) (*Core, error) {
	if cols < 1 {
		cols = 80
	}
	if rows < 2 {
		rows = 24
	}
	hostname, _ := os.Hostname()
	c := &Core{
		SessionName: sessionName,
		CreatedAt:   time.Now(),
		panes:       map[int]*pane.Pane{},
		shellName:   filepath.Base(pane.ShellPath()),
		hostname:    hostname,
		prefixKey:   tcell.KeyCtrlB,
		cols:        cols,
		rows:        rows,
		dirty:       make(chan struct{}, 1),
		exitCh:      make(chan struct{}),
	}
	if err := c.newWindow(); err != nil {
		return nil, err
	}
	return c, nil
}

// SetPrefixKey overrides the default Ctrl-B prefix (config's "prefix"
// setting). Call before the session has any attached clients.
func (c *Core) SetPrefixKey(k tcell.Key) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefixKey = k
}

// Dirty fires whenever state changes in a way that warrants re-rendering.
func (c *Core) Dirty() <-chan struct{} { return c.dirty }

// Exited fires once the session has no windows left and should shut down.
func (c *Core) Exited() <-chan struct{} { return c.exitCh }

func (c *Core) markDirty() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

func (c *Core) startPump(p *pane.Pane) {
	go p.Pump(
		func() { c.paneOutput(p.ID) },
		func() { c.handlePaneExit(p.ID) },
	)
}

// paneOutput flags id's window as having unseen activity if it isn't the
// one currently on screen, the same way tmux's monitor-activity marks a
// background window in the status bar.
func (c *Core) paneOutput(id int) {
	c.mu.Lock()
	if w, _ := c.findWindowAndLeaf(id); w != nil && c.windowIndex(w) != c.activeWindow {
		w.activity = true
	}
	c.mu.Unlock()
	c.markDirty()
}

// PaneCount returns how many panes are currently alive, across every
// window.
func (c *Core) PaneCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.panes)
}

// Resize adapts the whole session — every window, not just the visible
// one, so a hidden window's panes are already correctly sized by the time
// you switch to it — to a new client viewport.
func (c *Core) Resize(cols, rows int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// A real terminal never reports less than 1x1, but stay defensive:
	// negative/zero sizes here would go on to produce negative-length
	// slices in Frame().
	cols = max(cols, 1)
	rows = max(rows, 1)
	if cols == c.cols && rows == c.rows {
		return
	}
	c.cols, c.rows = cols, rows
	c.relayoutLocked()
	c.markDirty()
}

// Shutdown kills every pane in every window. Call once, when the server
// process is about to exit.
func (c *Core) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for _, p := range c.panes {
		p.Close()
	}
}

// Result reports side effects of one HandleClientMsg call that the server
// needs to act on for the specific connection that sent it (as opposed to
// state changes, which just get broadcast to every attached client via
// Dirty/Frame).
type Result struct {
	Clipboard    string // text yanked in copy-mode, to push via the client's OSC52
	HasClipboard bool
	Detach       bool // the requesting client should disconnect
}

// HandleClientMsg applies one input message from an attached client.
// "hello"/"query"/"kill" are handled by the server, not here.
func (c *Core) HandleClientMsg(m proto.ClientMsg) Result {
	switch m.Kind {
	case "key":
		return c.handleKey(m)
	case "mouse":
		return c.handleMouse(m)
	case "resize":
		c.Resize(m.Cols, m.Rows)
	}
	return Result{}
}

// --- pane lifecycle (within the active window) ---------------------------

func (c *Core) doSplit(st layout.SplitType) {
	if _, err := c.doSplitIn(c.win(), st, ""); err != nil {
		c.statusMsg = err.Error()
	}
}

// doSplitIn splits window w's active pane, optionally running command
// instead of an interactive shell in the new pane (command == "" for the
// interactive Ctrl-B v/s bindings). Shared by the interactive split
// keybindings and the split-window CLI command, which can target any
// window, not just the currently active one.
func (c *Core) doSplitIn(w *Window, st layout.SplitType, command string) (int, error) {
	if w.zoomed != nil {
		return 0, errors.New("exit zoom (prefix z) before splitting")
	}
	id := pane.NextID()
	p, err := pane.NewWithCommand(id, 80, 24, command)
	if err != nil {
		return 0, fmt.Errorf("error creating pane: %w", err)
	}
	newLeaf, ok := layout.Split(w.active, st, id, p)
	if !ok {
		p.Close()
		return 0, errors.New("not enough room to split")
	}
	c.panes[id] = p
	w.active = newLeaf
	c.relayoutLocked()
	c.startPump(p)
	return id, nil
}

func (c *Core) killActive() {
	w := c.win()
	n := w.active
	if p, ok := c.panes[n.ID]; ok {
		p.Close()
		delete(c.panes, n.ID)
	}
	c.detachLeafIn(w, n)
}

func (c *Core) handlePaneExit(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.panes[id]
	if !ok {
		return
	}
	w, n := c.findWindowAndLeaf(id)
	p.Close()
	delete(c.panes, id)
	if w != nil && n != nil {
		c.detachLeafIn(w, n)
	}
	c.markDirty()
}

// detachLeafIn removes an already-closed leaf from window w's layout
// tree. If that was w's last pane, the window itself closes; if that was
// the session's last window, the session ends.
func (c *Core) detachLeafIn(w *Window, n *layout.Node) {
	if w.zoomed == n {
		w.zoomed = nil
	}
	if c.copy.active && c.copy.paneID == n.ID {
		c.copy = copyState{}
		if c.mode == ModeCopy {
			c.mode = ModeNormal
		}
	}
	newRoot, next := layout.Remove(w.root, n)
	if newRoot == nil {
		if idx := c.windowIndex(w); idx >= 0 {
			c.removeWindow(idx)
		}
		return
	}
	w.root = newRoot
	if w.active == n {
		w.active = next
	}
	c.relayoutLocked()
}

func (c *Core) toggleZoom() {
	w := c.win()
	if w.zoomed != nil {
		w.zoomed = nil
	} else {
		w.zoomed = w.active
	}
	c.relayoutLocked()
}

func (c *Core) cycleFocus() {
	w := c.win()
	leaves := layout.Leaves(w.root)
	if len(leaves) < 2 {
		return
	}
	for i, l := range leaves {
		if l == w.active {
			c.setActive(leaves[(i+1)%len(leaves)])
			return
		}
	}
}

func (c *Core) moveFocus(dx, dy int) {
	w := c.win()
	leaves := layout.Leaves(w.root)
	cur := w.active.Rect
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2

	var best *layout.Node
	bestDist := int(^uint(0) >> 1)
	for _, l := range leaves {
		if l == w.active {
			continue
		}
		r := l.Rect
		lx, ly := r.X+r.W/2, r.Y+r.H/2
		if dx != 0 && sign(lx-cx) != dx {
			continue
		}
		if dy != 0 && sign(ly-cy) != dy {
			continue
		}
		d := iabs(lx-cx) + iabs(ly-cy)
		if d < bestDist {
			bestDist = d
			best = l
		}
	}
	if best != nil {
		c.setActive(best)
	}
}

func (c *Core) setActive(n *layout.Node) {
	w := c.win()
	if w.zoomed != nil {
		w.zoomed = n
	}
	w.active = n
	c.relayoutLocked()
}

func (c *Core) focusAt(x, y int) {
	if l := c.leafAt(x, y); l != nil {
		c.setActive(l)
	}
}

func (c *Core) leafAt(x, y int) *layout.Node {
	for _, l := range layout.Leaves(c.win().root) {
		r := l.Rect
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			return l
		}
	}
	return nil
}

func findLeafByID(root *layout.Node, id int) *layout.Node {
	for _, l := range layout.Leaves(root) {
		if l.ID == id {
			return l
		}
	}
	return nil
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	default:
		return 0
	}
}

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// statusRows reports whether there's a spare row for the status line (0 or
// 1). On a one-row-tall terminal, showing pane content is more useful than
// showing the status bar, so the status bar is the first thing dropped —
// mirroring how tmux gives up chrome before it gives up pane content.
func (c *Core) statusRows() int {
	if c.rows >= 2 {
		return 1
	}
	return 0
}

// relayoutLocked recomputes every window's layout, not just the visible
// one, so a hidden window's panes stay correctly sized (and their output
// wraps the way it will look once you switch to them) the whole time.
func (c *Core) relayoutLocked() {
	full := layout.Rect{X: 0, Y: 0, W: c.cols, H: max(c.rows-c.statusRows(), 0)}
	for _, w := range c.windows {
		if w.zoomed != nil {
			w.zoomed.Rect = full
			cr := w.zoomed.ContentRect()
			if cr.W > 0 && cr.H > 0 && w.zoomed.Pane != nil {
				w.zoomed.Pane.Resize(cr.W, cr.H)
			}
			continue
		}
		layout.Compute(w.root, full)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
