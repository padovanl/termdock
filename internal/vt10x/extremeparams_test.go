package vt10x

import (
	"fmt"
	"strings"
	"testing"
)

// Sequences that are well-formed but extreme: the parser has to survive
// parameters far outside any real terminal's range, since a program can
// emit them by accident (a bad ncurses build, a corrupted stream) and
// the daemon renders every session in one process.
func TestParserSurvivesExtremeParameters(t *testing.T) {
	term := New(WithSize(80, 24), WithHistoryLimit(200))

	huge := []string{
		"999999999", "2147483647", "4294967295", "9223372036854775807",
		"99999999999999999999", "0", "-1", "",
	}
	finals := "ABCDEFGHJKLMPSTXZ@`abcdefghilmnpqrstuvwxyz"

	for _, p := range huge {
		for _, f := range finals {
			term.Write([]byte("\x1b[" + p + string(f)))
			term.Write([]byte("\x1b[" + p + ";" + p + string(f)))
			term.Write([]byte("\x1b[?" + p + "h"))
			term.Write([]byte("\x1b[?" + p + "l"))
		}
	}

	// Many parameters in one sequence.
	term.Write([]byte("\x1b[" + strings.Repeat("1;", 500) + "m"))
	// A scroll region beyond the screen, then output that has to scroll.
	term.Write([]byte("\x1b[9999;99999r"))
	term.Write([]byte(strings.Repeat("line\r\n", 300)))
	// Cursor addressing far outside the grid.
	for _, s := range []string{"\x1b[99999;99999H", "\x1b[0;0H", "\x1b[;H"} {
		term.Write([]byte(s))
		term.Write([]byte("x"))
	}
	// Tab stops, insert/delete of absurd counts.
	term.Write([]byte("\x1b[99999b\x1b[99999@\x1b[99999P\x1b[99999L\x1b[99999M\x1b[99999X"))
	// OSC with an enormous payload and no terminator, then one with.
	term.Write([]byte("\x1b]0;" + strings.Repeat("t", 100000)))
	term.Write([]byte("\x07"))
	term.Write([]byte("\x1b]133;" + strings.Repeat("A", 10000) + "\x07"))

	cols, rows := term.Size()
	if cols <= 0 || rows <= 0 {
		t.Fatalf("terminal reports a %dx%d size after the stress", cols, rows)
	}
	// Every cell must still be readable: that is what the renderer does
	// on the next frame, and an out-of-range read here panics the daemon.
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
		_ = m
	}
	fmt.Fprint(&strings.Builder{}, term.String())
}

// Resizing while the buffer is full, repeatedly, in both directions and
// through degenerate sizes.
func TestResizeThroughDegenerateSizes(t *testing.T) {
	term := New(WithSize(80, 24), WithHistoryLimit(100))
	term.Write([]byte(strings.Repeat("filling the screen with text\r\n", 200)))

	for _, sz := range [][2]int{
		{1, 1}, {0, 0}, {-1, -1}, {200, 60}, {1, 200}, {200, 1},
		{80, 24}, {0, 24}, {80, 0}, {3, 3}, {500, 500}, {80, 24},
	} {
		term.Resize(sz[0], sz[1])
		term.Write([]byte("after resize\r\n"))
		cols, rows := term.Size()
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				_ = term.Cell(x, y)
			}
		}
		if cols < 0 || rows < 0 {
			t.Fatalf("resize to %v left a %dx%d terminal", sz, cols, rows)
		}
	}
}

// The exact sequence that brought the daemon down, and its relatives.
// Each handler here does arithmetic on a parameter and then indexes a
// line with the result: "\033[<huge>P" made "cursor column + n" overflow
// int64 into a negative index. A crash in the emulator is not a wrong
// picture on one pane, it is every pane of every session in the process,
// since they all live in the one daemon.
func TestHugeParametersCannotOverflowIntoANegativeIndex(t *testing.T) {
	for _, seq := range []string{
		"\x1b[9223372036854775807P", // DCH, the one that was reported
		"\x1b[9223372036854775807@", // ICH
		"\x1b[9223372036854775807X", // ECH
		"\x1b[9223372036854775807L", // IL
		"\x1b[9223372036854775807M", // DL
		"\x1b[9223372036854775807C", // CUF
		"\x1b[9223372036854775807D", // CUB
		"\x1b[9223372036854775807b", // REP
		"\x1b[9223372036854775807;9223372036854775807H",
		"\x1b[9223372036854775807;9223372036854775807r",
		"\x1b[-1P", // Atoi accepts a sign; ECMA-48 has no negative parameters
		"\x1b[-1@",
		"\x1b[-99;-99H",
	} {
		term := New(WithSize(20, 5), WithHistoryLimit(50))
		term.Write([]byte("some text on the line"))
		term.Write([]byte("\x1b[3;5H")) // somewhere in the middle
		term.Write([]byte(seq))         // must not panic

		cols, rows := term.Size()
		for y := 0; y < rows; y++ {
			for x := 0; x < cols; x++ {
				_ = term.Cell(x, y)
			}
		}
	}
}

// The clamp is what makes the above safe, so pin its edges: a negative
// parameter reads as zero, and anything past the cap stops there.
func TestCSIParametersAreClamped(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, 0},
		{1, 1},
		{maxCSIParam, maxCSIParam},
		{maxCSIParam + 1, maxCSIParam},
		{1 << 62, maxCSIParam},
		{-1, 0},
		{-(1 << 62), 0},
	} {
		if got := clampCSIParam(tc.in); got != tc.want {
			t.Errorf("clampCSIParam(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
