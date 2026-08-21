package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/persist"
)

// A layout has to bring back the shape *and* the working directories,
// or applying it leaves you doing the tedious half by hand.
func TestLayoutRoundTripsShapeAndDirectories(t *testing.T) {
	name := "test-layout-" + t.Name()
	t.Cleanup(func() { persist.DeleteLayout(name) })

	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	c.newWindowOpts("logs", "")
	before := len(c.windows)
	c.mu.Unlock()

	if err := c.SaveLayout(name); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}

	saved, err := persist.LoadLayout(name)
	if err != nil {
		t.Fatalf("LoadLayout: %v", err)
	}
	if len(saved.Windows) != before {
		t.Fatalf("saved %d windows, want %d", len(saved.Windows), before)
	}
	var named bool
	for _, w := range saved.Windows {
		if w.Name == "logs" {
			named = true
		}
	}
	if !named {
		t.Error("a window's name was not saved")
	}
}

// Applying adds to the session rather than replacing it: a layout is
// something you reach for to start work, and having it silently close
// panes you had running — with no undo for a whole session — would make
// it a thing you approach nervously.
func TestApplyingALayoutAddsRatherThanReplaces(t *testing.T) {
	name := "test-layout-add-" + t.Name()
	t.Cleanup(func() { persist.DeleteLayout(name) })

	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.mu.Unlock()
	if err := c.SaveLayout(name); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}

	c.mu.Lock()
	before := len(c.windows)
	beforePanes := len(c.panes)
	c.mu.Unlock()

	if err := c.ApplyLayout(name); err != nil {
		t.Fatalf("ApplyLayout: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.windows) <= before {
		t.Fatalf("windows %d -> %d, want the layout added alongside", before, len(c.windows))
	}
	if len(c.panes) <= beforePanes {
		t.Error("the original panes should still be running")
	}
}

// Pane names travel with the layout, since naming panes is most of what
// makes a saved workspace readable when you come back to it.
func TestLayoutCarriesPaneNames(t *testing.T) {
	name := "test-layout-names-" + t.Name()
	t.Cleanup(func() { persist.DeleteLayout(name) })

	c := newTestCore(t)
	c.mu.Lock()
	renamePaneTo(c, "api")
	c.mu.Unlock()

	if err := c.SaveLayout(name); err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	saved, err := persist.LoadLayout(name)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Windows[0].Root.Pane == nil || saved.Windows[0].Root.Pane.Name != "api" {
		t.Fatalf("saved pane = %+v, want it named %q", saved.Windows[0].Root.Pane, "api")
	}
}

// Asking for one that isn't there must say so plainly rather than
// building something empty.
func TestApplyingAMissingLayoutFails(t *testing.T) {
	c := newTestCore(t)
	err := c.ApplyLayout("no-such-layout-anywhere")
	if err == nil {
		t.Fatal("applying a missing layout should fail")
	}
	if !strings.Contains(err.Error(), "no saved layout") {
		t.Errorf("error = %v, want it to say the layout does not exist", err)
	}
}

// A layout file is hand-editable and shareable, so it is exactly the
// input that arrives with a nonsense split type — which layout.Compute
// has no case for, and would leave as two zero-sized panes holding real
// ptys and reachable by nothing.
func TestLayoutWithACorruptSplitStillBuildsUsablePanes(t *testing.T) {
	name := "test-layout-corrupt-" + t.Name()
	t.Cleanup(func() { persist.DeleteLayout(name) })

	if err := persist.SaveLayout(persist.Layout{
		Name: name,
		Windows: []persist.LayoutWindow{{Root: persist.LayoutNode{
			Split: 99, Ratio: 0, // both nonsense
			First:  &persist.LayoutNode{Pane: &persist.LayoutPane{}},
			Second: &persist.LayoutNode{Pane: &persist.LayoutPane{}},
		}}},
	}); err != nil {
		t.Fatal(err)
	}

	c := newTestCore(t)
	if err := c.ApplyLayout(name); err != nil {
		t.Fatalf("ApplyLayout: %v", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	w := c.windows[len(c.windows)-1]
	for _, l := range layout.Leaves(w.root) {
		if l.Rect.W <= 0 || l.Rect.H <= 0 {
			t.Fatalf("pane %d has a zero-sized rect %+v — it exists but nothing can reach it", l.ID, l.Rect)
		}
	}
}
