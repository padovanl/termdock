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

	"github.com/padovanl/termdock/internal/config"
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
	ModeInput     // typing a line for rename or search; see input.go
	ModeConfirm   // a pending destructive action awaiting y/n; see confirmKillWindow
	ModePicker    // type-ahead jump to any window/pane; see picker.go
	ModeHelp      // scrollable keybinding reference; see help.go
	ModeSessions  // type-ahead switch to another session; see sessions.go
	ModeSearch    // type-ahead search every pane's scrollback; see search.go
	ModeOverview  // live-thumbnail grid of every pane; see overview.go
	ModeRegisters // type-ahead pick which yank to paste; see registers.go
	ModePopup     // the floating scratch terminal is focused; see popup.go
	ModeOpener    // type-ahead pick a URL/path spotted on screen; see opener.go
	ModeQuickJump // display-panes: big numbers overlay, press one to jump; see quickjump.go
	ModeSettings  // browse/edit the session's settings; see settings.go
	ModeHistory   // fuzzy picker over every command run this session; see cmdhistory.go
	ModeTimeline  // when each command ran, and for how long; see timeline.go
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
	ModeOpener: "opener", ModeQuickJump: "quickjump", ModeSettings: "settings",
	ModeHistory: "history", ModeTimeline: "timeline",
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
//
// The pane is remembered by id, not by *layout.Node. A press is held
// across other events — and layout.Split rewrites the very node it
// splits *in place*, turning that leaf into the new split and giving the
// pane a freshly made leaf of its own. A stored pointer therefore stops
// being a leaf at all while the pane it named is still perfectly alive,
// so it has to be resolved against the live tree at the moment it's
// used, not held onto.
type contentPressState struct {
	paneID int
	x, y   int
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

	// cfg is the session's effective settings — seeded from the config
	// file at startup, then whatever ":set" has changed since. See
	// settings.go.
	cfg                 config.Config
	clientCfgOverridden bool // a look-and-feel setting was changed here, so clients follow the session rather than their own file

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
	settings     settingsState
	history      historyPickerState
	timeline     timelineState
	drag         *dragState
	tabDrag      *tabDragState
	contentPress *contentPressState
	titleDrag    *titleDragState
	closedPanes  []closedPane // undo stack behind Ctrl-B Z; see undoclose.go
	// lastTabs is the window tab strip as the last Frame laid it out, so
	// a click resolves against what is on screen rather than a strip
	// re-derived after the labels have moved on. See tabAt.
	lastTabs []proto.WindowTab
	// doneWatch maps a watched pane id to whether it was busy last time
	// we looked; see watchdone.go (Ctrl-B m).
	doneWatch map[int]bool

	// paneNames holds names the user gave individual panes. A pane is
	// otherwise titled after whatever process happens to hold its
	// foreground, which is "bash" for every idle one — useless exactly
	// when you have six of them and need to tell them apart. Keyed by
	// pane id so it survives splits and layout changes rebuilding the
	// tree. See renamePane.
	paneNames map[int]string
	registers []registerEntry // yanks, most recent first, for Ctrl-B ] and Ctrl-B = (see registers.go)

	popup        *pane.Pane // the floating scratch terminal (Ctrl-B P), lazily created; see popup.go
	popupVisible bool
	popupCommand string // command to run in the popup instead of an interactive shell; see SetPopupCommand

	statusSegments []string // enabled optional status-bar segments ("git", "battery"); see segments.go
	segCache       segmentCache

	bellCh chan struct{} // non-blocking signal for a background window's *new* activity; see Bell

	// ListSessions, if set (by the server, which knows about sibling
	// daemons — core deliberately doesn't import internal/server, so it
	// can't discover them itself), lists every session available to
	// switch to (Ctrl-B S), excluding this one.
	ListSessions func() []string

	// RenameSession, if set (by the server, same reasoning as
	// ListSessions), renames this live session — which is really a
	// filesystem operation, not a core one: the name is what its unix
	// socket and its persistence snapshot are called, so core on its own
	// could only change the label in the status bar and leave `termdock
	// ls` and `attach -t` still answering to the old one. Returns an
	// error the prompt shows as-is (name already taken, invalid, ...).
	RenameSession func(newName string) error

	mouseDown              bool
	mouseDownX, mouseDownY int

	dragDownX, dragDownY int // press position for the divider drag currently in c.drag, to detect a stationary click on release

	lastTitleClickID int // pane ID of the last title-bar click, for double-click detection
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
		// Sane settings from the very first frame. The server replaces
		// these with the config file's a moment later (ApplyConfig), but
		// until it does, a zero Config would have the settings screen
		// reporting a prefix of "key-0" and a scrollback of 0 lines.
		cfg:    config.Default(),
		cols:   cols,
		rows:   rows,
		dirty:  make(chan struct{}, 1),
		exitCh: make(chan struct{}),
		bellCh: make(chan struct{}, 1),
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

// Name returns the session's current name, read under the lock: a
// session can be renamed while it runs (Ctrl-B $, see
// applySessionRename), from the input goroutine, while the server
// reads the name on another to answer a query or say goodbye. Reading
// the field directly from outside core is a data race the moment
// renaming exists.
func (c *Core) Name() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.SessionName
}

// SetRepeatTime sets how long a bare arrow key keeps repeating a focus
// move after a prefixed one (config's "repeat-time", in milliseconds);
// 0 disables repeating entirely. Call before the session has any
// attached clients.
func (c *Core) SetRepeatTime(ms int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.setRepeatTimeLocked(ms)
}

func (c *Core) setRepeatTimeLocked(ms int) {
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

// maxCols/maxRows bound a client-reported terminal size. Far beyond any
// real terminal — a 5120px-wide display at six pixels per character is
// around 850 columns — while keeping the worst case a frame can cost to
// something a machine can actually hold.
const (
	maxCols = 2000
	maxRows = 1000
)

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
	// And nothing bounds it from above either — the size is just a number
	// a client sends. Frame() builds one proto.Cell per character cell of
	// every pane, for every frame, and then gob-encodes the lot to every
	// attached client: at 5000x3000 that is already ~700MB per frame, and
	// a client reporting something truly absurd would take the daemon out
	// of memory, and every pane in every window with it. The pty ioctl
	// takes a uint16 anyway, so past 65535 the panes and the layout would
	// silently disagree about their own size regardless.
	cols = min(cols, maxCols)
	rows = min(rows, maxRows)
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
	if w.lastActivePane == n.ID {
		w.lastActivePane = 0 // Ctrl-B ; has nothing to flip back to once it's closed
	}
	if c.copy.active && c.copy.paneID == n.ID {
		c.copy = copyState{}
		if c.mode == ModeCopy {
			c.mode = ModeNormal
		}
	}
	// A mouse gesture in flight can outlive the pane it started on: a
	// shell exiting is asynchronous, so the button can still be down when
	// its pane disappears. Left armed, the next mouse move resolved the
	// press against a leaf no longer in any tree and made *that* the
	// window's active pane — after which nothing rendered as focused, the
	// arrow keys had stale geometry to navigate from, and a split would
	// have attached a brand new pane to an orphaned branch nobody draws.
	if c.contentPress != nil && c.contentPress.paneID == n.ID {
		c.contentPress = nil
	}
	if c.titleDrag != nil && c.titleDrag.leaf == n {
		c.titleDrag = nil
	}
	// The divider being dragged may be the split that just got collapsed
	// away; either way the geometry it was dragging against is gone.
	c.drag = nil
	// Before it leaves the tree, while its working directory can still be
	// read off the live process — that path is the whole value of the
	// undo (see undoclose.go).
	c.recordClosedPane(w, n)
	defer c.pruneBroadcast(w) // after the tree loses it, so the set stops naming a pane that is gone
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
		// Through setWindowActiveLeaf rather than assigning directly, so
		// that zooming a *different* pane (double-clicking its title
		// bar) records the one being left as Ctrl-B ;'s target, the same
		// as every other way of changing which pane has focus.
		c.setWindowActiveLeaf(w, n)
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

// moveFocus moves the visible window's focus one pane in a direction,
// reporting whether there was anything in that direction to move to.
func (c *Core) moveFocus(dx, dy int) bool {
	return c.moveFocusIn(c.win(), dx, dy)
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
		w.lastActivePane = w.active.ID
	}
	if w.zoomed != nil {
		w.zoomed = leaf
	}
	w.active = leaf
}

// toggleLastPane jumps back to whichever pane was focused right before
// the current one, within this window — tmux's Ctrl-B ;. A no-op if
// there's no recorded last pane, or it isn't in this window's tree any
// more: resolving the id here rather than trusting a stored node is what
// makes it safe against everything that rewrites the tree underneath it
// (see Window.lastActivePane).
func (c *Core) toggleLastPane() {
	w := c.win()
	if w.lastActivePane == 0 || w.lastActivePane == w.active.ID {
		return
	}
	leaf := findLeafByID(w.root, w.lastActivePane)
	if leaf == nil {
		w.lastActivePane = 0
		return
	}
	c.setActive(leaf)
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
