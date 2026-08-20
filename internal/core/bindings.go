package core

import "sort"

// action names one of the prefix-key commands by a short, stable
// string — the vocabulary config.Config's "bind" setting rebinds keys
// to (see SetBindOverrides) and the help screen/cheat-sheet describe by
// (see liveHelpEntries in help.go / cheatSheet in frame.go), instead of
// only being reachable through a hardcoded switch on whichever key
// happens to trigger it today.
//
// Deliberately scoped to the top-level Ctrl-B-prefixed dispatch only:
// sub-mode keys (copy-mode's h/j/k/l/v/V/y/..., the popup's narrow key
// set, a picker's type-ahead, ...) aren't part of this and can't be
// rebound — remapping *those* would mean rethinking a dozen different
// modal key handlers, each with its own reasons for what it accepts,
// for far less real-world benefit than the one Ctrl-B dispatch table
// everyone actually reaches for.
type action string

const (
	actVSplit        action = "vsplit"
	actHSplit        action = "hsplit"
	actFocusLeft     action = "focus-left"
	actFocusRight    action = "focus-right"
	actFocusUp       action = "focus-up"
	actFocusDown     action = "focus-down"
	actCycleFocus    action = "cycle-focus"
	actClosePane     action = "close-pane"
	actZoom          action = "zoom"
	actResizeMode    action = "resize-mode"
	actCopyMode      action = "copy-mode"
	actSyncPanes     action = "sync-panes"
	actNewWindow     action = "new-window"
	actNextWindow    action = "next-window"
	actPrevWindow    action = "prev-window"
	actJumpPicker    action = "jump-picker"
	actLastWindow    action = "last-window"
	actLastPane      action = "last-pane"
	actOverview      action = "overview"
	actGlobalSearch  action = "search"
	actSwitchSession action = "switch-session"
	actPopup         action = "popup"
	actOpener        action = "open-link"
	actBreakPane     action = "break-pane"
	actQuickJump     action = "quick-jump"
	actCommandPrompt action = "command-prompt"
	actCycleLayout   action = "cycle-layout"
	actRespawnPane   action = "respawn-pane"
	actToggleLogging action = "toggle-logging"
	actRenameWindow  action = "rename-window"
	actKillWindow    action = "kill-window"
	actPaste         action = "paste"
	actPastePicker   action = "paste-picker"
	actDetach        action = "detach"
	actQuit          action = "quit"
	actHelp          action = "help"
)

// defaultBindings is termdock's out-of-the-box rune -> action map,
// exactly matching the keys documented in the README's keybinding
// table. config.Config's "bind" setting (see SetBindOverrides) can
// override individual entries; anything not mentioned there keeps its
// default here.
var defaultBindings = map[rune]action{
	'v': actVSplit, '%': actVSplit,
	's': actHSplit, '"': actHSplit,
	'h': actFocusLeft,
	'l': actFocusRight,
	'k': actFocusUp,
	'j': actFocusDown,
	'o': actCycleFocus,
	'x': actClosePane,
	'z': actZoom,
	'r': actResizeMode,
	'[': actCopyMode,
	'y': actSyncPanes,
	'c': actNewWindow,
	'n': actNextWindow,
	'p': actPrevWindow,
	'w': actJumpPicker,
	'W': actLastWindow,
	';': actLastPane,
	'g': actOverview,
	'/': actGlobalSearch,
	'S': actSwitchSession,
	'P': actPopup,
	'u': actOpener,
	'!': actBreakPane,
	'Q': actQuickJump,
	':': actCommandPrompt,
	' ': actCycleLayout,
	'R': actRespawnPane,
	'L': actToggleLogging,
	',': actRenameWindow,
	'&': actKillWindow,
	']': actPaste,
	'=': actPastePicker,
	'd': actDetach,
	'q': actQuit,
	'?': actHelp,
}

// actionOrder fixes the display order for the help screen and the
// prefix-held cheat-sheet — roughly the order the README's own
// keybinding table lists them in.
var actionOrder = []action{
	actVSplit, actHSplit,
	actFocusLeft, actFocusDown, actFocusUp, actFocusRight,
	actCycleFocus, actZoom, actResizeMode,
	actCopyMode, actPaste, actPastePicker,
	actSyncPanes, actNewWindow, actNextWindow, actPrevWindow,
	actJumpPicker, actLastWindow, actLastPane, actOverview,
	actGlobalSearch, actSwitchSession, actPopup, actOpener,
	actBreakPane, actQuickJump, actCommandPrompt, actCycleLayout,
	actRespawnPane, actToggleLogging, actRenameWindow, actKillWindow,
	actClosePane, actDetach, actQuit, actHelp,
}

// actionDescriptions is the full-sentence description shown on the
// help screen (Ctrl-B ?).
var actionDescriptions = map[action]string{
	actVSplit:        "split vertically (side by side)",
	actHSplit:        "split horizontally (stacked)",
	actFocusLeft:     "move focus left (arrows also always work, regardless of rebinding)",
	actFocusRight:    "move focus right (arrows also always work, regardless of rebinding)",
	actFocusUp:       "move focus up (arrows also always work, regardless of rebinding)",
	actFocusDown:     "move focus down (arrows also always work, regardless of rebinding)",
	actCycleFocus:    "cycle to the next pane (Tab also always works, regardless of rebinding)",
	actZoom:          "zoom the active pane full-screen (again to undo)",
	actResizeMode:    "resize-mode: arrows/hjkl resize, any other key exits",
	actCopyMode:      "enter copy-mode (scroll/select the scrollback)",
	actPaste:         "paste the last yank",
	actPastePicker:   "paste register picker: fuzzy-pick an older yank to paste",
	actSyncPanes:     "toggle sync-panes (type into every pane at once)",
	actNewWindow:     "create a new window",
	actNextWindow:    "switch to the next window",
	actPrevWindow:    "switch to the previous window",
	actJumpPicker:    "jump picker: fuzzy-jump to any window/pane",
	actLastWindow:    "toggle back to the previously active window",
	actLastPane:      "toggle back to the previously active pane in this window",
	actOverview:      "overview: a live-thumbnail grid of every pane",
	actGlobalSearch:  "search every pane's scrollback at once",
	actSwitchSession: "switch to another session without detaching",
	actPopup:         "toggle the floating scratch terminal",
	actOpener:        "open picker: fuzzy-pick a URL/path spotted on screen",
	actBreakPane:     "break the active pane out into its own new window",
	actQuickJump:     "quick-jump: press a pane's number to jump straight to it",
	actCommandPrompt: "command prompt: type a command (new-window, split-window, ...)",
	actCycleLayout:   "cycle the active window through preset layouts",
	actRespawnPane:   "respawn-pane: restart the shell in the active pane, in place",
	actToggleLogging: "toggle logging the active pane's output to a file",
	actRenameWindow:  "rename the current window",
	actKillWindow:    "close the current window and every pane in it (asks first)",
	actClosePane:     "close the active pane",
	actDetach:        "detach (session keeps running)",
	actQuit:          "quit (asks first; ends the whole session)",
	actHelp:          "toggle this help screen",
}

// actionShort is the terse label used in the prefix-held cheat-sheet
// (see (*Core).cheatSheet) — short enough that the whole thing still
// fits on one status-bar line at a normal terminal width.
var actionShort = map[action]string{
	actVSplit:        "vsplit",
	actHSplit:        "hsplit",
	actFocusLeft:     "left",
	actFocusRight:    "right",
	actFocusUp:       "up",
	actFocusDown:     "down",
	actCycleFocus:    "cycle",
	actZoom:          "zoom",
	actResizeMode:    "resize",
	actCopyMode:      "copy",
	actPaste:         "paste",
	actPastePicker:   "registers",
	actSyncPanes:     "sync",
	actNewWindow:     "new-win",
	actNextWindow:    "next-win",
	actPrevWindow:    "prev-win",
	actJumpPicker:    "jump",
	actLastWindow:    "last-win",
	actLastPane:      "last-pane",
	actOverview:      "overview",
	actGlobalSearch:  "search",
	actSwitchSession: "sessions",
	actPopup:         "popup",
	actOpener:        "open link",
	actBreakPane:     "break-pane",
	actQuickJump:     "quick-jump",
	actCommandPrompt: "command",
	actCycleLayout:   "layout",
	actRespawnPane:   "respawn",
	actToggleLogging: "log",
	actRenameWindow:  "rename",
	actKillWindow:    "kill-win",
	actClosePane:     "close-pane",
	actDetach:        "detach",
	actQuit:          "quit",
	actHelp:          "help",
}

// validActions is every action name SetBindOverrides accepts — anything
// else in a "bind" config line is ignored, the same "bad setting, keep
// the default" leniency every other termdock.conf key already has.
var validActions = func() map[action]bool {
	m := make(map[action]bool, len(actionOrder))
	for _, a := range actionOrder {
		m[a] = true
	}
	return m
}()

func cloneBindings(src map[rune]action) map[rune]action {
	dst := make(map[rune]action, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// keyLabel pretty-prints a rune the way the help screen/cheat-sheet
// show it — ' ' has no visible glyph of its own, so it's spelled out.
func keyLabel(r rune) string {
	if r == ' ' {
		return "Space"
	}
	return string(r)
}

// keysForAction returns every key currently bound to act (there can be
// more than one, e.g. v and % both default to vsplit — or zero, if a
// "bind" line moved every key that used to trigger it elsewhere),
// sorted for a stable display order.
func keysForAction(bindings map[rune]action, act action) []rune {
	var keys []rune
	for r, a := range bindings {
		if a == act {
			keys = append(keys, r)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
