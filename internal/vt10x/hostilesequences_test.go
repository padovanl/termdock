package vt10x

import (
	"math/rand"
	"strings"
	"testing"
)

// Beyond the CSI parameters: the other sequence families, each driven
// with well-formed but hostile content. Same stake as before, a panic
// here is every pane of every session in the daemon.
func TestOSCAndDCSSurviveHostileContent(t *testing.T) {
	term := New(WithSize(40, 10), WithHistoryLimit(100))

	payloads := []string{
		"", ";", ";;;;", strings.Repeat(";", 1000),
		strings.Repeat("A", 70000),
		"\x00\x01\x02", "\xff\xfe",
		strings.Repeat("😀", 500),
		"0;title", "2;", "133;", "133;D;notanumber", "133;D;-5",
		"133;D;99999999999999999999",
		"8;;http://example.com", "52;c;" + strings.Repeat("b", 10000),
		"10;?", "11;?", "4;256;rgb:ff/ff/ff",
	}
	terminators := []string{"\x07", "\x1b\\", ""}

	for _, p := range payloads {
		for _, term2 := range terminators {
			term.Write([]byte("\x1b]" + p + term2))
			term.Write([]byte("\x1bP" + p + term2)) // DCS
			term.Write([]byte("\x1b_" + p + term2)) // APC
			term.Write([]byte("\x1b^" + p + term2)) // PM
			term.Write([]byte("\x1bX" + p + term2)) // SOS
		}
	}
	readEverything(t, term)
}

// Charset selection, tab stops, alt screen and scroll regions, driven
// together: each keeps state the others move underneath it.
func TestCharsetsTabsAltScreenAndScrollRegions(t *testing.T) {
	term := New(WithSize(30, 8), WithHistoryLimit(80))

	seqs := []string{
		"\x1b(0", "\x1b(B", "\x1b)0", "\x1b)B", "\x1b*0", "\x1b+0",
		"\x1b(?", "\x1b(\xff", // designations nothing defines
		"\x0e", "\x0f", // shift out / shift in
		"\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?47l",
		"\x1b[1;3r", "\x1b[3;1r", "\x1b[0;0r", "\x1b[r",
		"\x1bH", "\x1b[0g", "\x1b[3g", "\x1b[g",
		"\x1bM", "\x1bD", "\x1bE", "\x1b7", "\x1b8",
		"\x1b[?6h", "\x1b[?6l", // origin mode
		"\x1b[?7h", "\x1b[?7l", // wrap
		"\x1b#8", // DEC alignment test, fills the screen
	}

	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 6000; i++ {
		term.Write([]byte(seqs[rng.Intn(len(seqs))]))
		if rng.Intn(3) == 0 {
			term.Write([]byte(strings.Repeat("x", rng.Intn(60))))
		}
		if rng.Intn(20) == 0 {
			term.Write([]byte("\r\n"))
		}
		if rng.Intn(50) == 0 {
			term.Resize(1+rng.Intn(60), 1+rng.Intn(20))
		}
	}
	readEverything(t, term)
}

// Text that is not simple: wide glyphs, combining marks, zero-width
// joiners and invalid UTF-8, written at the right-hand edge where a
// wide glyph does not fit.
func TestAwkwardTextAtTheEdge(t *testing.T) {
	for _, cols := range []int{1, 2, 3, 10, 40} {
		term := New(WithSize(cols, 4), WithHistoryLimit(20))
		for _, s := range []string{
			"漢字テスト", "á̂̃", "👨‍👩‍👧‍👦", "🇮🇹🇯🇵",
			"\xff\xfe\xfd", "\xc3", "\xe2\x82", // truncated UTF-8
			"\u200b\u200d\ufeff", // zero-width space, ZWJ, BOM
			strings.Repeat("漢", 50),
		} {
			for i := 0; i < 3; i++ {
				term.Write([]byte(s))
				term.Write([]byte("\r\n"))
			}
		}
		readEverything(t, term)
	}
}

// readEverything does what the renderer does on the next frame: touch
// every cell the terminal says it has, plus its scrollback and marks.
// An out-of-range read here is the crash, so this is the assertion.
func readEverything(t *testing.T, term Terminal) {
	t.Helper()
	cols, rows := term.Size()
	if cols < 0 || rows < 0 {
		t.Fatalf("terminal reports a %dx%d size", cols, rows)
	}
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			_ = term.Cell(x, y)
		}
	}
	for n := 0; n < term.HistoryLen(); n++ {
		for x := 0; x < cols; x++ {
			_ = term.HistoryCell(n, x)
		}
	}
	for _, m := range term.Marks() {
		if m.Line < 0 {
			t.Fatalf("a mark points at line %d", m.Line)
		}
	}
	_ = term.String()
	_ = term.Cursor()
}
