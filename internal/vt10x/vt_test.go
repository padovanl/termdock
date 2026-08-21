package vt10x

import (
	"io"
	"strings"
	"testing"
)

func extractStr(term Terminal, x0, x1, row int) string {
	var s []rune
	for i := x0; i <= x1; i++ {
		attr := term.Cell(i, row)
		s = append(s, attr.Char)
	}
	return string(s)
}

func TestPlainChars(t *testing.T) {
	term := New()
	expected := "Hello world!"
	_, err := term.Write([]byte(expected))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	actual := extractStr(term, 0, len(expected)-1, 0)
	if expected != actual {
		t.Fatal(actual)
	}
}

func TestNewline(t *testing.T) {
	term := New()
	expected := "Hello world!\n...and more."
	_, err := term.Write([]byte("\033[20h")) // set CRLF mode
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	_, err = term.Write([]byte(expected))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}

	split := strings.Split(expected, "\n")
	actual := extractStr(term, 0, len(split[0])-1, 0)
	actual += "\n"
	actual += extractStr(term, 0, len(split[1])-1, 1)
	if expected != actual {
		t.Fatal(actual)
	}

	// A newline with a color set should not make the next line that color,
	// which used to happen if it caused a scroll event.
	st := (term.(*terminal))
	st.moveTo(0, st.rows-1)
	_, err = term.Write([]byte("\033[1;37m\n$ \033[m"))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	cur := term.Cursor()
	attr := term.Cell(cur.X, cur.Y)
	if attr.FG != DefaultFG {
		t.Fatal(st.cur.X, st.cur.Y, attr.FG, attr.BG)
	}
}

// TestCellOutOfRangeIsEmptyNotAPanic: callers read the grid through
// absolute row indices that outlive the snapshot they were computed
// from — a resize changes the row count, and scrollback passing its
// limit drops lines off the front, shifting every index above it. An
// out-of-range read used to be an index panic, which in termdock's
// single-process server takes every pane in every session down with it.
func TestCellOutOfRangeIsEmptyNotAPanic(t *testing.T) {
	term := New(WithSize(20, 5))
	term.Write([]byte("hi"))
	term.Lock()
	defer term.Unlock()

	cols, rows := term.Size()
	for _, p := range [][2]int{
		{0, rows}, {0, rows + 100}, {0, -1},
		{cols, 0}, {cols + 100, 0}, {-1, 0},
	} {
		if g := term.Cell(p[0], p[1]); g != (Glyph{}) {
			t.Errorf("Cell(%d, %d) = %+v, want the zero Glyph", p[0], p[1], g)
		}
	}
	if g := term.Cell(0, 0); g.Char != 'h' {
		t.Errorf("an in-range read must still work, Cell(0,0).Char = %q", g.Char)
	}
}
