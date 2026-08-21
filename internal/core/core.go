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

// modeNames drives Mode's String, used by the input log (see logInput) —
// "mode=copy" is worth a great deal more than "mode=1" when the whole
// point of reading that log is working out why a key went somewhere
// unexpected.
var modeNames = map[Mode]string{
	ModeNormal: "normal", ModeCopy: "copy", ModeResize: "resize",
	ModeInput: "input", ModeConfirm: "confirm", ModePicker: "picker",
	ModeHelp: "help", ModeSessions: "sessions", ModeSearch: "search",
	ModeOverview: "overview", ModeRegisters: "registers", ModePopup: "popup",
	ModeOpener: "opener", ModeQuickJump: "quickjump",
}

func (m Mode) String() string {
	if s, ok := modeNames[m]; ok {
		return s
	}
	return fmt.Sprintf("Mode(%d)", int(m))
}

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
	lastWindow   *Window // previously active window, for Ctrl-B l; see toggleLastWindow

	panes          map[int]*pane.Pane // session-wide, keyed by pane ID
	paneLastActive map[int]time.Time  // for the jump picker's MRU ordering; see touchPane

	mode      Mode
	prefix    bool
	prefixKey tcell.Key
	statusMsg string
	shellName string
	hostname  string

	bindings map[rune]action // defaultBindings, overridden per-key by config's "bind" setting; see SetBindOverrides

	// bindOverridden marks which runes came from an explicit config
	// "bind" line rather than defaultBindings. Only the digits need it:
	// handleKey gives 0-9 their built-in "jump to window N" meaning
	// before ever consulting the bindings map, so without knowing a digit
	// was deliberately rebound there'd be no way to let the config win —
	// and `bind 5 vsplit` would be accepted, listed in the help screen,
	// and then silently never fire.
	bindOverridden map[rune]bool

	pendingConfirm func() // what ModeConfirm's y/n prompt runs on 'y'; see confirmKillWindow/confirmQuit/handleConfirmKey

	focusEvents   bool // config's "focus-events"; see SetFocusEvents
	focusedPaneID int  // the pane last sent a focus-in, for updateFocusEvents

	// repeatTime/repeatUntil implement repeatable focus moves — tmux's
	// `bind -r` plus repeat-time. After a prefixed arrow moves the focus,
	// a *bare* arrow keeps moving it until repeatUntil passes, so walking
	// three panes over is Ctrl-B ←←← rather than Ctrl-B ← Ctrl-B ← Ctrl-B ←.
	// See handleKey.
	repeatTime  time.Duration
	repeatUntil time.Time

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
	popupCommand string // command to run in the popup instead of an interactive shell; see SetPopupCommand

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
		bindings:    cloneBindings(defaultBindings),
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

// SetPopupCommand overrides what Ctrl-B P runs in the floating popup —
// "" (the default) keeps it an interactive shell, same as any other
// pane. Call before the session has any attached clients.
func (c *Core) SetPopupCommand(cmd string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.popupCommand = cmd
}

// SetBindOverrides applies config-driven keybinding overrides (config's
// "bind <key> <action>" lines) on top of defaultBindings — an unknown
// action name is ignored, the same "bad setting, keep the default"
// leniency every other termdock.conf key already has. Call before the
// session has any attached clients.
func (c *Core) SetBindOverrides(overrides map[rune]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for r, name := range overrides {
		if act := action(name); validActions[act] {
			c.bindings[r] = act
			if c.bindOverridden == nil {
				c.bindOverridden = map[rune]bool{}
			}
			c.bindOverridden[r] = true
		}
	}
}

// SetRepeatTime sets how long a bare arrow key keeps repeating a focus
// move after a prefixed one (config's "repeat-time", in milliseconds);
// 0 disables repeating entirely. Call before the session has any
// attached clients.
func (c *Core) SetRepeatTime(ms int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ms < 0 {
		ms = 0
	}
	c.repeatTime = time.Duration(ms) * time.Millisecond
}

// SetFocusEvents enables synthesizing terminal focus-in/focus-out
// escape sequences (config's "focus-events" setting) — see
// updateFocusEvents for exactly what that covers and doesn't. Call
// before the session has any attached clients.
func (c *Core) SetFocusEvents(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.focusEvents = enabled
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
	// Closing the panes is the part that must not run twice; deleting the
	// snapshot has to happen either way. Both of the ordinary ways a
	// session ends on purpose — Ctrl-B q and the last pane exiting — go
	// through requestQuit, which marks the session closed *before* the
	// server's Exited() watcher gets here, so returning early on that flag
	// meant the snapshot survived a deliberate quit: starting a session
	// with the same name again silently restored the very layout that was
	// just quit out of, instead of the fresh single pane it should have.
	alreadyClosed := c.closed
	c.closed = true
	if !alreadyClosed {
		for _, p := range c.panes {
			p.Close()
		}
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
	defer c.logInput(m)
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

// logInput appends one line per input message to $TERMDOCK_INPUT_LOG,
// when that's set — the raw key/mouse event as it arrived, plus the
// prefix/mode state it left behind. Off (and free) otherwise.
//
// Input bugs in a multiplexer are otherwise near-impossible to pin down:
// what the terminal emulator actually sends for a chord, whether a key
// arrived at all, and whether the prefix was armed when it did are all
// invisible from the outside, and every layer in between (terminal,
// tcell, client, server) is a plausible culprit. This makes the answer a
// file you can read.
func (c *Core) logInput(m proto.ClientMsg) {
	path := os.Getenv("TERMDOCK_INPUT_LOG")
	if path == "" {
		return
	}
	c.mu.Lock()
	prefix, mode := c.prefix, c.mode
	c.mu.Unlock()

	var what string
	switch m.Kind {
	case "key":
		what = fmt.Sprintf("key code=%d rune=%q mod=%d", m.KeyCode, m.KeyRune, m.KeyMod)
	case "mouse":
		what = fmt.Sprintf("mouse x=%d y=%d buttons=%d mod=%d", m.MouseX, m.MouseY, m.MouseButtons, m.MouseMod)
	case "resize":
		what = fmt.Sprintf("resize %dx%d", m.Cols, m.Rows)
	default:
		what = m.Kind
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %-44s -> prefix=%v mode=%v\n",
		time.Now().Format("15:04:05.000"), what, prefix, mode)
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
	return c.doSplitLeafIn(w, w.active, st, command)
}

// doSplitLeafIn is doSplitIn aimed at a specific pane rather than
// whichever one happens to be active — what a scripting TARGET's ".PANE"
// part resolves to (see CLISplitWindow).
func (c *Core) doSplitLeafIn(w *Window, target *layout.Node, st layout.SplitType, command string) (int, error) {
	if w.zoomed != nil {
		return 0, errors.New("exit zoom (prefix z) before splitting")
	}
	id := pane.NextID()
	p, err := pane.NewWithCommand(id, 80, 24, command)
	if err != nil {
		return 0, fmt.Errorf("error creating pane: %w", err)
	}
	newLeaf, ok := layout.Split(target, st, id, p)
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
	if w.lastActive == n {
		w.lastActive = nil // Ctrl-B ; has nothing to flip back to once it's closed
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
	c.moveFocusIn(c.win(), dx, dy)
}

// moveFocusIn moves w's focus to the nearest leaf in the spatial
// direction (dx,dy) from its current active one, same geometry-nearest
// logic hjkl/arrows already use — generalized to an arbitrary window
// (not just c.win()) so CLISelectPane can move focus in a *background*
// window from outside, the same way CLISelectWindow already targets any
// window by index/name regardless of which one's currently visible.
// Reports whether anything was actually found in that direction.
func (c *Core) moveFocusIn(w *Window, dx, dy int) bool {
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
	if best == nil {
		return false
	}
	if w == c.win() {
		c.setActive(best) // the visible window: go through the normal focus path (touchPane, focus-events, relayout)
	} else {
		w.active = best // a background window: nothing else needs updating until a client actually looks at it
	}
	return true
}

func (c *Core) setActive(n *layout.Node) {
	c.setWindowActiveLeaf(c.win(), n)
	c.touchPane(n.ID)
	c.updateFocusEvents(n.ID)
	c.relayoutLocked()
}

// setWindowActiveLeaf focuses leaf within w, recording the pane being
// left as w.lastActive (what Ctrl-B ; jumps back to). Split out of
// setActive because the jump picker, the overview and global search all
// switch window *and* pane in one step: they can't call setActive, which
// only ever operates on the currently visible window, and assigning
// w.active directly — which is what they used to do — skipped the
// lastActive bookkeeping entirely, quietly breaking Ctrl-B ; after any
// jump through one of them.
func (c *Core) setWindowActiveLeaf(w *Window, leaf *layout.Node) {
	if w.active != leaf {
		w.lastActive = w.active
	}
	if w.zoomed != nil {
		w.zoomed = leaf
	}
	w.active = leaf
}

// toggleLastPane jumps back to whichever pane was focused right before
// the current one, within this window — tmux's Ctrl-B ;. A no-op if
// there's no recorded last pane, or it's since closed or moved to
// another window (lastActive is cleared wherever that can happen; see
// detachLeafIn, movePaneToWindow, breakPaneToNewWindow).
func (c *Core) toggleLastPane() {
	w := c.win()
	if w.lastActive == nil || w.lastActive == w.active {
		return
	}
	c.setActive(w.lastActive)
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
