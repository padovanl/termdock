package core

import (
	"fmt"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
)

// Window is one tab: its own independent pane layout. Panes themselves
// live in Core.panes, keyed by a session-wide unique ID; a window's
// layout tree just references the ones that belong to it, the same way
// tmux windows each have their own pane tree within one session.
type Window struct {
	ID      int
	Name    string
	renamed bool // true once the user has explicitly set Name

	root, active, zoomed *layout.Node
	// lastActivePane is the pane Ctrl-B ; jumps back to, held as an id
	// rather than a *layout.Node (0 means none). A node pointer would
	// not survive: layout.Split rewrites the leaf it splits *in place*,
	// so the node stops being a leaf while the pane it named is still
	// running, and cycling the layout throws every node away and builds
	// new ones around the same panes. Ids are unique for the life of the
	// process and never reused, so resolving one against the live tree
	// is always either the right pane or nothing at all.
	lastActivePane int
	syncPanes      bool
	// syncOnly, when non-empty, restricts synchronized input to those
	// pane ids; empty means every pane in the window. See broadcast.go.
	syncOnly map[int]bool

	activity bool // output arrived while this window wasn't the active one

	layoutPreset layoutPreset // which preset Ctrl-B Space would apply next; see layoutpreset.go
}

// win returns the active window. Every Core method that manipulates "the"
// pane tree operates on this one; findWindowAndLeaf is used instead for
// the few cases (an exiting pane, mouse coordinates) that might belong to
// a different, currently-hidden window.
func (c *Core) win() *Window {
	return c.windows[c.activeWindow]
}

func (c *Core) windowIndex(w *Window) int {
	for i, ww := range c.windows {
		if ww == w {
			return i
		}
	}
	return -1
}

// resolveWindow returns the window at idx; if idx < 0 and name is set, it
// looks up name against each window's display name instead (so scripting
// commands can target a window by the name it was created with, not just
// its numeric index); if both are unset, it's the active window.
func (c *Core) resolveWindow(idx int, name string) (*Window, bool) {
	if idx >= 0 {
		if idx >= len(c.windows) {
			return nil, false
		}
		return c.windows[idx], true
	}
	if name != "" {
		for _, w := range c.windows {
			if c.windowDisplayName(w) == name {
				return w, true
			}
		}
		return nil, false
	}
	if len(c.windows) == 0 {
		return nil, false // session mid-shutdown; a lingering connection raced us here
	}
	return c.win(), true
}

func (c *Core) findWindowAndLeaf(paneID int) (*Window, *layout.Node) {
	for _, w := range c.windows {
		if l := findLeafByID(w.root, paneID); l != nil {
			return w, l
		}
	}
	return nil, nil
}

// windowDisplayName returns what the status bar shows for w: its custom
// name if the user set one (Ctrl-B ,), otherwise the foreground command
// of its active pane — mirroring tmux's automatic-rename default.
func (c *Core) windowDisplayName(w *Window) string {
	if w.renamed {
		return w.Name
	}
	if p, ok := c.panes[w.active.ID]; ok {
		if fg := p.ForegroundTitle(); fg != "" {
			return fg
		}
	}
	return c.shellName
}

// newWindow creates a window with one fresh pane and makes it active.
// Only the very first call (from New) can fail outright; later calls
// (Ctrl-B c) just report the error in the status bar and leave the
// session as it was.
func (c *Core) newWindow() error {
	_, err := c.newWindowOpts("", "")
	return err
}

// newWindowOpts is newWindow with an optional custom name and an optional
// command to run instead of the interactive shell (used by the
// new-window CLI command; the interactive Ctrl-B c binding just passes
// "", "").
func (c *Core) newWindowOpts(name, command string) (*Window, error) {
	id := pane.NextID()
	p, err := pane.NewWithCommand(id, max(c.cols, 1), max(c.rows-c.statusRows(), 1), command)
	if err != nil {
		c.statusMsg = "error creating window: " + err.Error()
		return nil, err
	}
	root := layout.NewLeaf(id, p)
	w := &Window{ID: c.nextWindowID, root: root, active: root}
	if name != "" {
		w.Name = name
		w.renamed = true
	}
	c.nextWindowID++

	c.panes[id] = p
	if len(c.windows) > 0 {
		c.lastWindow = c.windows[c.activeWindow] // not on the very first window a session ever gets
	}
	c.windows = append(c.windows, w)
	c.activeWindow = len(c.windows) - 1
	c.afterWindowSwitch()
	c.relayoutLocked()
	c.startPump(p)
	c.persistStateLocked()
	return w, nil
}

// moveWindow relocates the window at index from so that it ends up at
// index to in the resulting slice (everything between the two shifts
// over by one to make room) — the same semantics as dragging a browser
// tab to a new position. c.activeWindow is tracked by the identity of
// whichever *Window it currently points to (rather than recomputed by
// arithmetic on the two indices), which stays correct regardless of
// whether the window being moved is the active one, is on either side of
// it, or is it entirely unrelated.
func (c *Core) moveWindow(from, to int) {
	if from == to || from < 0 || from >= len(c.windows) || to < 0 || to >= len(c.windows) {
		return
	}
	active := c.windows[c.activeWindow]
	w := c.windows[from]
	rest := append(append([]*Window{}, c.windows[:from]...), c.windows[from+1:]...)
	windows := append(append([]*Window{}, rest[:to]...), w)
	windows = append(windows, rest[to:]...)
	c.windows = windows
	c.activeWindow = c.windowIndex(active)
}

func (c *Core) switchWindow(delta int) {
	if len(c.windows) < 2 {
		return
	}
	n := len(c.windows)
	c.setActiveWindowIndex(((c.activeWindow+delta)%n + n) % n)
}

func (c *Core) selectWindowIndex(i int) {
	c.setActiveWindowIndex(i)
}

// setActiveWindowIndex is the single place every deliberate "look at a
// different window" action funnels through — Ctrl-B n/p/0-9, the jump
// picker, global search, the overview, new-window, break-pane — so
// lastWindow (Ctrl-B l's target) is always the window being left,
// recorded here rather than duplicated at each call site. Reordering
// (moveWindow) and a forced switch after the current window closes
// (removeWindow) are deliberately NOT routed through this: neither is
// "you chose to look elsewhere," so neither should overwrite lastWindow.
// Always calls afterWindowSwitch, even re-selecting the window already
// active — several callers rely on that to refresh w.active/activity
// after just changing which *pane* is focused within the same window.
func (c *Core) setActiveWindowIndex(idx int) {
	if idx < 0 || idx >= len(c.windows) {
		return
	}
	if idx != c.activeWindow {
		c.lastWindow = c.windows[c.activeWindow]
	}
	c.activeWindow = idx
	c.afterWindowSwitch()
}

// toggleLastWindow jumps back to whichever window was active right
// before the current one — tmux's Ctrl-B l — flipping between the two
// like Alt-Tab. A no-op if there's no recorded last window, or it's
// since been closed.
func (c *Core) toggleLastWindow() {
	if c.lastWindow == nil {
		return
	}
	idx := c.windowIndex(c.lastWindow)
	if idx < 0 {
		c.lastWindow = nil
		return
	}
	c.setActiveWindowIndex(idx)
}

// afterWindowSwitch clears state that's tied to whichever pane/window you
// were just looking at — copy-mode and an in-flight mouse drag don't
// carry over when you jump to a different window — and clears the
// activity flag on the window you just switched to, the same way tmux
// stops flagging a window the moment you look at it.
func (c *Core) afterWindowSwitch() {
	c.win().activity = false
	c.touchPane(c.win().active.ID)
	c.updateFocusEvents(c.win().active.ID)
	c.copy = copyState{}
	if c.mode == ModeCopy {
		c.mode = ModeNormal
	}
	c.drag = nil
	c.tabDrag = nil
	c.contentPress = nil
	c.titleDrag = nil
	c.mouseDown = false
	c.lastTitleClickID = 0
}

// confirmKillWindow asks before killWindow actually runs: closing a
// window takes every pane in it down at once, with no undo, so — unlike
// closing a single pane with 'x' — it's worth one extra keypress to catch
// a stray 'x'-shaped typo. y/Y confirms, anything else (including Esc)
// cancels; see handleConfirmKey.
func (c *Core) confirmKillWindow() {
	w := c.win()
	n := len(layout.Leaves(w.root))
	plural := "s"
	if n == 1 {
		plural = ""
	}
	c.mode = ModeConfirm
	// The name is whatever the user called it (or whatever is running in
	// it), so it's unbounded — and this prompt has to stay readable on
	// one status row, where anything past the end is simply not drawn.
	c.statusMsg = fmt.Sprintf("kill window %q (%d pane%s)? (y/n)", truncateRunes(c.windowDisplayName(w), 24), n, plural)
	c.pendingConfirm = c.killWindow
}

// confirmQuit asks before requestQuit actually runs: quitting closes
// every window and every pane in the whole session at once, with no
// undo — strictly more destructive than confirmKillWindow's single
// window, so it gets the same y/n treatment rather than the immediate,
// no-confirmation "x close-pane" convention.
func (c *Core) confirmQuit() {
	windowN := len(c.windows)
	windowPlural := "s"
	if windowN == 1 {
		windowPlural = ""
	}
	paneN := len(c.panes)
	panePlural := "s"
	if paneN == 1 {
		panePlural = ""
	}
	c.mode = ModeConfirm
	// Kept short on purpose: this has to fit on one status row next to
	// the session name, and it's the end of it — "(y/n)" — that says what
	// to press. You already know it's termdock asking.
	c.statusMsg = fmt.Sprintf("quit? %d window%s, %d pane%s (y/n)", windowN, windowPlural, paneN, panePlural)
	c.pendingConfirm = c.requestQuit
}

// killWindow closes every pane in the active window and removes it.
func (c *Core) killWindow() {
	w := c.win()
	for _, l := range layout.Leaves(w.root) {
		if p, ok := c.panes[l.ID]; ok {
			p.Close()
			delete(c.panes, l.ID)
		}
	}
	c.removeWindow(c.activeWindow)
}

// removeWindow drops window idx from the list (its panes must already be
// closed) and quits the session if that was the last one.
func (c *Core) removeWindow(idx int) {
	// Tracked by identity, not by re-deriving an index: idx is almost
	// always c.activeWindow itself (killWindow always closes the window
	// you're looking at), but movePaneToWindow can also remove a window
	// that emptied out *without* being the active one — a naive "clamp
	// activeWindow to the new length" would silently point it at the
	// wrong window whenever idx falls before it.
	active := c.windows[c.activeWindow]
	removed := c.windows[idx]
	c.windows = append(c.windows[:idx], c.windows[idx+1:]...)
	if c.lastWindow == removed {
		c.lastWindow = nil // Ctrl-B l has nothing to flip back to once it's gone
	}
	if len(c.windows) == 0 {
		c.requestQuit()
		return
	}
	if i := c.windowIndex(active); i >= 0 {
		c.activeWindow = i
	} else if c.activeWindow >= len(c.windows) {
		c.activeWindow = len(c.windows) - 1
	}
	c.afterWindowSwitch()
	c.relayoutLocked()
	c.persistStateLocked()
}

// movePaneToWindow relocates leaf — currently part of from's layout tree
// — into to, splitting to's active pane to make room. Only the tree
// wiring changes; the pane's process is left completely alone, the same
// way dragging a browser tab into another window doesn't reload the
// page. Reports whether it actually happened: false (a no-op) if leaf's
// pane already closed out from under a slow drag, from == to, to is
// currently zoomed (there'd be nowhere on screen to show the split), or
// there's no room left to split to's active pane.
func (c *Core) movePaneToWindow(leaf *layout.Node, from, to *Window) bool {
	if from == to {
		return false
	}
	if _, ok := c.panes[leaf.ID]; !ok {
		return false
	}
	if to.zoomed != nil {
		c.statusMsg = "exit zoom in the target window before moving a pane into it"
		return false
	}

	newLeaf, ok := layout.Split(to.active, layout.Horizontal, leaf.ID, leaf.Pane)
	if !ok {
		newLeaf, ok = layout.Split(to.active, layout.Vertical, leaf.ID, leaf.Pane)
	}
	if !ok {
		c.statusMsg = "not enough room in that window to add the pane"
		return false
	}

	if from.zoomed == leaf {
		from.zoomed = nil
	}
	if from.lastActivePane == leaf.ID {
		from.lastActivePane = 0 // Ctrl-B ; has nothing to flip back to once it's in a different window
	}
	newRoot, next := layout.Remove(from.root, leaf)
	if newRoot == nil {
		// leaf was from's last pane; the window itself is gone. Its
		// process already lives on in to's tree via newLeaf above, so
		// this is purely bookkeeping, unlike killWindow's closing panes
		// down before calling removeWindow.
		if idx := c.windowIndex(from); idx >= 0 {
			c.removeWindow(idx)
		}
	} else {
		from.root = newRoot
		if from.active == leaf {
			from.active = next
		}
	}

	to.active = newLeaf
	c.relayoutLocked()
	c.persistStateLocked()
	return true
}

// breakPaneToNewWindow pulls the active pane out of the active window's
// split tree and gives it a brand new window all its own — tmux's
// break-pane (bound to the same "!"). The reverse of movePaneToWindow
// with an existing target: here the leaf itself becomes a new window's
// root instead of grafting into one that's already there.
func (c *Core) breakPaneToNewWindow() {
	w := c.win()
	leaf := w.active
	if len(layout.Leaves(w.root)) < 2 {
		c.statusMsg = "this pane is already alone in its window"
		return
	}

	newRoot, next := layout.Remove(w.root, leaf)
	w.root = newRoot // never nil: the length check above guarantees at least one sibling survives
	if w.active == leaf {
		w.active = next
	}
	if w.lastActivePane == leaf.ID {
		w.lastActivePane = 0 // Ctrl-B ; has nothing to flip back to once it's in a different window
	}
	// Zoom is always on the active pane (setActive carries it along), so
	// the pane being broken out is the zoomed one whenever the window is
	// zoomed at all. Left set, it would point into the *new* window's
	// tree: relayout would then resize that pane to this window's
	// dimensions, and coming back here would render it as this window's
	// only pane, hiding the ones that actually live here.
	if w.zoomed == leaf {
		w.zoomed = nil
	}
	leaf.Parent = nil

	nw := &Window{ID: c.nextWindowID, root: leaf, active: leaf}
	c.nextWindowID++
	c.lastWindow = w
	c.windows = append(c.windows, nw)
	c.activeWindow = len(c.windows) - 1
	c.afterWindowSwitch()
	c.relayoutLocked()
	c.persistStateLocked()
}
