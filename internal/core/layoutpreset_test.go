package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func TestCycleLayoutSinglePaneIsANoop(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	before := c.win().root
	c.cycleLayout()
	after := c.win().root
	msg := c.statusMsg
	c.mu.Unlock()

	if before != after {
		t.Fatal("cycling layout with only one pane should be a no-op")
	}
	if msg == "" {
		t.Fatal("expected a status message explaining the no-op")
	}
}

func TestCycleLayoutBlockedWhileZoomed(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.toggleZoom()
	before := c.win().root
	c.cycleLayout()
	after := c.win().root
	c.mu.Unlock()

	if before != after {
		t.Fatal("cycling layout while zoomed should be a no-op")
	}
}

// TestCycleLayoutPreservesEveryPaneAndActive cycles through every preset
// (plus one extra to confirm it wraps back around) and checks, at every
// step, that no pane's process is lost or duplicated and the active pane
// never silently changes — only the tree shape and each split's Ratio
// should move.
func TestCycleLayoutPreservesEveryPaneAndActive(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	c.doSplit(layout.Vertical)
	wantIDs := map[int]bool{}
	for _, l := range layout.Leaves(c.win().root) {
		wantIDs[l.ID] = true
	}
	activeID := c.win().active.ID

	for i := 0; i < int(numLayoutPresets)+1; i++ {
		c.cycleLayout()
		leaves := layout.Leaves(c.win().root)
		if len(leaves) != len(wantIDs) {
			t.Fatalf("iteration %d: expected %d panes, got %d", i, len(wantIDs), len(leaves))
		}
		seen := map[int]bool{}
		for _, l := range leaves {
			if seen[l.ID] {
				t.Fatalf("iteration %d: pane %d appears twice in the rebuilt tree", i, l.ID)
			}
			seen[l.ID] = true
			if !wantIDs[l.ID] {
				t.Fatalf("iteration %d: unexpected pane %d in the rebuilt tree", i, l.ID)
			}
		}
		if c.win().active.ID != activeID {
			t.Fatalf("iteration %d: active pane changed from %d to %d", i, activeID, c.win().active.ID)
		}
	}
	c.mu.Unlock()
}

func TestCycleLayoutRotatesThroughDistinctPresetNames(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)

	seen := map[string]bool{}
	for i := 0; i < int(numLayoutPresets); i++ {
		c.cycleLayout()
		seen[c.win().layoutPreset.String()] = true
	}
	c.mu.Unlock()

	if len(seen) != int(numLayoutPresets) {
		t.Fatalf("cycling through a full rotation should visit every preset exactly once, saw %v", seen)
	}
}

type fakePaneHost struct{}

func (fakePaneHost) Resize(cols, rows int) {}

func TestBuildLeafChainGivesEqualColumnsInOrder(t *testing.T) {
	refs := make([]leafRef, 4)
	for i := range refs {
		refs[i] = leafRef{id: i + 1, p: fakePaneHost{}}
	}
	root := buildLeafChain(refs, layout.Vertical)
	layout.Compute(root, layout.Rect{X: 0, Y: 0, W: 84, H: 20})

	leaves := layout.Leaves(root)
	if len(leaves) != 4 {
		t.Fatalf("expected 4 leaves, got %d", len(leaves))
	}
	for i, l := range leaves {
		if l.ID != i+1 {
			t.Fatalf("leaf %d out of order: got ID %d, want %d — original left-to-right order must be preserved", i, l.ID, i+1)
		}
		if l.Rect.W < 19 || l.Rect.W > 21 {
			t.Fatalf("leaf %d width = %d, want ~20 (four equal columns out of 84-3 dividers)", l.ID, l.Rect.W)
		}
	}
}

func TestBuildTiledMakesARoughlySquareGrid(t *testing.T) {
	refs := make([]leafRef, 5)
	for i := range refs {
		refs[i] = leafRef{id: i + 1, p: fakePaneHost{}}
	}
	root := buildTiled(refs)
	layout.Compute(root, layout.Rect{X: 0, Y: 0, W: 90, H: 30})

	leaves := layout.Leaves(root)
	if len(leaves) != 5 {
		t.Fatalf("expected 5 leaves, got %d", len(leaves))
	}
	seen := map[int]bool{}
	for _, l := range leaves {
		seen[l.ID] = true
		if l.Rect.W <= 0 || l.Rect.H <= 0 {
			t.Fatalf("leaf %d has a non-positive Rect %+v", l.ID, l.Rect)
		}
	}
	for _, r := range refs {
		if !seen[r.id] {
			t.Fatalf("pane %d missing from the tiled grid", r.id)
		}
	}
}
