package core

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

func TestDefaultBindingsMatchEveryKnownAction(t *testing.T) {
	seen := map[action]bool{}
	for _, act := range defaultBindings {
		seen[act] = true
	}
	for _, act := range actionOrder {
		if !seen[act] {
			t.Errorf("action %q is in actionOrder but bound to no default key", act)
		}
	}
	for act := range actionDescriptions {
		if !validActions[act] {
			t.Errorf("actionDescriptions has an entry for %q, which isn't in actionOrder", act)
		}
	}
	for act := range actionShort {
		if !validActions[act] {
			t.Errorf("actionShort has an entry for %q, which isn't in actionOrder", act)
		}
	}
}

// pressPrefixThen mirrors a real client's two-keystroke Ctrl-B <key>
// sequence: handleKey locks c.mu itself, so — unlike the mode-specific
// handlers (handleQuickJumpKey and friends) other tests call directly
// under their own c.mu.Lock() — these calls must NOT be wrapped in one,
// or the second call would deadlock against the first's own lock/unlock.
func pressPrefixThen(c *Core, key tcell.Key, r rune) Result {
	c.handleKey(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)})
	return c.handleKey(proto.ClientMsg{Kind: "key", KeyCode: int32(key), KeyRune: r})
}

func TestSetBindOverridesRebindsAKeyEndToEnd(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'w': "quick-jump"})

	c.mu.Lock()
	c.doSplit(layout.Vertical) // a second pane, so quick-jump has something to show
	c.mu.Unlock()

	pressPrefixThen(c, tcell.KeyRune, 'w')

	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	if mode != ModeQuickJump {
		t.Fatalf("rebinding w to quick-jump: mode after Ctrl-B w = %v, want ModeQuickJump", mode)
	}
}

func TestSetBindOverridesLeavesOtherKeysAlone(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'w': "quick-jump"})

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	paneCountBefore := len(c.panes)
	c.mu.Unlock()

	pressPrefixThen(c, tcell.KeyRune, 'x') // still close-pane, untouched by the override above

	c.mu.Lock()
	paneCountAfter := len(c.panes)
	c.mu.Unlock()
	if paneCountAfter != paneCountBefore-1 {
		t.Fatalf("'x' should still close the active pane: had %d panes, now %d", paneCountBefore, paneCountAfter)
	}
}

func TestSetBindOverridesUnknownActionIsIgnored(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'w': "not-a-real-action"})

	c.mu.Lock()
	act := c.bindings['w']
	c.mu.Unlock()
	if act != actJumpPicker {
		t.Fatalf("an unknown action name should leave the key on its default, got %q", act)
	}
}

func TestActionUnreachableAfterFullRebindDisappearsFromHelp(t *testing.T) {
	c := newTestCore(t)
	// jump-picker's only default key is 'w'; moving it to a different
	// action leaves jump-picker completely unreachable, and
	// liveHelpEntries should reflect that instead of still advertising a
	// key that no longer does it.
	c.SetBindOverrides(map[rune]string{'w': "quick-jump"})

	c.mu.Lock()
	entries := c.liveHelpEntries()
	c.mu.Unlock()

	for _, e := range entries {
		if e.desc == actionDescriptions[actJumpPicker] {
			t.Fatalf("jump-picker's help entry should be gone once its only key is rebound away, found %+v", e)
		}
	}
}

func TestRebindReflectedInCheatSheet(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'M': "quick-jump"})

	c.mu.Lock()
	sheet := c.cheatSheet()
	c.mu.Unlock()

	// quick-jump now has two keys (its default 'Q' plus the new 'M'),
	// joined with "/" the same way v/% already are for vsplit — "M"
	// sorts before "Q", so "M/Q quick-jump" is the expected label.
	if !strings.Contains(sheet, "M/Q "+actionShort[actQuickJump]) {
		t.Fatalf("cheat-sheet should mention the rebound key, got %q", sheet)
	}
}

func TestArrowsAndTabAlwaysWorkRegardlessOfRebinding(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'h': "quick-jump", 'o': "quick-jump"})

	c.mu.Lock()
	c.doSplit(layout.Vertical)
	before := c.win().active.ID
	c.mu.Unlock()

	pressPrefixThen(c, tcell.KeyLeft, 0)

	c.mu.Lock()
	afterArrow := c.win().active.ID
	c.mu.Unlock()
	if afterArrow == before {
		t.Fatal("the left arrow should still move focus even though 'h' was rebound away from it")
	}

	pressPrefixThen(c, tcell.KeyTab, 0)

	c.mu.Lock()
	mode := c.mode
	c.mu.Unlock()
	if mode == ModeQuickJump {
		t.Fatal("Tab should still cycle focus, not trigger whatever 'o' was rebound to")
	}
}

// A digit is "jump to window N" by default, but handleKey checks that
// before consulting the bindings map — so an explicit `bind 5 vsplit`
// used to be accepted by config, listed in the help screen, and then
// never fire.
func TestExplicitDigitBindOverridesWindowJump(t *testing.T) {
	c := newTestCore(t)
	c.SetBindOverrides(map[rune]string{'5': "vsplit"})

	c.mu.Lock()
	before := len(layout.Leaves(c.win().root))
	c.mu.Unlock()

	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)})
	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '5'})

	c.mu.Lock()
	after := len(layout.Leaves(c.win().root))
	c.mu.Unlock()

	if after != before+1 {
		t.Fatalf("`bind 5 vsplit` did not split: panes %d -> %d", before, after)
	}
}

// ...while a digit left alone must still jump to its window.
func TestUnboundDigitStillJumpsToWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.newWindow() // window 1
	c.selectWindowIndex(0)
	c.mu.Unlock()

	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)})
	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '1'})

	c.mu.Lock()
	active := c.activeWindow
	c.mu.Unlock()
	if active != 1 {
		t.Fatalf("Ctrl-B 1 should select window 1, got %d", active)
	}
}
