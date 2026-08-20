package client

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/proto"
)

func simScreen(t *testing.T, cols, rows int) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init: %v", err)
	}
	screen.SetSize(cols, rows)
	t.Cleanup(screen.Fini)
	return screen
}

func drawNoPanic(t *testing.T, f proto.Frame, cols, rows int) {
	t.Helper()
	screen := simScreen(t, cols, rows)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("draw panicked at %dx%d: %v", cols, rows, r)
		}
	}()
	draw(screen, f, config.Default())
}

// TestBorderGrid checks that drawBorders' junction-glyph auto-tiling
// produces the right box-drawing character at every kind of seam: a
// vertical divider's outer top corner, a T-junction where a vertical and
// a horizontal divider cross, a plain run of the divider, and the four
// outer corners of the whole layout.
func TestBorderGrid(t *testing.T) {
	// Mimics a vertical split (left/right) whose right half is then
	// split horizontally (top/bottom), with the 1-cell outer margin
	// relayoutLocked reserves before calling layout.Compute.
	panes := []proto.Rect{
		{X: 1, Y: 1, W: 20, H: 21},   // left
		{X: 22, Y: 1, W: 20, H: 10},  // top-right
		{X: 22, Y: 12, W: 20, H: 10}, // bottom-right
	}
	cells := map[[2]int]bool{}
	for _, r := range panes {
		addRectBorder(cells, r)
	}

	cols, rows := 45, 25
	grid := make([][]rune, rows)
	for y := range grid {
		grid[y] = make([]rune, cols)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}
	for cell := range cells {
		x, y := cell[0], cell[1]
		if x < 0 || y < 0 || x >= cols || y >= rows {
			continue
		}
		up := cells[[2]int{x, y - 1}]
		down := cells[[2]int{x, y + 1}]
		left := cells[[2]int{x - 1, y}]
		right := cells[[2]int{x + 1, y}]
		grid[y][x] = boxChar(up, down, left, right)
	}

	if grid[0][21] != '┬' {
		t.Errorf("expected a top tee where the middle vertical divider meets the outer top border at (21,0), got %q", grid[0][21])
	}
	mid := grid[11][21]
	if mid != '┼' && mid != '┤' && mid != '├' {
		t.Errorf("expected a junction where the vertical and horizontal dividers cross, got %q at (21,11)", mid)
	}
	for y := 2; y < 11; y++ {
		if grid[y][21] != '│' {
			t.Errorf("expected plain vertical divider at (21,%d), got %q", y, grid[y][21])
		}
	}
	corners := map[[2]int]rune{{0, 0}: '┌', {42, 0}: '┐', {0, 22}: '└', {42, 22}: '┘'}
	for c, want := range corners {
		if got := grid[c[1]][c[0]]; got != want {
			t.Errorf("expected outer corner %q at (%d,%d), got %q", want, c[0], c[1], got)
		}
	}
}

func TestDrawHelpOverlayDoesNotPanic(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "some binding   does a thing"
	}
	sizes := []struct{ cols, rows int }{
		{80, 24}, {40, 24}, {20, 10}, {200, 60}, {80, 5},
	}
	for _, sz := range sizes {
		f := proto.Frame{
			Cols: sz.cols, Rows: sz.rows,
			ShowStatus:   true,
			StatusPrefix: " termdock:test ",
			Overlay: &proto.Overlay{
				Title:      "keybindings — any key closes",
				ShowQuery:  false,
				Selectable: false,
				Items:      items,
				Selected:   7,
			},
		}
		drawNoPanic(t, f, sz.cols, sz.rows)
	}
}

func TestDrawPickerOverlayWithPreviewDoesNotPanic(t *testing.T) {
	sizes := []struct{ cols, rows int }{
		{80, 24}, {40, 24}, {20, 10}, {200, 60},
	}
	preview := make([][]proto.Cell, 8)
	for y := range preview {
		row := make([]proto.Cell, 36)
		for x := range row {
			row[x] = proto.Cell{Ch: 'x'}
		}
		preview[y] = row
	}
	for _, sz := range sizes {
		f := proto.Frame{
			Cols: sz.cols, Rows: sz.rows,
			ShowStatus:   true,
			StatusPrefix: " termdock:test ",
			Overlay: &proto.Overlay{
				Title:        "jump to window/pane",
				ShowQuery:    true,
				Query:        "depl",
				Selectable:   true,
				Items:        []string{"0:bash", "1:deploy › 1:npm", "1:deploy › 2:tail"},
				Selected:     1,
				PreviewCells: preview,
			},
		}
		drawNoPanic(t, f, sz.cols, sz.rows)
	}
}

func TestDrawOverlayNoMatchesDoesNotPanic(t *testing.T) {
	f := proto.Frame{
		Cols: 80, Rows: 24,
		ShowStatus:   true,
		StatusPrefix: " termdock:test ",
		Overlay: &proto.Overlay{
			Title:      "jump to window/pane",
			ShowQuery:  true,
			Query:      "nope",
			Selectable: true,
			Items:      nil,
			Selected:   0,
		},
	}
	drawNoPanic(t, f, 80, 24)
}
