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

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/persist"
	"github.com/padovanl/termdock/internal/proto"
)

// Mode is the session's current input mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeCopy
	ModeResize
	ModeInput   // typing a line for rename or search; see input.go
	ModeConfirm // a pending destructive action awaiting y/n; see confirmKillWindow
	ModePicker    // type-ahead jump to any window/pane; see picker.go
	ModeHelp      // scrollable keybinding reference; see help.go
	ModeSessions  // type-ahead switch to another session; see sessions.go
	ModeSearch    // type-ahead search every pane's scrollback; see search.go
	ModeOverview  // live-thumbnail grid of every pane; see overview.go
	ModeRegisters // type-ahead pick which yank to paste; see registers.go
	ModePopup     // the floating scratch terminal is focused; see popup.go
	ModeOpener    // type-ahead pick a URL/path spotted on screen; see opener.go
	ModeQuickJump // display-panes: big numbers overlay, press one to jump; see quickjump.go
)

const resizeStep = 2

// doubleClickWindow is how close together two clicks on the same pane's
// title bar need to land to count as a double-click (zoom toggle) instead
// of two independent single clicks (focus).
const doubleClickWindow = 400 * time.Millisecond

type dragState struct {
	node *layout.Node // split node whose divider is being dragged
	axis layout.SplitType
}

// tabDragState tracks a press-and-hold on a status bar window tab: while
// held, moving over a different tab live-reorders win to that position
// (see updateTabDrag); moved distinguishes that from a stationary click,
// which just selects win instead (see endTabDrag).
type tabDragState struct {
	win   *Window
	moved bool
}

// contentPressState tracks a press-and-hold on a pane's ordinary content
// (not its title, not a divider): x,y is the press point, in absolute
// screen coordinates, that a text selection would be anchored at if this
// turns into a drag (see startContentDragSelect) rather than a plain
// click (see handleNormalMouse's released branch).
type contentPressState struct {
	leaf *layout.Node
	x, y int
}

// titleDragState tracks a press-and-hold on a pane's title bar, armed
// alongside (not instead of) the ordinary click/double-click handling
// that already fires immediately on press — so dropping it on a window
// tab, in the status bar, moves that pane into that window (see
// endTitleDrag); dropping it anywhere else is simply a no-op on top of
// whatever the press already did.
type titleDragState struct {
	leaf *layout.Node
	win  *Window
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

	panes          map[int]*pane.Pane // session-wide, keyed by pane ID
	paneLastActive map[int]time.Time  // for the jump picker's MRU ordering; see touchPane

	mode      Mode
	prefix    bool
	prefixKey tcell.Key
	statusMsg string
	shellName string
	hostname  string

	copy         copyState
	input        inputState
	picker       pickerState
	regPicker    registerPickerState
	sessions     sessionPickerState
	search       globalSearchState
	overview     overviewState
	opener       openerState
	help         helpState
	drag         *dragState
	tabDrag      *tabDragState
	contentPress *contentPressState
	titleDrag    *titleDragState
	registers    []registerEntry // yanks, most recent first, for Ctrl-B ] and Ctrl-B = (see registers.go)

	popup        *pane.Pane // the floating scratch terminal (Ctrl-B P), lazily created; see popup.go
	popupVisible bool

	statusSegments []string      // enabled optional status-bar segments ("git", "battery"); see segments.go
	segCache       segmentCache

	bellCh chan struct{} // non-blocking signal for a background window's *new* activity; see Bell

	// ListSessions, if set (by the server, which knows about sibling
	// daemons — core deliberately doesn't import internal/server, so it
	// can't discover them itself), lists every session available to
	// switch to (Ctrl-B S), excluding this one.
	ListSessions func() []string

	mouseDown              bool
	mouseDownX, mouseDownY int

	dragDownX, dragDownY int // press position for the divider drag currently in c.drag, to detect a stationary click on release

	lastTitleClickID int       // pane ID of the last title-bar click, for double-click detection
	lastTitleClickAt time.Time

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
		bellCh:      make(chan struct{}, 1),
	}
	if snap, ok := persist.Load(sessionName); ok && c.restoreFromSnapshot(snap) {
		c.relayoutLocked()
		return c, nil
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

// Bell fires whenever a background window produces output for the first
// time since you last looked at it — see paneOutput and ringBell — so
// the server can pass a real terminal bell on to attached clients rather
// than relying solely on the passive "!" marker in the tab strip.
func (c *Core) Bell() <-chan struct{} { return c.bellCh }

func (c *Core) ringBell() {
	select {
	case c.bellCh <- struct{}{}:
	default:
	}
}

// SetStatusSegments enables optional status-bar segments ("git",
// "battery" — see segments.go), off by default. Call before the session
// has any attached clients.
func (c *Core) SetStatusSegments(segs []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statusSegments = segs
}

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
	if w, _ := c.findWindowAndLeaf(id); w != nil && c.windowIndex(w) != c.activeWindow && !w.activity {
		w.activity = true
		c.ringBell() // only on the false->true edge, not every line a background window prints
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
	c.resizePopupLocked()
	c.markDirty()
}

// Shutdown kills every pane in every window and deletes this session's
// persisted snapshot, if any — reaching Shutdown at all means the
// session is ending on purpose (Ctrl-B q, its last pane exiting, or
// kill-session), so there's nothing to recover next time this name is
// used. Call once, when the server process is about to exit; a crash or
// a killed process never reaches this, which is what leaves a snapshot
// behind to actually recover from.
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
	persist.Delete(c.SessionName)
}

// Result reports side effects of one HandleClientMsg call that the server
// needs to act on for the specific connection that sent it (as opposed to
// state changes, which just get broadcast to every attached client via
// Dirty/Frame).
type Result struct {
	Clipboard     string // text yanked in copy-mode, to push via the client's OSC52
	HasClipboard  bool
	Detach        bool   // the requesting client should disconnect
	SwitchSession string // the requesting client should reconnect to this session instead (see sessions.go)
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
	c.persistStateLocked()
	return id, nil
}

func (c *Core) killActive() {
	w := c.win()
	n := w.active
	if p, ok := c.panes[n.ID]; ok {
		p.Close()
		delete(c.panes, n.ID)
		delete(c.paneLastActive, n.ID)
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
	delete(c.paneLastActive, id)
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
	c.persistStateLocked()
}

func (c *Core) toggleZoom() {
	c.toggleZoomOn(c.win().active)
}

// toggleZoomOn zooms n specifically (making it active first if it wasn't),
// or un-zooms if n is already the zoomed pane — unlike toggleZoom, which
// always acts on whatever's currently active, this lets a click somewhere
// else (double-clicking a title bar) target a pane directly regardless of
// focus.
func (c *Core) toggleZoomOn(n *layout.Node) {
	w := c.win()
	if w.zoomed == n {
		w.zoomed = nil
	} else {
		w.active = n
		w.zoomed = n
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
	c.touchPane(n.ID)
	c.relayoutLocked()
}

// touchPane stamps id as just having become the one the user's looking
// at, for the jump picker's most-recently-used ordering (see
// refilterPicker) — the pane you just left ranks highest next time you
// open it with an empty query, the same "press it again to go back"
// feel as Alt-Tab.
func (c *Core) touchPane(id int) {
	if c.paneLastActive == nil {
		c.paneLastActive = map[int]time.Time{}
	}
	c.paneLastActive[id] = time.Now()
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
//
// A one-cell margin is reserved around the whole pane area first: the
// client draws every pane's border in that margin/in the gap each split
// reserves between its children, entirely outside the Rects computed
// here, so panes never need to give up their own content rows/columns for
// chrome (see internal/client/render.go).
func (c *Core) relayoutLocked() {
	outer := layout.Rect{X: 0, Y: 0, W: c.cols, H: max(c.rows-c.statusRows(), 0)}
	full := outer
	if full.W >= 3 {
		full.X++
		full.W -= 2
	} else {
		full.W = 0
	}
	if full.H >= 3 {
		full.Y++
		full.H -= 2
	} else {
		full.H = 0
	}
	for _, w := range c.windows {
		if w.zoomed != nil {
			w.zoomed.Rect = full
			if full.W > 0 && full.H > 0 && w.zoomed.Pane != nil {
				w.zoomed.Pane.Resize(full.W, full.H)
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
