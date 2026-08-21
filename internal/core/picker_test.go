package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
)

func typeQuery(c *Core, s string) {
	for _, r := range s {
		c.handlePickerKey(tcell.KeyRune, r)
	}
}

func TestPickerFuzzyFilterAndJump(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow() // window 1
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune("deploy")
	c.confirmInput() // window 1 renamed "deploy"
	c.newWindow()    // window 2, default name
	c.doSplit(layout.Vertical)
	c.enterPicker()
	itemCount := len(c.picker.items)
	c.mu.Unlock()

	// One item per pane across all windows: window 0 (1 pane) + window 1
	// "deploy" (1 pane) + window 2 (2 panes) = 4.
	if itemCount != 4 {
		t.Fatalf("expected 4 picker items (1+1+2 panes across 3 windows), got %d", itemCount)
	}

	c.mu.Lock()
	typeQuery(c, "depl")
	filtered := len(c.picker.filtered)
	var label string
	if filtered > 0 {
		label = c.picker.items[c.picker.filtered[c.picker.sel]].label
	}
	c.mu.Unlock()
	if filtered != 1 {
		t.Fatalf("query %q should match exactly the renamed 'deploy' window, matched %d: %v", "depl", filtered, label)
	}

	c.mu.Lock()
	before := c.activeWindow
	c.handlePickerKey(tcell.KeyEnter, 0)
	after := c.activeWindow
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("confirming a picker selection should return to ModeNormal, got %v", mode)
	}
	if after != 1 {
		t.Fatalf("expected jump to window 1 (deploy): was active=%d, now active=%d", before, after)
	}
}

func TestPickerEscCancelsWithoutChanging(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	c.newWindow()
	c.selectWindowIndex(0)
	before := c.activeWindow
	c.enterPicker()
	typeQuery(c, "xyz-no-such-window")
	c.handlePickerKey(tcell.KeyEsc, 0)
	after := c.activeWindow
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("Esc should return to ModeNormal, got %v", mode)
	}
	if after != before {
		t.Fatalf("Esc must not change the active window: was %d, now %d", before, after)
	}
}

func TestPickerArrowNavigationWraps(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	c.newWindow()
	c.enterPicker()
	n := len(c.picker.filtered)
	if n < 3 {
		c.mu.Unlock()
		t.Fatalf("need at least 3 windows for a meaningful wrap test, got %d items", n)
	}
	startSel := c.picker.sel
	c.handlePickerKey(tcell.KeyUp, 0) // moving up from 0 should wrap to the last item
	wrapped := c.picker.sel
	c.mu.Unlock()

	if startSel != 0 {
		t.Fatalf("picker should start with the first item selected, got %d", startSel)
	}
	if wrapped != n-1 {
		t.Fatalf("Up from the first item should wrap to the last (%d), got %d", n-1, wrapped)
	}
}

func TestPickerPreviewSizedAndCropped(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	id := c.win().root.ID // the one pane in the initial window
	preview := c.buildPreview(id, previewCols, previewRows)
	c.mu.Unlock()

	if preview == nil {
		t.Fatal("buildPreview returned nil for a live pane")
	}
	if len(preview) != previewRows {
		t.Fatalf("expected %d preview rows (pane is plenty tall), got %d", previewRows, len(preview))
	}
	for i, row := range preview {
		if len(row) != previewCols {
			t.Fatalf("row %d: expected %d cols, got %d", i, previewCols, len(row))
		}
	}

	c.mu.Lock()
	missing := c.buildPreview(999999, previewCols, previewRows)
	c.mu.Unlock()
	if missing != nil {
		t.Fatalf("expected nil preview for a nonexistent pane ID, got %v", missing)
	}
}

func TestPickerOverlayCarriesPreviewForSelection(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow()
	c.enterPicker()
	ov := c.pickerOverlay()
	c.mu.Unlock()

	if ov == nil {
		t.Fatal("pickerOverlay should be non-nil while ModePicker is active")
	}
	if len(ov.PreviewCells) == 0 {
		t.Fatal("overlay should carry a preview for the initially-selected item")
	}

	// Filter down to nothing: no selection, no preview, no crash.
	c.mu.Lock()
	typeQuery(c, "xyz-definitely-not-a-window-name")
	ovEmpty := c.pickerOverlay()
	c.mu.Unlock()

	if len(ovEmpty.Items) != 0 {
		t.Fatalf("expected the filter to leave no matches, got %v", ovEmpty.Items)
	}
	if ovEmpty.PreviewCells != nil {
		t.Fatalf("expected no preview when nothing is selected, got %v", ovEmpty.PreviewCells)
	}
}

func TestPickerMRUOrderingWithEmptyQuery(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindowOpts("A", "") // index 1 (index 0 is New's own default window)
	c.newWindowOpts("B", "") // index 2
	c.newWindowOpts("C", "") // index 3
	// touchPane stamps time.Now() with no artificial delay between
	// calls, so ties are possible on a fast machine; switching windows
	// in a specific order and checking *relative* order (B after C
	// after A, whatever the absolute gap) is what actually matters here,
	// not real elapsed time.
	c.selectWindowIndex(1) // touch A
	c.selectWindowIndex(2) // touch B
	c.selectWindowIndex(3) // touch C
	c.selectWindowIndex(2) // touch B again — most recent
	c.enterPicker()
	ov := c.pickerOverlay()
	c.mu.Unlock()

	if len(ov.Items) != 4 {
		t.Fatalf("expected 4 items (default window + A/B/C, one pane each), got %d: %v", len(ov.Items), ov.Items)
	}
	// Most-recently-touched first: B, then C, then A. The default
	// window (index 0) was only ever touched once, at creation, before
	// any of the explicit selectWindowIndex calls below — oldest of the
	// four, so it trails.
	wantOrder := []string{"2:B", "3:C", "1:A"}
	for i, want := range wantOrder {
		if ov.Items[i] != want {
			t.Errorf("item %d = %q, want %q (full order: %v)", i, ov.Items[i], want, ov.Items)
		}
	}
}
