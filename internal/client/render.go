package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/proto"
)

// These mirror pane.AttrXxx (internal/pane/pane.go), which itself mirrors
// vt10x's glyph attribute bits. Duplicated here so the client doesn't need
// to import the pty/terminal-emulation packages just for 7 constants.
const (
	attrReverse = 1 << iota
	attrUnderline
	attrBold
	attrGfx
	attrItalic
	attrBlink
	attrWrap
)

func draw(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	// Clear() paints with the screen's default style, which is also what
	// shows through the margin around the layout and behind the pane
	// borders — so it has to carry the theme too, or a themed session
	// sits in a frame of the emulator's own background.
	screen.SetStyle(tcell.StyleDefault.Foreground(cfg.PaneFG).Background(cfg.PaneBG))
	screen.Clear()

	if f.Overview != nil {
		drawOverview(screen, f, cfg)
	} else {
		for _, p := range f.Panes {
			drawPaneContent(screen, p, cfg)
		}
		drawBorders(screen, f, cfg)
	}

	if f.ShowStatus {
		drawStatusBar(screen, f, cfg)
	}
	if f.Popup != nil {
		drawPopup(screen, *f.Popup, cfg)
	}
	if len(f.QuickJump) > 0 {
		drawQuickJump(screen, f.QuickJump, cfg)
	}
	if f.Overlay != nil {
		drawOverlay(screen, f, cfg)
	}
	// An overlay or the overview covers the pane area completely and
	// draws whatever cursor it wants as an ordinary character (the
	// picker's "_", say). tcell keeps the real one wherever it was last
	// placed, though, and screen.Clear() doesn't move it — so without
	// this it stays parked over the pane underneath, blinking through the
	// middle of the help box or the overview grid.
	if f.Overlay != nil || f.Overview != nil {
		screen.HideCursor()
	}
	screen.Show()
}

// drawQuickJump paints one highlighted digit badge centered over each
// tagged pane — Ctrl-B Q's display-panes equivalent — on top of the
// pane content already drawn, so the user can still tell what's running
// underneath while picking which one to jump to.
func drawQuickJump(screen tcell.Screen, tags []proto.QuickJumpTag, cfg config.Config) {
	style := tcell.StyleDefault.Background(cfg.PaneActiveBG).Foreground(tcell.ColorBlack).Bold(true)
	for _, t := range tags {
		r := t.Rect
		if r.W < 3 || r.H < 1 {
			continue
		}
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		screen.SetContent(cx-1, cy, ' ', nil, style)
		screen.SetContent(cx, cy, t.Digit, nil, style)
		screen.SetContent(cx+1, cy, ' ', nil, style)
	}
}

// drawPopup paints the Ctrl-B P floating scratch terminal: a bordered
// box, its content drawn exactly like an ordinary pane's (see
// drawPaneContent), on top of everything else already on screen.
func drawPopup(screen tcell.Screen, p proto.PaneFrame, cfg config.Config) {
	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		return
	}
	// Floating chrome sits on the palette's surface colour (what the
	// status bar uses), one step "above" the panes' background — a
	// hardcoded black box was a hole in any themed session.
	accent := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.PaneActiveBG).Bold(true)
	fillRect(screen, r.X-1, r.Y-1, r.W+2, r.H+2, tcell.StyleDefault.Background(cfg.StatusBG))
	drawFloatingBorder(screen, r.X-1, r.Y-1, r.W+2, r.H+2, accent)
	overlayText(screen, r.X+1, r.Y-1, r.X+r.W, accent, " "+p.Title+" ")
	drawPaneContent(screen, p, cfg)
}

// drawOverview paints the Ctrl-B g "mission control" grid: every pane's
// tile, already positioned and sized by the server (see
// Core.overviewLayout), drawn as a small bordered box with its title and
// a live content preview — the selected tile's border in the accent
// color, same convention as a pane's own border elsewhere.
func drawOverview(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	normalStyle := tcell.StyleDefault.Background(cfg.PaneBG).Foreground(tcell.ColorGray)
	accentStyle := tcell.StyleDefault.Background(cfg.PaneBG).Foreground(cfg.PaneActiveBG).Bold(true)
	for _, tile := range f.Overview.Tiles {
		r := tile.Rect
		if r.W <= 2 || r.H <= 2 {
			continue // too small to draw a border and any content
		}
		style := normalStyle
		if tile.Active {
			style = accentStyle
		}
		fillRect(screen, r.X, r.Y, r.W, r.H, tcell.StyleDefault.Background(cfg.PaneBG))
		drawFloatingBorder(screen, r.X, r.Y, r.W, r.H, style)
		overlayText(screen, r.X+2, r.Y, r.X+r.W-1, style, " "+tile.Title+" ")
		for y, row := range tile.Cells {
			for x, cell := range row {
				cx, cy := r.X+1+x, r.Y+1+y
				if cx < r.X+r.W-1 && cy < r.Y+r.H-1 {
					screen.SetContent(cx, cy, cellRune(cell), nil, cellStyle(cell, cfg))
				}
			}
		}
	}
}

func drawPaneContent(screen tcell.Screen, p proto.PaneFrame, cfg config.Config) {
	r := p.Rect
	if r.W <= 0 || r.H <= 0 {
		return
	}

	for y, row := range p.Cells {
		for x, cell := range row {
			screen.SetContent(r.X+x, r.Y+y, cellRune(cell), nil, cellStyle(cell, cfg))
		}
	}

	if p.Active {
		if p.CursorVisible {
			screen.ShowCursor(p.CursorX, p.CursorY)
		} else {
			screen.HideCursor()
		}
	}
}

// cellStyle and cellRune decode one proto.Cell into what tcell.SetContent
// wants — shared by real pane content (drawPaneContent), the overview's
// tiles and the picker's preview box, which all paint pane content.
//
// cellStyle turns one pane cell into a tcell style. A cell the running
// program never styled comes through as ColorDefault, which tcell hands
// to the terminal emulator's own default colours; substituting the
// theme's pane background/foreground there is what makes a theme cover
// the panes and not just termdock's own chrome. With no theme (and no
// explicit pane-bg/pane-fg) both are ColorDefault and this is a no-op,
// leaving the emulator's colours exactly as before.
func cellStyle(cell proto.Cell, cfg config.Config) tcell.Style {
	fg, bg := convColor(cell.FG), convColor(cell.BG)
	if fg == tcell.ColorDefault {
		fg = cfg.PaneFG
	}
	if bg == tcell.ColorDefault {
		bg = cfg.PaneBG
	}
	style := tcell.StyleDefault.Foreground(fg).Background(bg)
	if cell.Attr&attrBold != 0 {
		style = style.Bold(true)
	}
	if cell.Attr&attrUnderline != 0 {
		style = style.Underline(true)
	}
	if cell.Attr&attrReverse != 0 {
		style = style.Reverse(true)
	}
	if cell.Attr&attrBlink != 0 {
		style = style.Blink(true)
	}
	return style
}

func cellRune(cell proto.Cell) rune {
	if cell.Ch == 0 {
		return ' '
	}
	return cell.Ch
}

// drawBorders draws a one-cell border around every pane in f, entirely
// outside each pane's content Rect (in the margin reserved around the
// whole tree and the single row/column each split reserves between its
// two children — see internal/layout.Compute). Borders are inferred purely
// from the geometry of the pane Rects the server already sends, rather
// than an explicit divider list: two panes can only be geometrically
// adjacent if the split tree actually placed them that way, so unioning
// every pane's own outline and auto-tiling the box-drawing glyphs from
// each cell's neighbors naturally produces correctly joined T/cross
// junctions wherever three or four panes meet, with no tree-topology
// bookkeeping needed here. The active pane's own outline is drawn in the
// accent color so it's obvious at a glance which pane has focus (and,
// practically, which divider a click will grab for resizing).
func drawBorders(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	cells := map[[2]int]bool{}
	var active *proto.PaneFrame
	for i := range f.Panes {
		p := &f.Panes[i]
		if p.Rect.W <= 0 || p.Rect.H <= 0 {
			continue
		}
		addRectBorder(cells, p.Rect)
		if p.Active {
			active = p
		}
	}
	activeCells := map[[2]int]bool{}
	if active != nil {
		addRectBorder(activeCells, active.Rect)
	}

	// Backgrounded with the theme like everything else: a border drawn on
	// the emulator's default background framed every themed session in a
	// black grid.
	normalStyle := tcell.StyleDefault.Background(cfg.PaneBG).Foreground(tcell.ColorGray)
	activeStyle := tcell.StyleDefault.Background(cfg.PaneBG).Foreground(cfg.PaneActiveBG).Bold(true)
	if active != nil && active.Zoomed {
		// A visibly different accent from "just focused" — zoom hides
		// every other pane, so it's worth a color you can't mistake for
		// the ordinary single-pane-window case.
		activeStyle = tcell.StyleDefault.Background(cfg.PaneBG).Foreground(tcell.ColorFuchsia).Bold(true)
	}

	for cell := range cells {
		x, y := cell[0], cell[1]
		if x < 0 || y < 0 || x >= f.Cols || y >= f.Rows {
			continue
		}
		up := cells[[2]int{x, y - 1}]
		down := cells[[2]int{x, y + 1}]
		left := cells[[2]int{x - 1, y}]
		right := cells[[2]int{x + 1, y}]
		style := normalStyle
		if activeCells[cell] {
			style = activeStyle
		}
		screen.SetContent(x, y, boxChar(up, down, left, right), nil, style)
	}

	for i := range f.Panes {
		p := &f.Panes[i]
		if p.Rect.W <= 0 || p.Rect.H <= 0 {
			continue
		}
		style := normalStyle
		if p.Active {
			style = activeStyle
		}
		drawPaneTitle(screen, p.Rect, p.Title, style, f.Cols, f.Rows)
	}
}

// addRectBorder marks the perimeter of r, expanded by one cell on every
// side, as border cells.
func addRectBorder(cells map[[2]int]bool, r proto.Rect) {
	x0, y0 := r.X-1, r.Y-1
	x1, y1 := r.X+r.W, r.Y+r.H
	for x := x0; x <= x1; x++ {
		cells[[2]int{x, y0}] = true
		cells[[2]int{x, y1}] = true
	}
	for y := y0; y <= y1; y++ {
		cells[[2]int{x0, y}] = true
		cells[[2]int{x1, y}] = true
	}
}

// boxChar picks the Unicode box-drawing glyph matching which of a border
// cell's four neighbors are themselves border cells.
func boxChar(up, down, left, right bool) rune {
	switch {
	case up && down && left && right:
		return '┼'
	case down && left && right:
		return '┬'
	case up && left && right:
		return '┴'
	case up && down && right:
		return '├'
	case up && down && left:
		return '┤'
	case down && right:
		return '┌'
	case down && left:
		return '┐'
	case up && right:
		return '└'
	case up && left:
		return '┘'
	case left, right:
		return '─'
	case up, down:
		return '│'
	default:
		return ' '
	}
}

// drawPaneTitle overlays a pane's title on its own top border edge, e.g.
// "┌─ 1:bash ──────┐". Skipped entirely if the pane is too narrow to fit
// anything more than its corners.
func drawPaneTitle(screen tcell.Screen, r proto.Rect, title string, style tcell.Style, cols, rows int) {
	y := r.Y - 1
	if y < 0 || y >= rows {
		return
	}
	label := " " + title + " "
	x := r.X + 1 // one cell in from the top-left corner/dash
	maxW := r.W - 2
	if maxW < 1 {
		return
	}
	i := 0
	for _, ch := range label {
		if i >= maxW {
			break
		}
		cx := x + i
		if cx >= 0 && cx < cols {
			screen.SetContent(cx, y, ch, nil, style)
		}
		i++
	}
}

// drawOverlay paints the jump picker or the help screen (or any future
// modal list) as a centered floating box on top of everything already
// drawn: a title, optionally the live query with a block cursor
// (ov.ShowQuery — off for a static list like help), and a scrollable
// item list, selection-highlighted when ov.Selectable — termdock's
// answer to tmux's choose-tree, but type-ahead filterable instead of a
// static list you page through. When the selected item has a live
// preview, a second box is drawn alongside it (see drawPreviewBox).
// minOverlayInnerW is the narrowest a modal list box may get squeezed to
// when making room for a preview beside it — below this the list stops
// being readable and giving the preview more width is a net loss.
const minOverlayInnerW = 24

func drawOverlay(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	ov := f.Overlay

	// The list box and the preview box beside it share one row of screen
	// width, so size them together rather than letting the list take
	// whatever it likes first. A picker's title is a full sentence of
	// instructions (~70 characters): on a wide terminal there's room for
	// it on one line *and* a full preview, which reads best, so it sets
	// the list's preferred width. When both don't fit, the list gives
	// ground first — down to what its items actually need, with the title
	// wrapping onto extra lines — because an unreadably narrow preview is
	// worth less than a wrapped title. Only if that still isn't enough
	// does the preview itself get cropped (and eventually dropped, see
	// below).
	itemsW := 0
	for _, it := range ov.Items {
		if l := len([]rune(it)) + 2; l > itemsW {
			itemsW = l
		}
	}
	preferredW := maxi(itemsW, len([]rune(ov.Title))+2)

	rawPreviewW, previewH := 0, 0
	if len(ov.PreviewCells) > 0 {
		previewH = len(ov.PreviewCells) + 2
		rawPreviewW = len(ov.PreviewCells[0]) + 2
	}

	innerW := preferredW
	if rawPreviewW > 0 {
		if room := f.Cols - 2 - 1 - rawPreviewW; innerW > room {
			innerW = maxi(room, maxi(itemsW, minOverlayInnerW))
		}
	}
	if maxW := f.Cols - 4; innerW > maxW {
		innerW = maxW
	}
	if innerW < minOverlayInnerW {
		innerW = minOverlayInnerW
	}
	w := innerW + 2 // + left/right border

	titleLines := wordWrap(ov.Title, innerW-1, 3) // -1 for the leading space drawn with it
	headerRows := len(titleLines)
	if ov.ShowQuery {
		headerRows += 2 // + query + separator
	}
	listRows := len(ov.Items)
	if maxList := f.Rows - headerRows - 3; listRows > maxList {
		listRows = maxList
	}
	if listRows < 1 {
		listRows = 1
	}
	h := headerRows + listRows + 2 // + top/bottom border

	if w > f.Cols {
		w = f.Cols
	}
	if h > f.Rows {
		h = f.Rows
	}

	// The preview is a second box bolted onto the right edge of the list
	// box — centered as one unit with it, so both stay visually joined
	// instead of the list sliding around independently as the selection
	// (and so the preview's presence/size) changes. Crop it to whatever
	// room the list left rather than dropping it outright: an
	// all-or-nothing preview on a narrow terminal is always "nothing",
	// and a narrow peek beats none. Below minPreviewW there's nothing
	// legible left, so at that point it does drop.
	const minPreviewW = 12
	previewW := rawPreviewW
	if previewW > 0 {
		if room := f.Cols - w - 1; previewW > room {
			previewW = room
		}
		if previewW < minPreviewW+2 {
			previewW = 0
		}
	}
	if previewH > f.Rows {
		previewH = f.Rows
	}
	totalW := w
	if previewW > 0 {
		totalW = w + 1 + previewW
	}
	totalH := maxi(h, previewH)

	x0 := maxi(0, (f.Cols-totalW)/2)
	y0 := maxi(0, (f.Rows-totalH)/2)

	bg := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.StatusFG)
	accent := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.PaneActiveBG).Bold(true)
	// The title carries the overlay's instructions ("type to filter, ↑↓
	// select, enter jump, esc cancel") and "no matches" is an answer, so
	// both are text to read and get the status bar's own foreground —
	// 8.6:1 against the Dracula palette, where a hardcoded grey managed
	// 2.3:1 and was the one element in the box that ignored the theme
	// entirely. The rule under the query is a divider rather than
	// writing, and is the only thing still drawn faintly.
	title := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.StatusFG)
	rule := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(tcell.ColorGray)

	fillRect(screen, x0, y0, w, h, bg)
	drawFloatingBorder(screen, x0, y0, w, h, accent)

	innerX, maxX := x0+1, x0+1+innerW
	for i, line := range titleLines {
		overlayText(screen, innerX, y0+1+i, maxX, title, " "+line)
	}
	if ov.ShowQuery {
		queryY := y0 + len(titleLines) + 1
		overlayText(screen, innerX, queryY, maxX, bg.Bold(true), " > "+ov.Query+"_")
		for x := innerX; x < maxX; x++ {
			screen.SetContent(x, queryY+1, '─', nil, rule)
		}
	}

	if len(ov.Items) == 0 {
		overlayText(screen, innerX, y0+headerRows+1, maxX, title, " no matches")
		return
	}

	// A selectable list scrolls only as far as it must to keep the
	// highlighted row on screen; a non-selectable one (help) has no
	// highlight at all, so its Selected is the scroll offset itself and
	// becomes the first visible row directly — see proto.Overlay.
	// Running that offset through the keep-the-selection-visible math is
	// what used to make the help screen look like it didn't scroll until
	// the offset had passed a whole screenful.
	maxStart := maxi(0, len(ov.Items)-listRows)
	start := ov.Selected
	if ov.Selectable {
		start = ov.Selected - listRows + 1
	}
	start = clampi(start, 0, maxStart)
	for i := 0; i < listRows && start+i < len(ov.Items); i++ {
		idx := start + i
		row := y0 + headerRows + 1 + i
		style := bg
		if ov.Selectable && idx == ov.Selected {
			style = tcell.StyleDefault.Background(cfg.PaneActiveBG).Foreground(tcell.ColorBlack).Bold(true)
			for x := innerX; x < maxX; x++ {
				screen.SetContent(x, row, ' ', nil, style)
			}
		}
		overlayText(screen, innerX, row, maxX, style, " "+ov.Items[idx])
	}

	if previewW > 0 {
		drawPreviewBox(screen, x0+w+1, y0, previewW, previewH, ov.PreviewCells, cfg)
	}
}

// wordWrap splits text into lines of at most width runes, breaking on
// spaces rather than mid-word — used for a picker's title, which is
// always a fixed instructional sentence, not user data, so this doesn't
// need to handle pathological input beyond a single word longer than
// width (hard-broken as a fallback). At most maxLines are returned; any
// remaining words are dropped rather than growing the box further.
func wordWrap(text string, width, maxLines int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			lines = append(lines, string(cur))
			cur = cur[:0]
		}
	}
	for _, word := range strings.Fields(text) {
		w := []rune(word)
		for len(w) > width { // a single word wider than the whole box
			flush()
			lines = append(lines, string(w[:width]))
			w = w[width:]
		}
		if len(cur) == 0 {
			cur = append(cur, w...)
		} else if len(cur)+1+len(w) <= width {
			cur = append(cur, ' ')
			cur = append(cur, w...)
		} else {
			flush()
			cur = append(cur, w...)
		}
		if len(lines) >= maxLines {
			return lines[:maxLines]
		}
	}
	flush()
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines
}

// fillRect blank-fills a rectangle in the given style.
func fillRect(screen tcell.Screen, x0, y0, w, h int, style tcell.Style) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			screen.SetContent(x, y, ' ', nil, style)
		}
	}
}

// drawFloatingBorder draws a plain rectangular box border — unlike
// drawBorders' pane dividers, a floating overlay box never shares an edge
// with anything else, so there's no junction-glyph logic needed here.
func drawFloatingBorder(screen tcell.Screen, x0, y0, w, h int, style tcell.Style) {
	for x := x0; x < x0+w; x++ {
		screen.SetContent(x, y0, '─', nil, style)
		screen.SetContent(x, y0+h-1, '─', nil, style)
	}
	for y := y0; y < y0+h; y++ {
		screen.SetContent(x0, y, '│', nil, style)
		screen.SetContent(x0+w-1, y, '│', nil, style)
	}
	screen.SetContent(x0, y0, '┌', nil, style)
	screen.SetContent(x0+w-1, y0, '┐', nil, style)
	screen.SetContent(x0, y0+h-1, '└', nil, style)
	screen.SetContent(x0+w-1, y0+h-1, '┘', nil, style)
}

// drawPreviewBox paints the picker's live pane-content preview: a bordered
// box the exact size of cells (plus border), rendered with the same cell
// styling as a real pane (see drawPaneContent).
func drawPreviewBox(screen tcell.Screen, x0, y0, w, h int, cells [][]proto.Cell, cfg config.Config) {
	accent := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.PaneActiveBG).Bold(true)
	fillRect(screen, x0, y0, w, h, tcell.StyleDefault.Background(cfg.StatusBG))
	drawFloatingBorder(screen, x0, y0, w, h, accent)
	// The box may have been cropped narrower/shorter than the cells the
	// server sent (see drawOverlay), so clip rather than drawing over the
	// border and whatever's beyond it.
	for y, row := range cells {
		if y >= h-2 { // -2 for the top/bottom border rows
			break
		}
		for x, cell := range row {
			if x >= w-2 { // likewise for the left/right border columns
				break
			}
			screen.SetContent(x0+1+x, y0+1+y, cellRune(cell), nil, cellStyle(cell, cfg))
		}
	}
}

// drawStatusBar paints the status line in three pieces after a full-row
// background fill: StatusPrefix and StatusText in the line's base style,
// and the window tab strip in between with its own per-tab styling (see
// tabStyle) so the active window and ones with unseen activity stand out
// visually, not just via the "*"/"!" markers already baked into each
// tab's Label.
func drawStatusBar(screen tcell.Screen, f proto.Frame, cfg config.Config) {
	y := f.Rows - 1
	if y < 0 {
		return
	}
	style := tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.StatusFG)
	switch f.StatusStyle {
	case "prefix":
		style = tcell.StyleDefault.Background(tcell.ColorDarkGreen).Foreground(tcell.ColorWhite)
	case "mode":
		style = tcell.StyleDefault.Background(tcell.ColorPurple).Foreground(tcell.ColorWhite)
	case "confirm":
		style = tcell.StyleDefault.Background(tcell.ColorDarkRed).Foreground(tcell.ColorWhite).Bold(true)
	}
	drawText(screen, 0, y, f.Cols, style, "") // blank-fill the row

	overlayText(screen, 0, y, f.Cols, style, f.StatusPrefix)
	suffixX := textWidth(f.StatusPrefix)
	for _, t := range f.Windows {
		overlayText(screen, t.X, y, f.Cols, tabStyle(t, cfg), t.Label)
		if end := t.X + t.W; end > suffixX {
			suffixX = end
		}
	}
	overlayText(screen, suffixX, y, f.Cols, style, f.StatusText)

	rw := textWidth(f.StatusRight)
	if rw > 0 {
		leftLen := suffixX + textWidth(f.StatusText)
		if leftLen > f.Cols {
			leftLen = f.Cols // the left side was itself clipped to the screen width
		}
		x := f.Cols - rw
		if x > leftLen { // don't clobber the left side on a narrow terminal
			overlayText(screen, x, y, f.Cols, style, f.StatusRight)
		}
	}
}

// tabStyle picks a window tab's background: the accent color for the one
// you're looking at, a warm color for one that produced output while you
// weren't, and a neutral one blending into the status bar otherwise.
func tabStyle(t proto.WindowTab, cfg config.Config) tcell.Style {
	switch {
	case t.Dragging:
		// Distinct from Active on purpose: "picked up and moving," not
		// "this is where you are" — the two can coincide (dragging the
		// current tab) or not (dragging a background one).
		return tcell.StyleDefault.Background(tcell.ColorGray).Foreground(tcell.ColorBlack).Bold(true).Underline(true)
	case t.Active:
		return tcell.StyleDefault.Background(cfg.PaneActiveBG).Foreground(tcell.ColorBlack).Bold(true)
	case t.Activity:
		return tcell.StyleDefault.Background(tcell.ColorDarkOrange).Foreground(tcell.ColorBlack)
	default:
		// Explicitly the bar's own background: left at the default it was
		// the emulator's, punching black gaps through a themed status bar.
		return tcell.StyleDefault.Background(cfg.StatusBG).Foreground(cfg.StatusFG)
	}
}

// overlayText draws text starting at column x without first blanking the
// row (unlike drawText), clipped to the screen's width.
func overlayText(screen tcell.Screen, x, y, cols int, style tcell.Style, text string) {
	for _, r := range text {
		if x >= cols {
			return
		}
		if x >= 0 {
			screen.SetContent(x, y, r, nil, style)
		}
		// Advance by the columns the glyph actually takes: a wide one
		// occupies two cells, and stepping by one would let the next
		// character land on its second half. A zero-width mark advances
		// nothing, attaching to the glyph before it.
		x += runeWidth(r)
	}
}

func drawText(screen tcell.Screen, x, y, w int, style tcell.Style, text string) {
	for i := 0; i < w; i++ {
		screen.SetContent(x+i, y, ' ', nil, style)
	}
	i := 0
	for _, r := range text {
		rw := runeWidth(r)
		if i+rw > w {
			break // a wide glyph that would only half fit is not drawn at all
		}
		screen.SetContent(x+i, y, r, nil, style)
		i += rw
	}
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampi(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// convColor decodes a raw vt10x.Color value (see internal/vt10x/color.go):
// [0,16) base ANSI, [16,256) xterm 256-color palette, [256, 1<<24) 24-bit
// truecolor packed as r<<16|g<<8|b, and >= 1<<24 the terminal's default
// fg/bg/cursor sentinels.
func convColor(raw uint32) tcell.Color {
	switch {
	case raw >= 1<<24:
		return tcell.ColorDefault
	case raw < 256:
		return tcell.PaletteColor(int(raw))
	default:
		r := int32(raw>>16) & 0xff
		g := int32(raw>>8) & 0xff
		b := int32(raw) & 0xff
		return tcell.NewRGBColor(r, g, b)
	}
}
