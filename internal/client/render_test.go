package client

import (
	"fmt"
	"strings"
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

// screenText renders the simulation screen's contents as one string per
// row, for asserting on what a user would actually see.
func screenText(screen tcell.SimulationScreen) []string {
	cells, w, h := screen.GetContents()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		line := make([]rune, w)
		for x := 0; x < w; x++ {
			runes := cells[y*w+x].Runes
			if len(runes) == 0 {
				line[x] = ' '
				continue
			}
			line[x] = runes[0]
		}
		rows[y] = string(line)
	}
	return rows
}

func screenContains(screen tcell.SimulationScreen, want string) bool {
	for _, row := range screenText(screen) {
		if strings.Contains(row, want) {
			return true
		}
	}
	return false
}

// TestNonSelectableOverlayScrollsByOffset is the client half of the help
// screen not scrolling on a short terminal: Selected was run through the
// picker's keep-the-selection-visible math even for a list with no
// selection, so a scroll offset anywhere inside the first screenful
// resolved back to "start at 0" and the view never moved.
func TestNonSelectableOverlayScrollsByOffset(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("entry-%02d", i)
	}
	// 12 rows leaves room for 8 list rows: an offset of 5 is well inside
	// that first screenful, which is exactly the case that used to look
	// like nothing happened.
	const cols, rows = 40, 12
	screen := simScreen(t, cols, rows)
	draw(screen, proto.Frame{
		Cols: cols, Rows: rows,
		Overlay: &proto.Overlay{
			Title:      "keybindings",
			Selectable: false,
			Items:      items,
			Selected:   5,
		},
	}, config.Default())

	if !screenContains(screen, "entry-05") {
		t.Errorf("the scroll offset should put entry-05 on screen; got:\n%s", strings.Join(screenText(screen), "\n"))
	}
	if screenContains(screen, "entry-00") {
		t.Errorf("scrolled down by 5, entry-00 should be off screen; got:\n%s", strings.Join(screenText(screen), "\n"))
	}
}

// TestSelectableOverlayStillKeepsSelectionVisible guards the other half:
// a picker must still scroll only as far as it needs to keep the
// highlighted row on screen, not treat its selection as an offset.
func TestSelectableOverlayStillKeepsSelectionVisible(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = fmt.Sprintf("entry-%02d", i)
	}
	const cols, rows = 40, 12
	screen := simScreen(t, cols, rows)
	draw(screen, proto.Frame{
		Cols: cols, Rows: rows,
		Overlay: &proto.Overlay{
			Title:      "jump",
			ShowQuery:  true,
			Selectable: true,
			Items:      items,
			Selected:   3,
		},
	}, config.Default())

	if !screenContains(screen, "entry-03") {
		t.Errorf("the selected entry must be visible; got:\n%s", strings.Join(screenText(screen), "\n"))
	}
	if !screenContains(screen, "entry-00") {
		t.Errorf("selection 3 fits on the first page, so the list should not have scrolled; got:\n%s", strings.Join(screenText(screen), "\n"))
	}
}
