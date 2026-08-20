package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
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
