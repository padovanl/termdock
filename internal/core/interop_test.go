package core

import (
	"testing"

	"termdock/internal/layout"
)

// This file specifically exercises interactions BETWEEN this round's new
// features (logging, quick-jump, command prompt, layout presets,
// respawn) rather than any one of them in isolation — the same panes,
// windows, and processes are shared state every feature touches, so
// it's worth pinning down what happens when one acts on something
// another just set up.

// TestLoggingSurvivesBreakPaneToNewWindow: break-pane only rewires the
// layout tree (see breakPaneToNewWindow) — the underlying *pane.Pane
// object, and whatever it's logging to, is untouched, the same
// process-survives-the-move guarantee movePaneToWindow already gives.
func TestLoggingSurvivesBreakPaneToNewWindow(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	paneID := c.win().active.ID
	c.toggleLogging()
	path, logging := c.panes[paneID].LogPath()
	c.mu.Unlock()
	if !logging {
		t.Fatal("setup: expected logging to have started")
	}

	c.mu.Lock()
	c.breakPaneToNewWindow()
	gotPath, stillLogging := c.panes[paneID].LogPath()
	c.mu.Unlock()

	if !stillLogging || gotPath != path {
		t.Fatalf("logging should survive break-pane (same process, new window): logging=%v path=%q, want %q", stillLogging, gotPath, path)
	}
}

// TestRespawnStopsLoggingOnTheOldPaneAndDoesNotCarryOver: respawn
// replaces the pane's process outright (see respawnActivePane), so
// there's no "the same shell, logging continues" story here the way
// there is for break-pane above — the old pane (and its log file handle)
// is closed via Pane.Close, and the fresh one that takes its place
// starts with nothing enabled, the same as a brand new split would.
func TestRespawnStopsLoggingOnTheOldPaneAndDoesNotCarryOver(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	oldID := c.win().active.ID
	c.toggleLogging()
	_, logging := c.panes[oldID].LogPath()
	c.mu.Unlock()
	if !logging {
		t.Fatal("setup: expected logging to have started")
	}

	c.mu.Lock()
	c.respawnActivePane()
	newID := c.win().active.ID
	_, newLogging := c.panes[newID].LogPath()
	c.mu.Unlock()

	if newID == oldID {
		t.Fatal("sanity check failed: respawn should have assigned a new pane ID")
	}
	if newLogging {
		t.Fatal("a respawned pane should start fresh, not inherit the old pane's logging")
	}
}

// TestQuickJumpCapsAtNinePanes checks the display-panes-style digit cap
// (1-9) holds even when a window has more panes than that — built
// directly against the layout tree (bypassing real ptys/MinWidth splitting
// constraints, the same way layoutpreset_test.go's fakePaneHost tests
// do) since forcing 10+ *real* panes to coexist in one 80-column terminal
// would fight Split's own MinWidth guard rather than testing anything
// about quick-jump itself.
func TestQuickJumpCapsAtNinePanes(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	refs := make([]leafRef, 12)
	for i := range refs {
		refs[i] = leafRef{id: 1000 + i, p: fakePaneHost{}}
	}
	root := buildLeafChain(refs, layout.Vertical)
	c.win().root = root
	c.win().active = layout.FirstLeaf(root)
	c.relayoutLocked()
	c.enterQuickJump()
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeQuickJump {
		t.Fatalf("enterQuickJump with 12 panes: mode = %v, want ModeQuickJump", mode)
	}

	f := c.Frame()
	if len(f.QuickJump) != 9 {
		t.Fatalf("QuickJump tags = %d, want capped at 9", len(f.QuickJump))
	}
	for i, tag := range f.QuickJump {
		if tag.Digit != rune('1'+i) {
			t.Fatalf("tag %d digit = %q, want %q", i, tag.Digit, rune('1'+i))
		}
	}
}

// TestCommandPromptAndDirectKeybindingsAgree: the command-prompt verbs
// (kill-pane, break-pane, respawn-pane — see runCommand) are meant to be
// exactly the same actions as their direct keybindings (x, !, R), not a
// parallel implementation that could drift out of sync. Exercising
// kill-pane here (the others already have their own direct tests) checks
// runCommand really calls killActive rather than reimplementing it.
func TestCommandPromptKillPaneMatchesDirectKeybinding(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	targetID := c.win().active.ID
	c.runCommand("kill-pane")
	_, stillTracked := c.panes[targetID]
	c.mu.Unlock()

	if stillTracked {
		t.Fatal("runCommand(\"kill-pane\") should close the active pane exactly like the 'x' keybinding")
	}
}
