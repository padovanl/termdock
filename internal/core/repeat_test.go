package core

import (
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

func pressKey(c *Core, code tcell.Key) {
	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(code)})
}

func activeID(c *Core) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.win().active.ID
}

// setupStack builds a window of four stacked panes and focuses the
// bottom one, then returns the leaf IDs top-to-bottom.
func setupStack(t *testing.T) (*Core, []int) {
	t.Helper()
	c := newTestCore(t)
	c.SetRepeatTime(1000)
	c.mu.Lock()
	for i := 0; i < 3; i++ {
		c.doSplit(layout.Horizontal)
	}
	leaves := layout.Leaves(c.win().root)
	ids := make([]int, len(leaves))
	for i, l := range leaves {
		ids[i] = l.ID
	}
	c.setActive(leaves[len(leaves)-1])
	c.mu.Unlock()
	return c, ids
}

// The exact sequence from a real TERMDOCK_INPUT_LOG capture: one Ctrl-B,
// then four bare Up presses. Every one of them should walk the focus up,
// rather than only the first and the rest going to the shell.
func TestBareArrowRepeatsFocusMove(t *testing.T) {
	c, ids := setupStack(t)

	pressKey(c, tcell.KeyCtrlB)
	pressKey(c, tcell.KeyUp)
	if got := activeID(c); got != ids[2] {
		t.Fatalf("first (prefixed) Up: active pane %d, want %d", got, ids[2])
	}
	for _, want := range []int{ids[1], ids[0]} {
		pressKey(c, tcell.KeyUp) // no prefix this time
		if got := activeID(c); got != want {
			t.Fatalf("bare Up should have repeated the move: active pane %d, want %d", got, want)
		}
	}
}

// The window must close once it expires, so an arrow pressed a while
// later is ordinary input for the pane again.
func TestRepeatWindowExpires(t *testing.T) {
	c, ids := setupStack(t)
	c.SetRepeatTime(20)

	pressKey(c, tcell.KeyCtrlB)
	pressKey(c, tcell.KeyUp)
	moved := activeID(c)

	time.Sleep(60 * time.Millisecond)
	pressKey(c, tcell.KeyUp)
	if got := activeID(c); got != moved {
		t.Fatalf("an arrow after the repeat window expired should go to the pane, but focus moved %d -> %d", moved, got)
	}
	_ = ids
}

// repeat-time 0 opts out completely.
func TestRepeatDisabled(t *testing.T) {
	c, ids := setupStack(t)
	c.SetRepeatTime(0)

	pressKey(c, tcell.KeyCtrlB)
	pressKey(c, tcell.KeyUp)
	moved := activeID(c)
	pressKey(c, tcell.KeyUp)
	if got := activeID(c); got != moved {
		t.Fatalf("with repeat-time 0 a bare arrow must not move focus, went %d -> %d", moved, got)
	}
	_ = ids
}

// A non-focus command in between must close the window, so arrows aren't
// left hijacked afterwards.
func TestUnrelatedCommandEndsRepeatWindow(t *testing.T) {
	c, ids := setupStack(t)

	pressKey(c, tcell.KeyCtrlB)
	pressKey(c, tcell.KeyUp)
	moved := activeID(c)

	// Ctrl-B z (zoom) — not a focus move.
	pressKey(c, tcell.KeyCtrlB)
	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'z'})

	pressKey(c, tcell.KeyUp)
	if got := activeID(c); got != moved {
		t.Fatalf("an unrelated command should end the repeat window, but a bare arrow still moved focus %d -> %d", moved, got)
	}
	_ = ids
}

// hjkl deliberately never repeat: they're ordinary text, and swallowing
// the "h" of something you start typing right after switching panes
// would be worse than the keystroke it saves.
func TestLettersNeverRepeat(t *testing.T) {
	c, ids := setupStack(t)

	pressKey(c, tcell.KeyCtrlB)
	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'k'})
	moved := activeID(c)

	c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'k'})
	if got := activeID(c); got != moved {
		t.Fatalf("a bare 'k' must go to the pane as text, but focus moved %d -> %d", moved, got)
	}
	_ = ids
}

// The horizontal arrows repeat exactly like the vertical ones — worth
// its own case since the first version of this test only ever pressed Up.
func TestBareLateralArrowRepeatsFocusMove(t *testing.T) {
	c := newTestCore(t)
	c.SetRepeatTime(1000)
	c.mu.Lock()
	for i := 0; i < 3; i++ {
		c.doSplit(layout.Vertical) // side by side
	}
	leaves := layout.Leaves(c.win().root)
	ids := make([]int, len(leaves))
	for i, l := range leaves {
		ids[i] = l.ID
	}
	c.setActive(leaves[0]) // leftmost
	c.mu.Unlock()

	pressKey(c, tcell.KeyCtrlB)
	pressKey(c, tcell.KeyRight)
	if got := activeID(c); got != ids[1] {
		t.Fatalf("first (prefixed) Right: active pane %d, want %d", got, ids[1])
	}
	for _, want := range []int{ids[2], ids[3]} {
		pressKey(c, tcell.KeyRight) // bare
		if got := activeID(c); got != want {
			t.Fatalf("bare Right should have repeated the move: active pane %d, want %d", got, want)
		}
	}
	// ...and back the other way, still without the prefix.
	pressKey(c, tcell.KeyLeft)
	if got := activeID(c); got != ids[2] {
		t.Fatalf("bare Left should have repeated too: active pane %d, want %d", got, ids[2])
	}
}

// TestRepeatNotArmedByAMoveThatWentNowhere: a focus move with nowhere to
// go — every arrow in a single-pane window, or the one direction without
// a neighbour — used to open the repeat window anyway. termdock then
// swallowed every arrow key for the next second on behalf of a command
// that had done nothing, which in a shell means losing history recall.
func TestRepeatNotArmedByAMoveThatWentNowhere(t *testing.T) {
	c := newTestCore(t)
	c.SetRepeatTime(1000)

	pressPrefixThen(c, tcell.KeyLeft, 0) // one pane: nothing to the left

	c.mu.Lock()
	armed := c.repeatActive()
	c.mu.Unlock()
	if armed {
		t.Fatal("a move that went nowhere should not start hijacking bare arrows")
	}
}

// TestRepeatSurvivesOvershooting: walking across panes and pressing once
// more at the far edge must not drop you out of navigation — the arrow
// that comes back is the very next thing you reach for.
func TestRepeatSurvivesOvershooting(t *testing.T) {
	c := newTestCore(t)
	c.SetRepeatTime(1000)
	c.mu.Lock()
	c.doSplit(layout.Vertical) // two panes, the right one active
	c.mu.Unlock()

	pressPrefixThen(c, tcell.KeyLeft, 0) // moves: now on the left pane
	c.mu.Lock()
	movedOK := c.repeatActive()
	c.mu.Unlock()
	if !movedOK {
		t.Fatal("a move that worked should open the repeat window")
	}

	// Bare ← again: already at the left edge, so it goes nowhere.
	c.handleKey(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyLeft)})
	c.mu.Lock()
	stillOpen := c.repeatActive()
	c.mu.Unlock()
	if !stillOpen {
		t.Error("overshooting shouldn't close a repeat window that's already open")
	}

	// ...and → still comes back.
	c.handleKey(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyRight)})
	c.mu.Lock()
	active := c.win().active
	right := layout.Leaves(c.win().root)[1]
	c.mu.Unlock()
	if active != right {
		t.Error("a bare → after overshooting should still move focus back")
	}
}
