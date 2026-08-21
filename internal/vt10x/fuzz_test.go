package vt10x

import (
	"math/rand"
	"testing"
)

// TestParserSurvivesAdversarialOutput: everything a pane's program
// writes lands in this parser, so it sees whatever a misbehaving program
// emits — truncated escapes, absurd parameters, control bytes in the
// middle of a sequence, invalid UTF-8. A panic here takes the whole
// daemon down, and with it every pane in every window, so "malformed
// input is someone else's problem" is not available as an answer.
func TestParserSurvivesAdversarialOutput(t *testing.T) {
	pieces := []string{
		"\x1b[", "\x1b]", "\x1b", "\x1b[?", "\x1b[999999999999999999999;99999H",
		"\x1b[38;2;255;0;0m", "\x1b[48;5;300m", "\x1b[-1;-1H", "\x1b[;;;;;;;;;;;;;;;;m",
		"\x1b#8", "\x1b[6n", "\x1b[r", "\x1b[1;1r", "\x1b[0;0r", "\x1b[99;1r",
		"\x1b]0;title\a", "\x1b]52;c;", "\x1b P", "\x1b\\", "\x1b[?1049h", "\x1b[?1049l",
		"\x1b[?25l", "\x1b[2J", "\x1b[3J", "\x1b[K", "\x1b[10M", "\x1b[10L", "\x1b[999S", "\x1b[999T",
		"\x1b[@", "\x1b[999@", "\x1b[999P", "\x1b[999X", "\x1b[999b", "\x1bM", "\x1bD", "\x1bE",
		"\r", "\n", "\t", "\b", "\x00", "\x7f", "\x0e", "\x0f", "\x18", "\x1a",
		"hello", "ħëłłø", "🐳🐳🐳", "\xff\xfe\xfd", "\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18m",
	}
	sizes := [][2]int{{1, 1}, {2, 3}, {80, 24}, {200, 60}, {5, 1}}

	for seed := int64(0); seed < 300; seed++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("PANIC seed=%d: %v", seed, r)
				}
			}()
			rng := rand.New(rand.NewSource(seed))
			sz := sizes[rng.Intn(len(sizes))]
			term := New(WithSize(sz[0], sz[1]))
			for i := 0; i < 120; i++ {
				switch rng.Intn(10) {
				case 0:
					ns := sizes[rng.Intn(len(sizes))]
					term.Resize(ns[0], ns[1])
				case 1:
					// Random bytes, not just well-formed pieces.
					b := make([]byte, rng.Intn(24))
					rng.Read(b)
					term.Write(b)
				default:
					term.Write([]byte(pieces[rng.Intn(len(pieces))]))
				}
				// Read it back the way Frame() and copy-mode do.
				term.Lock()
				cols, rows := term.Size()
				hl := term.HistoryLen()
				for y := 0; y < rows; y++ {
					for x := 0; x < cols; x++ {
						_ = term.Cell(x, y)
					}
				}
				for n := 0; n < hl; n++ {
					for x := 0; x < cols; x++ {
						_ = term.HistoryCell(n, x)
					}
				}
				_ = term.Cursor()
				_ = term.CursorVisible()
				term.Unlock()
			}
		}()
	}
}
