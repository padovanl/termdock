package core

import (
	"testing"

	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/vt10x"
)

// A preview cell's colours are the raw vt10x values, where "default"
// is 1<<24 and up and anything below 256 is a palette index. Zero is
// therefore not "unset", it is palette entry 0: black. Building an empty
// cell as proto.Cell{Ch: ' '} made every blank part of a preview black
// on black, whatever theme was set, which is the black slab that showed
// in the jump picker beside a mostly empty pane.
func isDefaultColour(v uint32) bool { return v >= 1<<24 }

func TestPreviewBlanksUseTheThemeNotBlack(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.win().active.ID

	// Both preview kinds: the picker's braille thumbnail, and the
	// character-for-character one.
	for name, cells := range map[string][][]proto.Cell{
		"thumbnail": c.buildThumbnail(id, 20, 8),
		"preview":   c.buildPreview(id, 20, 8),
	} {
		if len(cells) == 0 {
			t.Fatalf("%s produced no cells", name)
		}
		for y, row := range cells {
			for x, cell := range row {
				if !isDefaultColour(cell.BG) {
					t.Errorf("%s cell (%d,%d) has background %d, want a default colour so the theme decides",
						name, x, y, cell.BG)
				}
				if cell.Ch == ' ' && !isDefaultColour(cell.FG) {
					t.Errorf("%s blank cell (%d,%d) has foreground %d, want a default colour",
						name, x, y, cell.FG)
				}
			}
		}
	}
}

// blankCell is the single place that gets this right, so pin it.
func TestBlankCellIsDefaultOnDefault(t *testing.T) {
	b := blankCell()
	if b.Ch != ' ' {
		t.Errorf("blank cell char = %q, want a space", b.Ch)
	}
	if b.FG != uint32(vt10x.DefaultFG) || b.BG != uint32(vt10x.DefaultBG) {
		t.Errorf("blank cell colours = fg %d / bg %d, want the vt10x defaults %d / %d",
			b.FG, b.BG, vt10x.DefaultFG, vt10x.DefaultBG)
	}
	// The specific trap: zero would be palette index 0, which is black.
	if b.BG == 0 || b.FG == 0 {
		t.Error("a zero colour is palette entry 0, which renders as black rather than as the theme's")
	}
}
