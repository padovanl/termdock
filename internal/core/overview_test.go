package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
)

func TestOverviewListsEveryPaneAndSelectsCurrent(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.doSplit(layout.Vertical) // window 0: 2 panes, right one active
	c.newWindow()              // window 1: 1 pane
	activeLeaf := c.win().active
	c.enterOverview()
	if c.mode != ModeOverview {
		t.Fatalf("enterOverview should set ModeOverview, got %v", c.mode)
	}
	tileCount := len(c.overview.tiles)
	selTarget := c.overview.tiles[c.overview.sel]
	c.mu.Unlock()

	if tileCount != 3 {
		t.Fatalf("expected 3 tiles (2 panes in window 0 + 1 in window 1), got %d", tileCount)
	}
	if selTarget.paneID != activeLeaf.ID {
		t.Fatalf("initial selection should be the currently active pane (id=%d), got tile for id=%d", activeLeaf.ID, selTarget.paneID)
	}
}

func TestOverviewGridNavigationStaysInBounds(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	for i := 0; i < 5; i++ {
		c.newWindow()
	}
	c.enterOverview()
	n := len(c.overview.tiles)
	c.overview.sel = 0
	c.handleOverviewKey(tcell.KeyUp, 0) // up from the first tile must clamp, not go negative
	afterUp := c.overview.sel
	for i := 0; i < n+5; i++ {
		c.handleOverviewKey(tcell.KeyRight, 0)
	}
	afterManyRight := c.overview.sel
	c.mu.Unlock()

	if afterUp != 0 {
		t.Errorf("Up from the first tile should clamp at 0, got %d", afterUp)
	}
	if afterManyRight != n-1 {
		t.Errorf("Right past the last tile should clamp at %d, got %d", n-1, afterManyRight)
	}
}

func TestOverviewConfirmJumpsToSelectedPane(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow() // window 1
	c.selectWindowIndex(0)
	c.enterOverview()
	// find the tile belonging to window 1 and select it
	target := -1
	for i, tl := range c.overview.tiles {
		if tl.windowIdx == 1 {
			target = i
		}
	}
	if target < 0 {
		c.mu.Unlock()
		t.Fatal("expected a tile for window 1")
	}
	c.overview.sel = target
	c.confirmOverview()
	mode := c.mode
	active := c.activeWindow
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("confirming should return to ModeNormal, got %v", mode)
	}
	if active != 1 {
		t.Fatalf("expected to jump to window 1, active=%d", active)
	}
}

func TestOverviewClickJumpsToTile(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow() // window 1
	c.selectWindowIndex(0)
	c.enterOverview()
	tileLayout := c.overviewLayout()
	var clickX, clickY, wantWindow int
	for i, tl := range tileLayout {
		if c.overview.tiles[i].windowIdx == 1 {
			clickX, clickY = tl.rect.X+1, tl.rect.Y+1
			wantWindow = 1
		}
	}
	c.handleOverviewMouse(true, false, clickX, clickY)
	mode := c.mode
	active := c.activeWindow
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("clicking a tile should confirm and return to ModeNormal, got %v", mode)
	}
	if active != wantWindow {
		t.Fatalf("expected to jump to window %d, active=%d", wantWindow, active)
	}
}

func TestOverviewLayoutTilesCoverScreenWithoutOverlap(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	for i := 0; i < 7; i++ {
		c.newWindow()
	}
	c.enterOverview()
	tiles := c.overviewLayout()
	c.mu.Unlock()

	if len(tiles) != 8 {
		t.Fatalf("expected 8 tiles, got %d", len(tiles))
	}
	for i := 0; i < len(tiles); i++ {
		for j := i + 1; j < len(tiles); j++ {
			a, b := tiles[i].rect, tiles[j].rect
			if a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H {
				t.Fatalf("tiles %d and %d overlap: %+v vs %+v", i, j, a, b)
			}
		}
	}
}
