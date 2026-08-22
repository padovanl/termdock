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

// TestPickerOverlayLeavesRoomForPreviewOnAn80ColTerminal is a regression
// test for the picker box swallowing an 80-column terminal almost
// whole: with the real title (a ~70-character instruction sentence) and
// short items, the preview must still get a legible amount of width —
// not the single-digit sliver it got back when the title's raw length
// set the box's width floor (see drawOverlay).
func TestPickerOverlayLeavesRoomForPreviewOnAn80ColTerminal(t *testing.T) {
	preview := make([][]proto.Cell, 8)
	for y := range preview {
		row := make([]proto.Cell, 48)
		for x := range row {
			row[x] = proto.Cell{Ch: 'x'}
		}
		preview[y] = row
	}
	f := proto.Frame{
		Cols: 80, Rows: 24,
		ShowStatus:   true,
		StatusPrefix: " termdock:test ",
		Overlay: &proto.Overlay{
			Title:        "jump to window/pane — type to filter, ↑↓ select, enter jump, esc cancel",
			ShowQuery:    true,
			Query:        "",
			Selectable:   true,
			Items:        []string{"0:bash › 1:bash", "0:bash › 2:bash"},
			Selected:     0,
			PreviewCells: preview,
		},
	}
	screen := simScreen(t, 80, 24)
	draw(screen, f, config.Default())

	cells, w, h := screen.GetContents()
	xCount := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if c := cells[y*w+x]; len(c.Runes) > 0 && c.Runes[0] == 'x' {
				xCount++
			}
		}
	}
	// 8 rows x 48 cols of preview content exists server-side; demand at
	// least a third of it actually made it to screen, proving the box
	// split didn't starve the preview down to nothing.
	const wantAtLeast = 8 * 48 / 3
	if xCount < wantAtLeast {
		t.Fatalf("only %d preview cells reached the screen on an 80-col terminal, want at least %d — the picker box is starving the preview of width", xCount, wantAtLeast)
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

func TestWordWrapBreaksOnSpacesNotMidWord(t *testing.T) {
	lines := wordWrap("jump to window/pane — type to filter, enter jump, esc cancel", 20, 5)
	for _, l := range lines {
		if len([]rune(l)) > 20 {
			t.Errorf("line %q exceeds width 20", l)
		}
	}
	joined := strings.Join(lines, " ")
	for _, word := range strings.Fields("jump to window/pane type to filter enter jump esc cancel") {
		if !strings.Contains(joined, word) {
			t.Errorf("word %q lost from wrapped output: %v", word, lines)
		}
	}
}

func TestWordWrapCapsAtMaxLines(t *testing.T) {
	lines := wordWrap("one two three four five six seven eight nine ten", 5, 2)
	if len(lines) > 2 {
		t.Fatalf("expected at most 2 lines, got %d: %v", len(lines), lines)
	}
}

func TestWordWrapHardBreaksAWordWiderThanWidth(t *testing.T) {
	lines := wordWrap("supercalifragilisticexpialidocious", 10, 5)
	for _, l := range lines {
		if len([]rune(l)) > 10 {
			t.Errorf("line %q exceeds width 10", l)
		}
	}
	if len(lines) < 3 {
		t.Fatalf("expected the long word to be hard-broken across multiple lines, got %v", lines)
	}
}

// With no theme (and no explicit pane-bg/pane-fg) a pane cell the
// program left unstyled must still come out as ColorDefault, i.e. the
// emulator's own colors — the whole point of ColorDefault being the
// default for PaneBG/PaneFG. A regression here would repaint every
// unthemed user's terminal.
func TestUnthemedCellsKeepTheTerminalsOwnColors(t *testing.T) {
	cfg := config.Default()
	unstyled := proto.Cell{Ch: 'x', FG: 1 << 24, BG: 1 << 24} // convColor's "default" marker
	fg, bg, _ := cellStyle(unstyled, cfg).Decompose()
	if fg != tcell.ColorDefault || bg != tcell.ColorDefault {
		t.Fatalf("unthemed unstyled cell = fg %v / bg %v, want both ColorDefault", fg, bg)
	}
}

// ...and with a theme, that same cell picks up the theme's pane colors.
func TestThemedCellsUseThePaneColors(t *testing.T) {
	cfg := config.Default()
	cfg.PaneBG = tcell.NewHexColor(0x300a24)
	cfg.PaneFG = tcell.NewHexColor(0xeeeeec)
	unstyled := proto.Cell{Ch: 'x', FG: 1 << 24, BG: 1 << 24}
	fg, bg, _ := cellStyle(unstyled, cfg).Decompose()
	if bg != cfg.PaneBG || fg != cfg.PaneFG {
		t.Fatalf("themed unstyled cell = fg %v / bg %v, want fg %v / bg %v", fg, bg, cfg.PaneFG, cfg.PaneBG)
	}
}

// A cell the program *did* style must keep its own colors regardless of
// the theme: the theme fills in the terminal's defaults, it doesn't
// repaint output that asked for a specific color.
func TestThemeDoesNotOverrideExplicitCellColors(t *testing.T) {
	cfg := config.Default()
	cfg.PaneBG = tcell.NewHexColor(0x300a24)
	cfg.PaneFG = tcell.NewHexColor(0xeeeeec)
	green := proto.Cell{Ch: 'x', FG: 0x00ff00, BG: 0x123456}
	fg, bg, _ := cellStyle(green, cfg).Decompose()
	if fg != tcell.NewRGBColor(0, 0xff, 0) || bg != tcell.NewRGBColor(0x12, 0x34, 0x56) {
		t.Fatalf("explicitly colored cell was altered: fg %v bg %v", fg, bg)
	}
}

// TestThemedFrameLeavesNoDefaultColoredCells is the "no black gaps"
// guarantee: with a theme on, every cell of a full frame — pane content,
// the margin around the layout, the pane borders, the status bar, the
// window tabs — must have a real background, not ColorDefault. Anything
// left at the default shows through as the emulator's own background,
// which is exactly the black frame a themed session used to sit in.
func TestThemedFrameLeavesNoDefaultColoredCells(t *testing.T) {
	cfg := config.Default()
	cfg.PaneBG = tcell.NewHexColor(0x300a24)
	cfg.PaneFG = tcell.NewHexColor(0xeeeeec)
	cfg.StatusBG = tcell.NewHexColor(0x772953)
	cfg.StatusFG = tcell.NewHexColor(0xeeeeec)

	cells := make([][]proto.Cell, 8)
	for y := range cells {
		row := make([]proto.Cell, 30)
		for x := range row {
			row[x] = proto.Cell{Ch: ' ', FG: 1 << 24, BG: 1 << 24} // unstyled
		}
		cells[y] = row
	}
	f := proto.Frame{
		Cols: 80, Rows: 24,
		ShowStatus:   true,
		StatusPrefix: " termdock:test ",
		StatusText:   " | active pane: 1",
		Windows: []proto.WindowTab{
			{Index: 0, Label: " 0:bash ", Active: true, X: 16, W: 8},
			{Index: 1, Label: " 1:vim ", X: 24, W: 7}, // inactive: used to be a black hole
		},
		Panes: []proto.PaneFrame{
			{ID: 1, Rect: proto.Rect{X: 1, Y: 1, W: 30, H: 8}, Active: true, Cells: cells},
			{ID: 2, Rect: proto.Rect{X: 33, Y: 1, W: 30, H: 8}, Cells: cells},
		},
	}

	screen := simScreen(t, 80, 24)
	draw(screen, f, cfg)

	got, w, h := screen.GetContents()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			_, bg, _ := got[y*w+x].Style.Decompose()
			if bg == tcell.ColorDefault {
				t.Fatalf("cell (%d,%d) still has the terminal's default background — a themed frame must not leave gaps", x, y)
			}
		}
	}
}

// TestTextWidthCountsColumnsNotRunes is the measurement the status bar's
// right-alignment depends on. A rune count is wrong in both directions:
// 🔋 is one rune drawn two columns wide, 🖥️ is two runes (base plus a
// variation selector) drawn as one glyph.
func TestTextWidthCountsColumnsNotRunes(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want int
	}{
		{"cpu 42%", 7},
		{" main", 6}, // a Nerd Font glyph is one column
		{"🔋", 2},
		{"🖥️", 2},
		{"", 0},
	} {
		if got := textWidth(tc.s); got != tc.want {
			t.Errorf("textWidth(%q) = %d, want %d", tc.s, got, tc.want)
		}
	}
}

// A wide glyph must advance the cursor by two, or whatever follows lands
// on its second half and both are corrupted.
func TestWideGlyphsDoNotOverlapWhatFollows(t *testing.T) {
	screen := simScreen(t, 20, 3)
	overlayText(screen, 0, 0, 20, tcell.StyleDefault, "🔋ab")
	screen.Show()

	cells, w, _ := screen.GetContents()
	// The battery occupies columns 0-1, so 'a' must be at column 2.
	at := func(x int) rune {
		r := cells[0*w+x].Runes
		if len(r) == 0 {
			return ' '
		}
		return r[0]
	}
	if got := at(2); got != 'a' {
		t.Errorf("after a two-column glyph, column 2 = %q, want 'a'", got)
	}
	if got := at(3); got != 'b' {
		t.Errorf("column 3 = %q, want 'b'", got)
	}
}

// Highlights used to be black text on the theme's accent whatever that
// accent was: fine on Nord's pale frost blue, poor on Solarized's mid
// blue or Dracula's purple. A fixed foreground cannot be right for
// eleven palettes, so it is chosen per colour.
func TestHighlightForegroundIsReadableOnEveryAccent(t *testing.T) {
	dark := []int32{0x268bd2, 0xbd93f9, 0xe95420, 0x7aa2f7, 0x61afef}  // want white
	light := []int32{0x88c0d0, 0xc4a7e7, 0xa7c080, 0xcba6f7, 0xa6e22e} // want black

	for _, hex := range dark {
		if got := readableOn(tcell.NewHexColor(hex)); got != tcell.ColorWhite {
			t.Errorf("#%06x is dark; got %v, want white text on it", hex, got)
		}
	}
	for _, hex := range light {
		if got := readableOn(tcell.NewHexColor(hex)); got != tcell.ColorBlack {
			t.Errorf("#%06x is light; got %v, want black text on it", hex, got)
		}
	}
}

// The focused pane is marked by drawing its outline in the accent colour.
// That outline has to read as a rectangle, which means its four corners
// must be corners — but glyphs are tiled from the union of every pane's
// outline, so a corner that sits against a neighbour tiles as a T and the
// highlight grows an arm poking into the pane next door.
func TestFocusedPaneOutlineClosesAtItsCorners(t *testing.T) {
	// A vertical split: the focused pane is the left one, so its right-hand
	// corners land on the shared divider — the case that went wrong.
	left := proto.Rect{X: 1, Y: 1, W: 20, H: 21}
	right := proto.Rect{X: 22, Y: 1, W: 20, H: 21}

	cells := map[[2]int]bool{}
	addRectBorder(cells, left)
	addRectBorder(cells, right)
	corners := cornerRunes(left)

	at := func(x, y int) rune {
		ch := boxChar(cells[[2]int{x, y - 1}], cells[[2]int{x, y + 1}],
			cells[[2]int{x - 1, y}], cells[[2]int{x + 1, y}])
		if c, ok := corners[[2]int{x, y}]; ok {
			ch = c
		}
		return ch
	}

	// Where the focused pane's outline meets the divider it must still be
	// a corner, not the '┬'/'┴' the union tiles there.
	for _, tc := range []struct {
		x, y int
		want rune
		desc string
	}{
		{0, 0, '┌', "top-left, against the outer frame"},
		{21, 0, '┐', "top-right, against the divider"},
		{0, 22, '└', "bottom-left, against the outer frame"},
		{21, 22, '┘', "bottom-right, against the divider"},
	} {
		if got := at(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: got %q at (%d,%d), want %q", tc.desc, got, tc.x, tc.y, tc.want)
		}
	}
}

// Closing the corners must not be done by tiling the focused outline
// against itself: that also drops the T-junctions along its edges, where
// a neighbour's divider genuinely runs into the focused pane's side, and
// leaves that divider floating unattached.
func TestFocusedOutlineKeepsJunctionsAlongItsEdges(t *testing.T) {
	left := proto.Rect{X: 1, Y: 1, W: 20, H: 21} // focused
	topRight := proto.Rect{X: 22, Y: 1, W: 20, H: 10}
	botRight := proto.Rect{X: 22, Y: 12, W: 20, H: 10}

	cells := map[[2]int]bool{}
	for _, r := range []proto.Rect{left, topRight, botRight} {
		addRectBorder(cells, r)
	}
	corners := cornerRunes(left)

	// (21,11) is on the focused pane's right edge, halfway down, where the
	// two right-hand panes' shared divider arrives.
	x, y := 21, 11
	if _, isCorner := corners[[2]int{x, y}]; isCorner {
		t.Fatalf("(%d,%d) should not be one of the focused pane's corners", x, y)
	}
	got := boxChar(cells[[2]int{x, y - 1}], cells[[2]int{x, y + 1}],
		cells[[2]int{x - 1, y}], cells[[2]int{x + 1, y}])
	if got != '├' {
		t.Errorf("the neighbours' divider must stay attached to the focused pane's edge: got %q at (%d,%d), want '├'", got, x, y)
	}
}
