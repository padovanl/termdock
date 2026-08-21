package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"
)

// TestPaneNumberingIsPositionalNotID is a regression test: pane numbers
// shown in the title bar and status line used to be the pane's
// session-wide id (pane.NextID(), a counter that never resets or reuses
// a number), so after a bit of splitting and closing panes the numbers
// stopped meaning anything spatial. They should instead be the pane's
// 1-based position within its window, which stays small and predictable
// regardless of how high the underlying id has climbed.
func TestPaneNumberingIsPositionalNotID(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical) // now 2 panes: IDs N, N+1
	c.doSplit(layout.Horizontal)
	c.mu.Unlock()

	f := c.Frame()
	if len(f.Panes) != 3 {
		t.Fatalf("expected 3 panes, got %d", len(f.Panes))
	}
	for i, p := range f.Panes {
		want := byte('1' + i)
		if len(p.Title) == 0 || p.Title[0] != want {
			t.Errorf("pane %d (ID=%d) title = %q, want it to start with %q (positional, 1-based)", i, p.ID, p.Title, string(want))
		}
	}

	// Close the middle pane and split again — this bumps the global ID
	// counter further, so if numbering were ID-based the remaining
	// panes' displayed numbers would jump/skip. Positionally they must
	// stay a clean 1..N.
	c.mu.Lock()
	mid := f.Panes[1].ID
	if leaf := findLeafByID(c.win().root, mid); leaf != nil {
		c.panes[mid].Close()
		delete(c.panes, mid)
		c.detachLeafIn(c.win(), leaf)
	}
	c.doSplit(layout.Vertical)
	c.mu.Unlock()

	f2 := c.Frame()
	if len(f2.Panes) != 3 {
		t.Fatalf("expected 3 panes after close+split, got %d: %v", len(f2.Panes), f2.Panes)
	}
	for i, p := range f2.Panes {
		want := byte('1' + i)
		if len(p.Title) == 0 || p.Title[0] != want {
			t.Errorf("after close+split: pane %d title = %q, want positional prefix %q", i, p.Title, string(want))
		}
	}

	var maxID int
	for _, p := range f2.Panes {
		if p.ID > maxID {
			maxID = p.ID
		}
	}
	if maxID <= 3 {
		t.Fatalf("expected IDs to have climbed past 3 by now (global counter), got max %d — test setup is wrong", maxID)
	}
}

func TestWindowTabsLayoutIsSelfConsistent(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	c.newWindow()
	c.mu.Unlock()

	f := c.Frame()
	if len(f.Windows) != 3 {
		t.Fatalf("expected 3 window tabs, got %d", len(f.Windows))
	}
	if !f.Windows[2].Active {
		t.Fatalf("expected window 2 (just created) to be active, tabs=%+v", f.Windows)
	}
	// Tabs must be contiguous, starting right after the prefix, and each
	// one's Label must actually be W runes long — what the client will
	// draw must match what the server hit-tests a click against.
	x := len([]rune(f.StatusPrefix))
	for i, tab := range f.Windows {
		if tab.X != x {
			t.Errorf("tab %d: X=%d, want %d (contiguous after previous tab)", i, tab.X, x)
		}
		if got := len([]rune(tab.Label)); got != tab.W {
			t.Errorf("tab %d: W=%d but Label %q is %d runes", i, tab.W, tab.Label, got)
		}
		x += tab.W
	}
}

// statusRowWidth is what the status bar's left half actually needs: the
// session name, every tab laid out, and the message after them.
func statusRowWidth(f proto.Frame) int {
	w := len([]rune(f.StatusPrefix))
	for _, t := range f.Windows {
		w += t.W
	}
	return w + len([]rune(f.StatusText))
}

// TestConfirmPromptStaysOnScreenWithManyWindows: the tab strip used to
// be laid out in full regardless of the row's width, pushing the status
// message off the end. From about eight windows on, a confirm prompt was
// drawn entirely off screen — so termdock sat waiting for a y/n that
// nothing on screen had asked for, and the next keystroke either
// destroyed a window or didn't, with no way to tell which.
func TestConfirmPromptStaysOnScreenWithManyWindows(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = "main"
	for i := 0; i < 11; i++ {
		c.newWindow()
	}
	c.mu.Unlock()
	c.Resize(80, 24)

	c.mu.Lock()
	c.confirmQuit()
	c.mu.Unlock()

	f := c.Frame()
	if got := statusRowWidth(f); got > f.Cols {
		t.Errorf("the status row needs %d columns of %d — the prompt is partly off screen:\n  %s", got, f.Cols, f.StatusText)
	}
	if !strings.HasSuffix(strings.TrimSpace(f.StatusText), "(y/n)") {
		t.Errorf("the prompt must end in the answer it wants, got %q", f.StatusText)
	}
}

// TestKillWindowPromptFitsWithALongName: the window name is user data
// and unbounded, and this prompt has to stay readable on one row.
func TestKillWindowPromptFitsWithALongName(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = "main"
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune(strings.Repeat("very-long-window-name-", 8))
	c.confirmInput()
	c.confirmKillWindow()
	c.mu.Unlock()
	c.Resize(80, 24)

	f := c.Frame()
	if got := statusRowWidth(f); got > f.Cols {
		t.Errorf("the status row needs %d columns of %d:\n  %s", got, f.Cols, f.StatusText)
	}
	if !strings.HasSuffix(strings.TrimSpace(f.StatusText), "(y/n)") {
		t.Errorf("the prompt must end in the answer it wants, got %q", f.StatusText)
	}
}

// TestTabStripAlwaysShowsTheActiveWindow: with the strip now truncated to
// fit, the one tab that must never be dropped is the one saying where
// you are.
func TestTabStripAlwaysShowsTheActiveWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = "main"
	for i := 0; i < 15; i++ {
		c.newWindow()
	}
	c.mu.Unlock()

	for _, cols := range []int{200, 80, 40, 20} {
		c.Resize(cols, 24)
		for _, target := range []int{0, 7, 15} {
			c.mu.Lock()
			c.selectWindowIndex(target)
			active := c.activeWindow
			c.mu.Unlock()

			f := c.Frame()
			found := false
			for _, tab := range f.Windows {
				if tab.Index == active {
					found = true
					if !tab.Active {
						t.Errorf("cols=%d window=%d: the active window's tab isn't marked active", cols, active)
					}
				}
			}
			if !found {
				t.Errorf("cols=%d: window %d is active but has no tab; tabs=%v", cols, active, tabIndexes(f))
			}
		}
	}
}

// TestTabStripStaysHitTestable: the strip's X/W are the single source of
// truth for both drawing and resolving a click, so truncating it must
// not leave the two disagreeing.
func TestTabStripStaysHitTestable(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = "main"
	for i := 0; i < 15; i++ {
		c.newWindow()
	}
	c.mu.Unlock()
	c.Resize(80, 24)

	f := c.Frame()
	for _, tab := range f.Windows {
		for x := tab.X; x < tab.X+tab.W; x++ {
			c.mu.Lock()
			got, ok := c.tabAt(x)
			c.mu.Unlock()
			if !ok || got != tab.Index {
				t.Fatalf("column %d is inside window %d's tab but hit-tested to (%d, %v)", x, tab.Index, got, ok)
			}
		}
	}
	// Tabs must not overlap or run backwards.
	for i := 1; i < len(f.Windows); i++ {
		if f.Windows[i].X != f.Windows[i-1].X+f.Windows[i-1].W {
			t.Errorf("tab %d starts at %d, expected right after tab %d ending at %d",
				f.Windows[i].Index, f.Windows[i].X, f.Windows[i-1].Index, f.Windows[i-1].X+f.Windows[i-1].W)
		}
	}
}

func tabIndexes(f proto.Frame) []int {
	var out []int
	for _, t := range f.Windows {
		out = append(out, t.Index)
	}
	return out
}

// TestPromptsFitEvenOnANarrowRow: every prompt has to be readable to be
// answered, and a confirm's answer destroys something. On a row too
// narrow for both, the session name gives way rather than the end of the
// question — which is where "(y/n)" lives.
func TestPromptsFitEvenOnANarrowRow(t *testing.T) {
	for _, cols := range []int{120, 80, 60, 40} {
		c := newTestCore(t)
		c.mu.Lock()
		c.SessionName = "main"
		for i := 0; i < 11; i++ {
			c.newWindow()
		}
		c.mu.Unlock()
		c.Resize(cols, 24)

		c.mu.Lock()
		c.confirmQuit()
		c.mu.Unlock()
		f := c.Frame()
		if got := statusRowWidth(f); got > f.Cols {
			t.Errorf("cols=%d: confirm needs %d columns:\n  %s%s", cols, got, f.StatusPrefix, f.StatusText)
		}
		if !strings.HasSuffix(strings.TrimSpace(f.StatusText), "(y/n)") {
			t.Errorf("cols=%d: prompt = %q, must end in the answer it wants", cols, f.StatusText)
		}

		c.mu.Lock()
		c.handleConfirmKey('n')
		c.startInput("rename", "Rename window: ", "a-fairly-long-window-name", ModeNormal)
		c.mu.Unlock()
		f = c.Frame()
		if got := statusRowWidth(f); got > f.Cols {
			t.Errorf("cols=%d: the rename prompt needs %d columns:\n  %s%s", cols, got, f.StatusPrefix, f.StatusText)
		}
		if !strings.HasSuffix(f.StatusText, "_") {
			t.Errorf("cols=%d: you must be able to see the end of what you're typing, got %q", cols, f.StatusText)
		}
		closeAllPanes(c)
	}
}

// TestLongNamesCannotTakeTheWholeRow: window and session names are both
// user data with no natural bound, and either one used to be able to
// push everything else off the status row.
func TestLongNamesCannotTakeTheWholeRow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = strings.Repeat("session-name-", 8)
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune(strings.Repeat("window-name-", 8))
	c.confirmInput()
	c.mu.Unlock()
	c.Resize(80, 24)

	f := c.Frame()
	if n := len([]rune(f.StatusPrefix)); n > 40 {
		t.Errorf("the session name alone takes %d of %d columns", n, f.Cols)
	}
	for _, tab := range f.Windows {
		if n := len([]rune(tab.Label)); n > 40 {
			t.Errorf("window %d's tab alone takes %d of %d columns", tab.Index, n, f.Cols)
		}
	}
}

// TestTypingALongLineKeepsTheCursorVisible: what you type has no length
// limit (a ":" command line, a window name) while the status row does,
// and the row is drawn left to right and clipped at the edge. Past about
// half a screenful the cursor and every character just typed were simply
// not on screen — you were typing blind into a prompt that looked frozen.
func TestTypingALongLineKeepsTheCursorVisible(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.SessionName = "main"
	c.startInput("cmd", ":", "", ModeNormal)
	c.mu.Unlock()
	c.Resize(80, 24)

	typed := ""
	for i := 0; i < 200; i++ {
		ch := string(rune('a' + i%26))
		typed += ch
		c.mu.Lock()
		c.input.buffer = append(c.input.buffer, []rune(ch)...)
		c.mu.Unlock()

		f := c.Frame()
		if got := statusRowWidth(f); got > f.Cols {
			t.Fatalf("after %d characters the row needs %d of %d columns", i+1, got, f.Cols)
		}
		if !strings.HasSuffix(f.StatusText, "_") {
			t.Fatalf("after %d characters the cursor is off screen: %q", i+1, f.StatusText)
		}
		// The last few characters typed must still be readable.
		tail := typed[maxi(len(typed)-5, 0):]
		if !strings.Contains(f.StatusText, tail) {
			t.Fatalf("after %d characters, %q is not on screen: %q", i+1, tail, f.StatusText)
		}
	}
}
