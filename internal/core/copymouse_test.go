package core

import (
	"strings"
	"testing"
)

// feedPane writes text straight into a pane's terminal state, without
// going through its shell — enough for the copy-mode tests to have real
// characters on screen to select.
func feedPane(c *Core, paneID int, text string) {
	p := c.panes[paneID]
	t := p.Term()
	t.Write([]byte(text))
}

// dragSelect performs a whole press-move-release gesture and returns what
// the release produced, the way the client's event stream would.
func dragSelect(c *Core, fromX, fromY, toX, toY int) Result {
	if c.mode == ModeCopy {
		c.handleCopyMouse(true, false, fromX, fromY)
		c.handleCopyMouse(true, false, toX, toY)
		return c.handleCopyMouse(false, true, toX, toY)
	}
	c.handleNormalMouse(true, false, fromX, fromY)
	c.handleNormalMouse(true, false, toX, toY)
	return c.handleCopyMouse(false, true, toX, toY)
}

// TestMouseSelectionEndsCopyMode is the regression test for "selecting
// the scrollback with the mouse worked once, then stopped": the drag
// entered copy-mode and the release never left it, so the pane stayed
// frozen at whatever row the view was on and every keystroke after that
// went to the copy cursor instead of the shell. The keyboard's y already
// exited; the mouse now does the same.
func TestMouseSelectionEndsCopyMode(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	leaf := c.win().root
	feedPane(c, leaf.ID, "hello termdock")

	res := dragSelect(c, leaf.Rect.X, leaf.Rect.Y, leaf.Rect.X+13, leaf.Rect.Y)

	if !res.HasClipboard || !strings.Contains(res.Clipboard, "hello") {
		t.Fatalf("the drag should have copied the line, got %q (has=%v)", res.Clipboard, res.HasClipboard)
	}
	if c.mode != ModeNormal {
		t.Fatalf("releasing the drag should leave copy-mode, mode=%v", c.mode)
	}
	if c.copy.selecting || c.copy.active {
		t.Fatal("the selection state should be cleared once the yank is done")
	}
	if !strings.HasPrefix(c.statusMsg, "copied") {
		t.Errorf("the status bar should confirm the copy, got %q", c.statusMsg)
	}
}

// TestSecondMouseSelectionStillWorks is the user-visible half of the same
// bug: whatever the first selection leaves behind must not stop the next
// one from working.
func TestSecondMouseSelectionStillWorks(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	leaf := c.win().root
	feedPane(c, leaf.ID, "first line\r\nsecond line")

	first := dragSelect(c, leaf.Rect.X, leaf.Rect.Y, leaf.Rect.X+9, leaf.Rect.Y)
	second := dragSelect(c, leaf.Rect.X, leaf.Rect.Y+1, leaf.Rect.X+10, leaf.Rect.Y+1)

	if !strings.Contains(first.Clipboard, "first") {
		t.Fatalf("first selection = %q, want the first line", first.Clipboard)
	}
	if !second.HasClipboard || !strings.Contains(second.Clipboard, "second") {
		t.Fatalf("second selection = %q (has=%v), want the second line", second.Clipboard, second.HasClipboard)
	}
	if c.mode != ModeNormal {
		t.Fatalf("mode after the second selection = %v, want ModeNormal", c.mode)
	}
}

// TestBlankSelectionDoesNotTouchTheClipboard: an empty yank reported as a
// success has the client push an empty OSC52, which most terminals read
// as "clear the clipboard" — so a stray drag over blank space would throw
// away whatever was copied before it.
func TestBlankSelectionDoesNotTouchTheClipboard(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	leaf := c.win().root // nothing written to this pane: all blank
	res := dragSelect(c, leaf.Rect.X+1, leaf.Rect.Y+1, leaf.Rect.X+6, leaf.Rect.Y+1)

	if res.HasClipboard {
		t.Fatalf("a selection over blank space should not report a clipboard write, got %q", res.Clipboard)
	}
}

// TestCopyCursorHiddenWhenScrolledOutOfView: the wheel scrolls the
// viewport and deliberately leaves the copy cursor where it was, so it
// can end up off screen — drawing it anyway put the terminal's real
// cursor outside the pane's own rect, over a neighbor or the status bar.
func TestCopyCursorHiddenWhenScrolledOutOfView(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	leaf := c.win().root
	// Enough output to push plenty of lines into the scrollback.
	feedPane(c, leaf.ID, strings.Repeat("filler line\r\n", 200))
	c.enterCopyMode()
	c.copy.curY = c.copy.top // start with the cursor on the first visible row
	c.mu.Unlock()

	f := c.Frame()
	pf := findPaneFrame(f.Panes, leaf.ID)
	if pf == nil || !pf.CursorVisible {
		t.Fatal("the copy cursor should be visible while it's on screen")
	}

	c.mu.Lock()
	c.scrollView(-50) // wheel up, well past the cursor
	scrolled := c.copy.top
	cur := c.copy.curY
	c.mu.Unlock()
	if cur < scrolled+leaf.Rect.H {
		t.Fatalf("test setup: cursor at %d is still within the view starting at %d", cur, scrolled)
	}

	f = c.Frame()
	pf = findPaneFrame(f.Panes, leaf.ID)
	if pf == nil {
		t.Fatal("the pane should still be in the frame")
	}
	if pf.CursorVisible {
		t.Fatalf("the copy cursor is scrolled out of view and must be hidden, got it drawn at row %d (pane rows %d..%d)", pf.CursorY, pf.Rect.Y, pf.Rect.Y+pf.Rect.H-1)
	}
}

func TestTruncateRunesKeepsCharactersWhole(t *testing.T) {
	// Byte-slicing this would cut the last "à" in half and render as a
	// replacement glyph.
	s := strings.Repeat("à", 10)
	got := truncateRunes(s, 5)
	if want := strings.Repeat("à", 5) + "…"; got != want {
		t.Fatalf("truncateRunes = %q, want %q", got, want)
	}
	if got := truncateRunes("short", 40); got != "short" {
		t.Fatalf("a string under the limit should come back untouched, got %q", got)
	}
}
