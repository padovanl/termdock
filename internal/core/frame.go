package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/vt10x"
)

// Frame snapshots everything an attached client needs to paint: it never
// touches a real terminal itself, which is what lets any number of
// clients attach, detach and reattach to the same running session.
func (c *Core) Frame() proto.Frame {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Frame already samples every pane's foreground process to title it,
	// so the "has it finished?" check rides along rather than polling.
	c.checkDoneWatches()

	f := proto.Frame{
		Cols:        c.cols,
		Rows:        c.rows,
		ShowStatus:  c.statusRows() > 0,
		SessionName: c.SessionName,
	}

	if len(c.windows) == 0 {
		// The session's last window just closed (Exited() is about to
		// fire, or just did): nothing left to render. Guards a real race
		// against the periodic clock broadcast, which isn't otherwise
		// synchronized with window teardown.
		return f
	}

	w := c.win()
	if w.zoomed != nil {
		f.Panes = append(f.Panes, c.buildPaneFrame(w.zoomed, true, 1, true))
	} else {
		for i, leaf := range layout.Leaves(w.root) {
			f.Panes = append(f.Panes, c.buildPaneFrame(leaf, leaf == w.active, i+1, false))
		}
	}

	f.StatusPrefix = c.statusPrefix()
	f.Windows = c.windowTabs()
	f.StatusText, f.StatusRight, f.StatusStyle = c.statusLine()
	for _, provider := range []func() *proto.Overlay{
		c.pickerOverlay, c.helpOverlay, c.settingsOverlay, c.historyOverlay, c.registersOverlay, c.sessionsOverlay, c.searchOverlay, c.openerOverlay,
	} {
		if f.Overlay = provider(); f.Overlay != nil {
			break
		}
	}
	f.Overview = c.overviewFrame()
	f.Popup = c.popupFrame()
	f.QuickJump = c.quickJumpFrame()
	f.Settings = c.clientSettings()
	return f
}

// idx is the pane's 1-based position within its window (left-to-right,
// top-to-bottom), not its internal ID: the ID is a session-wide counter
// that never resets or reuses a number, so after a bit of splitting and
// closing panes it stops meaning anything spatial ("why is this pane
// called 7 when there are only two panes?"). idx is what's shown in the
// title bar and status line instead — small, stable-within-the-window,
// and lines up with what the user can actually see and count.
func (c *Core) buildPaneFrame(n *layout.Node, active bool, idx int, zoomed bool) proto.PaneFrame {
	pf := proto.PaneFrame{
		ID:     n.ID,
		Rect:   proto.Rect(n.Rect),
		Active: active,
		Zoomed: zoomed,
	}
	p, ok := c.panes[n.ID]
	if !ok {
		return pf
	}
	pf.Title = c.paneTitle(idx, p)
	if zoomed {
		pf.Title += " [Z]"
	}
	if _, logging := p.LogPath(); logging {
		pf.Title += " [REC]"
	}

	if active && c.mode == ModeCopy && c.copy.paneID == n.ID {
		return c.buildCopyFrame(n, p, pf)
	}

	cr := n.Rect
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()

	cells := make([][]proto.Cell, cr.H)
	for y := 0; y < cr.H; y++ {
		row := make([]proto.Cell, cr.W)
		if y < rows {
			for x := 0; x < cr.W && x < cols; x++ {
				row[x] = glyphToCell(t.Cell(x, y))
			}
		}
		cells[y] = row
	}
	pf.Cells = cells

	if active {
		pf.CursorVisible = t.CursorVisible()
		cur := t.Cursor()
		pf.CursorX = cr.X + cur.X
		pf.CursorY = cr.Y + cur.Y
	}
	return pf
}

func (c *Core) buildCopyFrame(n *layout.Node, p *pane.Pane, pf proto.PaneFrame) proto.PaneFrame {
	cr := n.Rect
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, _ := t.Size()
	hl := t.HistoryLen()

	cells := make([][]proto.Cell, cr.H)
	for y := 0; y < cr.H; y++ {
		absY := c.copy.top + y
		row := make([]proto.Cell, cr.W)
		for x := 0; x < cr.W && x < cols; x++ {
			cell := glyphToCell(cellAt(t, hl, absY, x))
			if c.selected(x, absY) {
				cell.Attr |= pane.AttrReverse
			}
			row[x] = cell
		}
		cells[y] = row
	}
	pf.Cells = cells
	// The copy cursor doesn't have to be on screen: the wheel scrolls the
	// viewport and deliberately leaves the cursor where it was, so it can
	// easily sit above or below what's currently shown. Drawing it anyway
	// put the terminal's real cursor outside this pane's rect — over a
	// neighboring pane, or on the status bar — so it's simply hidden while
	// it's out of view, the same as any editor scrolled away from its
	// caret.
	row := c.copy.curY - c.copy.top
	pf.CursorVisible = row >= 0 && row < cr.H
	pf.CursorX = cr.X + clampi(c.copy.curX, 0, maxi(0, cr.W-1))
	pf.CursorY = cr.Y + row
	return pf
}

func glyphToCell(g vt10x.Glyph) proto.Cell {
	return proto.Cell{Ch: g.Char, FG: uint32(g.FG), BG: uint32(g.BG), Attr: uint16(g.Mode)}
}

func (c *Core) paneTitle(idx int, p *pane.Pane) string {
	name := c.shellName
	if fg := p.ForegroundTitle(); fg != "" {
		name = fg
	}
	return fmt.Sprintf("%d:%s%s%s", idx, name, c.lastCommandStatus(p.ID), c.watchedPaneMarker(p.ID))
}

// cheatSheet is the terse keybinding cheat-sheet shown transiently
// while the prefix key is held — "Ctrl-B ?" is what the idle status
// bar tells you to press instead, for the full, one-line-per-binding
// reference (see help.go). Built from the session's actual current
// bindings (defaults, overridden per-key by config's "bind" setting)
// rather than a hardcoded string, so a rebound key shows up correctly
// here too instead of the cheat-sheet quietly lying about what a key
// does. hjkl/arrows and o/Tab are shown grouped only when all of a
// group's keys are still on their defaults — a partial rebind (say,
// just 'h' moved elsewhere) falls back to listing each surviving
// binding on its own instead of a group label that would no longer be
// accurate.
func (c *Core) cheatSheet() string {
	var parts []string
	if movementIsDefault(c.bindings) {
		parts = append(parts, "hjkl/arrows move")
	} else {
		for _, act := range []action{actFocusLeft, actFocusDown, actFocusUp, actFocusRight} {
			for _, r := range keysForAction(c.bindings, act) {
				parts = append(parts, keyLabel(r)+" "+actionShort[act])
			}
		}
	}
	if cycleIsDefault(c.bindings) {
		parts = append(parts, "o/Tab cycle")
	} else {
		for _, r := range keysForAction(c.bindings, actCycleFocus) {
			parts = append(parts, keyLabel(r)+" cycle")
		}
	}
	for _, act := range actionOrder {
		switch act {
		case actFocusLeft, actFocusRight, actFocusUp, actFocusDown, actCycleFocus:
			continue // handled above, grouped when possible
		}
		keys := keysForAction(c.bindings, act)
		if len(keys) == 0 {
			continue
		}
		labels := make([]string, len(keys))
		for i, r := range keys {
			labels[i] = keyLabel(r)
		}
		parts = append(parts, strings.Join(labels, "/")+" "+actionShort[act])
	}
	parts = append(parts, "0-9 win#")
	return strings.Join(parts, " | ")
}

func movementIsDefault(b map[rune]action) bool {
	return b['h'] == actFocusLeft && b['j'] == actFocusDown && b['k'] == actFocusUp && b['l'] == actFocusRight
}

func cycleIsDefault(b map[rune]action) bool {
	return b['o'] == actCycleFocus
}

// statusHint picks the message for the status bar and the style it's
// drawn in. Split out of statusLine so tabBudget can ask what the
// message will be without building the whole line.
func (c *Core) statusHint() (hint, style string) {
	// Minimal at rest — like tmux's default status line, not a permanent
	// cheat-sheet — so there's room left for the clock; the full binding
	// list only shows up once you've actually pressed the prefix, when
	// you're presumably looking for it.
	hint = "Ctrl-B ?"
	style = "normal"

	switch {
	case c.mode == ModeInput:
		style = "mode"
		hint = c.inputHintText()
	case c.mode == ModeCopy || c.mode == ModeResize:
		style = "mode"
		hint = c.statusMsg
	case c.mode == ModeConfirm:
		style = "confirm"
		hint = c.statusMsg
	case c.mode == ModePicker:
		style = "mode"
		hint = "type to filter, ↑↓/Tab select, enter jump, esc cancel"
	case c.mode == ModeHelp:
		style = "mode"
		hint = "↑↓/jk/PgUp/PgDn scroll, any other key closes"
	case c.mode == ModeRegisters:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter paste, esc cancel"
	case c.mode == ModeSessions:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter switch, esc cancel"
	case c.mode == ModeSearch:
		style = "mode"
		hint = "type to search every pane's scrollback (regex or text), ↑↓ select, enter jump, esc cancel"
	case c.mode == ModeOverview:
		style = "mode"
		hint = "arrows/hjkl move, click or enter jump, esc cancel"
	case c.mode == ModePopup:
		style = "mode"
		hint = "typing goes to the popup — Ctrl-B P hides it, Ctrl-B d/q still work"
	case c.mode == ModeOpener:
		style = "mode"
		hint = "type to filter, ↑↓ select, enter copies to clipboard, esc cancel"
	case c.mode == ModeSettings:
		style = "mode"
		hint = c.settingsHint()
	case c.mode == ModeQuickJump:
		style = "mode"
		hint = "press a pane's number to jump there, any other key cancels"
	case c.prefix:
		style = "prefix"
		hint = "PREFIX > " + c.cheatSheet()
	case c.statusMsg != "":
		hint = c.statusMsg
	}
	return hint, style
}

func (c *Core) statusLine() (text, right, style string) {
	hint, style := c.statusHint()
	text = c.statusLeftText(hint, style)

	right = time.Now().Format("15:04 02-Jan-06")
	if c.hostname != "" {
		right = c.hostname + " " + right
	}
	if seg := c.statusSegmentsText(); seg != "" {
		right = seg + " | " + right
	}
	right += " "
	return text, right, style
}

// statusLeftText is the part of the status bar drawn after the window
// tab strip. Deliberately cheap — no subprocesses, unlike the right-hand
// side's git segment — because the tab strip is laid out against its
// width (see tabBudget) and mouse hit-testing recomputes that on every
// click.
//
// A prompt — a confirm, or a line being typed — drops the "active pane"
// preamble. Both are asking for something, and which pane you're on is
// not what you need to read at that moment; on an 80-column row those
// twenty columns are the difference between seeing the end of the
// question and not.
func (c *Core) statusLeftText(hint, style string) string {
	if style == "confirm" || c.mode == ModeInput {
		return " " + hint
	}
	w := c.win()
	sync := ""
	if w.syncPanes {
		sync = " [SYNC]"
	}
	return fmt.Sprintf(" | active pane: %d%s | %s", activePaneIndex(w), sync, hint)
}

// tabBudget is how many columns the window tab strip may occupy.
//
// Normally: all of them. The status message at rest is a hint ("Ctrl-B
// ?") and which windows exist matters more than repeating it, so the
// tabs take the row and the hint gets whatever is left, as they always
// have.
//
// The exception is a prompt — a confirm asking y/n, or a line being
// typed. Those have to be read to be answered, and the tab strip used to
// push them clean off the end of the row: from about eight windows on, a
// confirm prompt was drawn entirely off screen, so termdock sat waiting
// for a y/n that nothing on screen had asked for, and the next keystroke
// either destroyed something or didn't with no way to tell which. Then
// the message gets its width first and the strip takes what's left —
// never less than the active window's own tab, so you can always see
// where you are.
func (c *Core) tabBudget() int {
	avail := maxi(c.cols-len([]rune(c.statusPrefix())), 0)
	if c.mode != ModeConfirm && c.mode != ModeInput {
		return avail
	}
	hint, style := c.statusHint()
	budget := avail - len([]rune(c.statusLeftText(hint, style)))
	// No floor for either kind of prompt: both have to be readable to be
	// answered, so on a row too narrow for both, the prompt wins and the
	// strip goes entirely. It comes straight back the moment the prompt
	// is answered.
	return maxi(budget, 0)
}

func (c *Core) activeTabWidth() int {
	if c.activeWindow < 0 || c.activeWindow >= len(c.windows) {
		return 0
	}
	return len([]rune(c.windowTabLabel(c.activeWindow)))
}

// activePaneIndex returns the active pane's 1-based position within its
// window (matching the number shown in its title bar; see
// Core.buildPaneFrame), or 1 when zoomed, since zoom always shows exactly
// the one pane.
func activePaneIndex(w *Window) int {
	if w.zoomed != nil {
		return 1
	}
	for i, l := range layout.Leaves(w.root) {
		if l == w.active {
			return i + 1
		}
	}
	return 1
}

// statusPrefix is the lead-in text on the status bar's left side, drawn
// before the window tab strip.
//
// It gives way entirely to a confirm prompt the row is too narrow to
// hold both of: the prompt is a question whose answer destroys
// something, and the end of it is where "(y/n)" lives, while the session
// name is something you already know. Anywhere with room for both, both
// are shown.
func (c *Core) statusPrefix() string {
	name := c.sessionLabel()
	if c.mode == ModeConfirm || c.mode == ModeInput {
		hint, style := c.statusHint()
		if len([]rune(name))+len([]rune(c.statusLeftText(hint, style))) > c.cols {
			return ""
		}
	}
	return name
}

// sessionLabel is the session name as the status bar shows it. The name
// is user data (see rename-session), so it gets the same cap a window
// name does — one long one shouldn't be able to take the whole row.
func (c *Core) sessionLabel() string {
	return fmt.Sprintf(" termdock:%s ", truncateRunes(c.SessionName, maxTabNameWidth))
}

// inputHintText renders a line being typed — the prompt, what's been
// typed so far, and the cursor — scrolled so the end stays visible.
//
// What you type has no length limit (a ":" command line, a window name),
// while the status row does, and the row is drawn left to right and
// clipped at the edge. So past about half a screenful the cursor and
// every character just typed were simply not on screen: you were typing
// blind into a prompt that appeared frozen. Scrolling keeps the tail in
// view, with a leading ellipsis where text has scrolled off, which is
// what any single-line text field does.
func (c *Core) inputHintText() string {
	prompt := c.input.prompt
	buf := c.input.buffer
	// Sized against the bare row, ignoring the session name and the tab
	// strip: both of those give way to a prompt when the row is tight
	// (see statusPrefix and tabBudget), so measuring against them instead
	// would be circular — and this way the prompt is guaranteed to fit on
	// its own line whatever else is going on.
	room := c.cols - len([]rune(prompt)) - 2
	if room < 8 {
		room = 8 // narrower than this and scrolling stops meaning anything
	}
	if len(buf) <= room {
		return prompt + string(buf) + "_"
	}
	return prompt + "…" + string(buf[len(buf)-room+1:]) + "_"
}

// maxTabNameWidth caps how much of a window's name its tab shows.
// Generous enough that ordinary names ("editor", "npm run dev") are
// never touched, small enough that one absurd name can't take the row.
const maxTabNameWidth = 24

// windowTabLabel renders one window's tab text. "*" marks the window
// you're looking at, "!" marks one that produced output while you
// weren't — tmux's monitor-activity, on by default here since there's no
// window list UI to toggle it from otherwise.
func (c *Core) windowTabLabel(i int) string {
	w := c.windows[i]
	// The name is user data — whatever rename-window was given, or the
	// foreground command — so it has no natural bound, and one long one
	// used to blow out the whole strip and take the status message with
	// it. tmux truncates window names in its status line for the same
	// reason.
	label := fmt.Sprintf(" %d:%s", i, truncateRunes(c.windowDisplayName(w), maxTabNameWidth))
	if w.activity {
		label += "!"
	}
	if i == c.activeWindow {
		label += "*"
	}
	return label + " "
}

// windowTabs lays out the status bar's window tab strip, one WindowTab
// per visible window with its exact display label and the column range
// it occupies — the single source of truth for both how the client draws
// it (colored per active/activity state) and how a click on it is
// hit-tested back to a window index (see handleNormalMouse), so the two
// can never drift apart.
//
// Only as many tabs as fit in tabBudget are laid out, as a run around
// the active window, which is always among them. Every tab is labelled
// with its own window number, so a strip starting at "3:" says plainly
// that there are windows before it — no separate "more this way" marker
// needed. Laying out all of them regardless is what used to push the
// status message clean off the end of the row.
func (c *Core) windowTabs() []proto.WindowTab {
	if len(c.windows) == 0 {
		return nil
	}
	labels := make([]string, len(c.windows))
	widths := make([]int, len(c.windows))
	total := 0
	for i := range c.windows {
		labels[i] = c.windowTabLabel(i)
		widths[i] = len([]rune(labels[i]))
		total += widths[i]
	}

	first, last := 0, len(c.windows)-1
	budget := c.tabBudget()
	if (c.mode == ModeConfirm || c.mode == ModeInput) && widths[c.activeWindow] > budget {
		// A prompt has already been given the row's width first (see
		// tabBudget). If even one tab won't fit in what's left, the strip
		// goes entirely rather than pushing the end of the prompt — the
		// "(y/n)", or the cursor you're typing at — past the screen edge.
		return nil
	}
	if total > budget {
		// Grow outward from the active window — right first, so the
		// common case of being on window 0 shows 0,1,2... rather than
		// stopping dead at 0.
		first, last = c.activeWindow, c.activeWindow
		used := widths[c.activeWindow]
		for {
			grew := false
			if last+1 < len(c.windows) && used+widths[last+1] <= budget {
				last++
				used += widths[last]
				grew = true
			}
			if first > 0 && used+widths[first-1] <= budget {
				first--
				used += widths[first]
				grew = true
			}
			if !grew {
				break
			}
		}
	}

	tabs := make([]proto.WindowTab, 0, last-first+1)
	x := len([]rune(c.statusPrefix()))
	for i := first; i <= last; i++ {
		tabs = append(tabs, proto.WindowTab{
			Index:    i,
			Label:    labels[i],
			Active:   i == c.activeWindow,
			Activity: c.windows[i].activity,
			Dragging: c.tabDrag != nil && c.tabDrag.win == c.windows[i],
			X:        x,
			W:        widths[i],
		})
		x += widths[i]
	}
	return tabs
}
