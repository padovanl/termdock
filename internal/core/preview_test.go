package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/proto"
)

// The preview is a minimap of the whole pane, so content anywhere in it —
// including a fresh shell's prompt sitting alone at the very top — has to
// show up. A preview that only ever sampled the bottom of the grid drew
// an entirely blank box for every pane in a new session.
func TestPreviewShowsContentFromTheTopOfThePane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	p := c.panes[id]
	c.mu.Unlock()

	p.Term().Write([]byte("MARKER-TOP"))

	c.mu.Lock()
	cells := c.buildThumbnail(id, previewCols, previewRows)
	c.mu.Unlock()

	if countInk(cells) == 0 {
		t.Fatal("preview is entirely blank despite content at the top of the pane")
	}
}

// Content far down the pane must also register: a minimap covers the
// whole grid, not a window onto part of it.
func TestPreviewCoversTheBottomOfThePane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	p := c.panes[id]
	c.mu.Unlock()

	// Push everything off with blank lines, then write low in the grid.
	p.Term().Write([]byte(strings.Repeat("\r\n", 18) + "MARKER-BOTTOM"))

	c.mu.Lock()
	cells := c.buildThumbnail(id, previewCols, previewRows)
	c.mu.Unlock()

	if countInk(cells) == 0 {
		t.Fatal("preview is entirely blank despite content near the bottom of the pane")
	}
}

// The overlay is centered on its total size, so a preview whose row count
// varied with the selected pane's height made the whole box jump around
// the screen while arrowing through the list.
func TestPreviewIsAFixedSizeRegardlessOfPaneDimensions(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	p := c.panes[id]
	c.mu.Unlock()

	for _, sz := range [][2]int{{200, 60}, {80, 24}, {20, 4}, {8, 2}} {
		p.Resize(sz[0], sz[1])
		c.mu.Lock()
		cells := c.buildThumbnail(id, previewCols, previewRows)
		c.mu.Unlock()

		if len(cells) != previewRows {
			t.Fatalf("pane %dx%d produced a %d-row preview, want a fixed %d", sz[0], sz[1], len(cells), previewRows)
		}
		for i, row := range cells {
			if len(row) != previewCols {
				t.Fatalf("pane %dx%d row %d is %d wide, want a fixed %d", sz[0], sz[1], i, len(row), previewCols)
			}
		}
	}
}

// Every glyph the minimap emits must be blank or a braille cell — a stray
// raw character from the pane would break the "shrunken terminal" look.
func TestPreviewEmitsOnlyBrailleOrBlank(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	p := c.panes[id]
	c.mu.Unlock()

	p.Term().Write([]byte("some plain text\r\nand another line\r\n"))

	c.mu.Lock()
	cells := c.buildThumbnail(id, previewCols, previewRows)
	c.mu.Unlock()

	for _, row := range cells {
		for _, cell := range row {
			if cell.Ch == ' ' || cell.Ch == 0 {
				continue
			}
			if cell.Ch < 0x2800 || cell.Ch > 0x28FF {
				t.Fatalf("preview emitted %q (%U), want only braille or blank", cell.Ch, cell.Ch)
			}
		}
	}
}

func countInk(cells [][]proto.Cell) int {
	n := 0
	for _, row := range cells {
		for _, cell := range row {
			if cell.Ch != ' ' && cell.Ch != 0 {
				n++
			}
		}
	}
	return n
}

// The overview's tiles are big enough for real text, so they keep using
// buildPreview — and it must be fixed-size too, so a short pane doesn't
// come back as a ragged tile.
func TestBuildPreviewIsFixedSizeAndShowsRealCharacters(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	p := c.panes[id]
	c.mu.Unlock()

	p.Resize(40, 6)
	p.Term().Write([]byte("REALTEXT"))

	c.mu.Lock()
	cells := c.buildPreview(id, 60, 20)
	c.mu.Unlock()

	if len(cells) != 20 {
		t.Fatalf("got %d rows, want a fixed 20", len(cells))
	}
	for i, row := range cells {
		if len(row) != 60 {
			t.Fatalf("row %d is %d wide, want a fixed 60", i, len(row))
		}
	}
	var sb strings.Builder
	for _, row := range cells {
		for _, cell := range row {
			if cell.Ch != 0 {
				sb.WriteRune(cell.Ch)
			}
		}
	}
	if !strings.Contains(sb.String(), "REALTEXT") {
		t.Fatalf("overview preview should show real characters, got:\n%s", sb.String())
	}
}
