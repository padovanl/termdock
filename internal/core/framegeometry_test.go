package core

import (
	"math/rand"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// TestFrameGeometryStaysOnScreen: everything a client draws comes from
// Frame(), and it draws by the coordinates in it. Whatever the session
// has been put through — and whatever size the terminal claims to be —
// anything that would actually be painted has to lie inside the screen,
// and each pane's cell grid has to match the rect it claims, since the
// client indexes one by the other.
//
// It found the floating popup being drawn clean off the edge of a small
// terminal, and the shell inside it sized to a width that could never be
// shown.
func TestFrameGeometryStaysOnScreen(t *testing.T) {
	c := newTestCore(t)
	rng := rand.New(rand.NewSource(5))

	keys := []proto.ClientMsg{
		{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'v'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 's'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'z'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'c'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'g'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'P'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'Q'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'C'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '?'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: 'w'},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: '['},
		{Kind: "key", KeyCode: int32(tcell.KeyEsc)},
		{Kind: "key", KeyCode: int32(tcell.KeyRune), KeyRune: ' '},
	}
	sizes := [][2]int{{80, 24}, {1, 1}, {3, 2}, {200, 60}, {40, 5}, {12, 12}}

	for i := 0; i < 3000; i++ {
		if rng.Intn(7) == 0 {
			sz := sizes[rng.Intn(len(sizes))]
			c.HandleClientMsg(proto.ClientMsg{Kind: "resize", Cols: sz[0], Rows: sz[1]})
		} else {
			c.HandleClientMsg(keys[rng.Intn(len(keys))])
		}

		f := c.Frame()
		check := func(what string, r proto.Rect, cells [][]proto.Cell) {
			if r.W < 0 || r.H < 0 {
				t.Fatalf("step %d: %s has a negative rect %+v", i, what, r)
			}
			// A zero-sized rect draws nothing, and squeezing panes to
			// nothing is how a too-small terminal is meant to degrade
			// (see layout.Compute); only a rect that would actually be
			// painted has to be on screen.
			if r.W > 0 && r.H > 0 && (r.X < 0 || r.Y < 0 || r.X+r.W > f.Cols || r.Y+r.H > f.Rows) {
				t.Fatalf("step %d: %s rect %+v falls outside the %dx%d screen", i, what, r, f.Cols, f.Rows)
			}
			if cells == nil {
				return
			}
			if len(cells) != r.H {
				t.Fatalf("step %d: %s has %d rows of cells for a rect %d tall", i, what, len(cells), r.H)
			}
			for _, row := range cells {
				if len(row) != r.W {
					t.Fatalf("step %d: %s has a row %d wide in a rect %d wide", i, what, len(row), r.W)
				}
			}
		}
		for _, p := range f.Panes {
			check("pane", p.Rect, p.Cells)
		}
		if f.Popup != nil {
			check("popup", f.Popup.Rect, f.Popup.Cells)
		}
		if f.Overview != nil {
			for _, tile := range f.Overview.Tiles {
				check("overview tile", tile.Rect, nil)
			}
		}
		for _, q := range f.QuickJump {
			check("quickjump tag", q.Rect, nil)
		}
	}
}
