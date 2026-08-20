package layout

import "testing"

type fakePane struct{ w, h int }

func (f *fakePane) Resize(w, h int) { f.w, f.h = w, h }

func TestHorizontalSplitReservesDivider(t *testing.T) {
	p1, p2 := &fakePane{}, &fakePane{}
	root := NewLeaf(1, p1)
	root.Rect = Rect{0, 0, 20, 21}
	second, ok := Split(root, Horizontal, 2, p2)
	if !ok {
		t.Fatal("split rejected")
	}
	Compute(root, Rect{0, 0, 20, 21})

	if root.First.Rect.Y+root.First.Rect.H >= second.Rect.Y {
		t.Fatalf("top and bottom panes overlap or touch with no divider gap: first ends at %d, second starts at %d",
			root.First.Rect.Y+root.First.Rect.H, second.Rect.Y)
	}
	gap := second.Rect.Y - (root.First.Rect.Y + root.First.Rect.H)
	if gap != 1 {
		t.Fatalf("expected exactly 1 row divider gap, got %d", gap)
	}
	if root.DividerY != root.First.Rect.Y+root.First.Rect.H {
		t.Fatalf("DividerY %d should sit exactly in the gap row %d", root.DividerY, root.First.Rect.Y+root.First.Rect.H)
	}

	// hit-test the divider
	if hit := HitDivider(root, 5, root.DividerY); hit != root {
		t.Fatalf("HitDivider should find the horizontal divider node, got %v", hit)
	}
	if hit := HitDivider(root, 5, root.DividerY+1); hit != nil {
		t.Fatalf("HitDivider should not match off-row, got %v", hit)
	}

	// drag resize via row
	before := root.Ratio
	SetRatioFromRow(root, root.Rect.Y+2)
	if root.Ratio == before {
		t.Fatal("SetRatioFromRow did not change ratio")
	}
	Compute(root, Rect{0, 0, 20, 21})

	// keyboard resize-mode path
	before = root.Ratio
	Resize(second, Horizontal, 2)
	if root.Ratio == before {
		t.Fatal("Resize (keyboard) did not change ratio")
	}
}

func TestNestedSplitsDoNotOverlap(t *testing.T) {
	p1, p2, p3 := &fakePane{}, &fakePane{}, &fakePane{}
	root := NewLeaf(1, p1)
	root.Rect = Rect{0, 0, 41, 21}
	right, ok := Split(root, Vertical, 2, p2)
	if !ok {
		t.Fatal("vsplit rejected")
	}
	right.Rect = Rect{21, 0, 20, 21}
	_, ok = Split(right, Horizontal, 3, p3)
	if !ok {
		t.Fatal("hsplit rejected")
	}
	Compute(root, Rect{0, 0, 41, 21})

	leaves := Leaves(root)
	for i := 0; i < len(leaves); i++ {
		for j := i + 1; j < len(leaves); j++ {
			a, b := leaves[i].Rect, leaves[j].Rect
			if rectsOverlap(a, b) {
				t.Fatalf("leaves %d and %d overlap: %+v vs %+v", leaves[i].ID, leaves[j].ID, a, b)
			}
		}
	}
	if p1.w != root.First.Rect.W || p1.h != root.First.Rect.H {
		t.Fatalf("pane1 not resized to its content rect: got %dx%d want %dx%d", p1.w, p1.h, root.First.Rect.W, root.First.Rect.H)
	}
}

func rectsOverlap(a, b Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// TestRemovePreservesSiblingIdentity is a regression test: Remove used to
// splice a removed leaf's sibling into its parent's slot by copying the
// sibling's fields into the parent node in place, which orphaned any
// *other* pointer to the sibling itself (e.g. core.Window.active)
// whenever the sibling being promoted also happened to be the one that
// pointer referenced — an ordinary sequence (a background pane's shell
// exiting while a sibling pane has focus), not an edge case.
func TestRemovePreservesSiblingIdentity(t *testing.T) {
	root := NewLeaf(1, &fakePane{})
	root.Rect = Rect{0, 0, 80, 24}
	b, ok := Split(root, Vertical, 2, &fakePane{}) // root: A(1) | B(2)
	if !ok {
		t.Fatal("split rejected")
	}
	Compute(root, Rect{0, 0, 80, 24})

	// Split B into top/bottom; "bottom" becomes the active pane in the
	// real app (Split returns the second child as the new focus).
	bottom, ok := Split(b, Horizontal, 3, &fakePane{}) // B -> top(2) / bottom(3)
	if !ok {
		t.Fatal("split rejected")
	}
	Compute(root, Rect{0, 0, 80, 24})
	top := b.First
	active := bottom // what Window.active would hold

	newRoot, _ := Remove(root, top) // top's shell exited while bottom (sibling) has focus
	if newRoot == nil {
		t.Fatal("tree should not be empty")
	}

	found := false
	for _, l := range Leaves(newRoot) {
		if l == active {
			found = true
		}
	}
	if !found {
		t.Fatalf("active pane (id=%d) is no longer reachable from the tree after removing its sibling — "+
			"it's a dangling pointer, exactly the bug this test guards against", active.ID)
	}
	if len(Leaves(newRoot)) != 2 {
		t.Fatalf("expected 2 leaves after removal, got %d", len(Leaves(newRoot)))
	}
}
