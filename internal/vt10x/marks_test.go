package vt10x

import (
	"strings"
	"testing"
)

func newMarkTerm(t *testing.T, cols, rows int) *terminal {
	t.Helper()
	term := New(WithSize(cols, rows), WithHistoryLimit(1000))
	st, ok := term.(*terminal)
	if !ok {
		t.Fatalf("unexpected terminal type %T", term)
	}
	return st
}

// osc133 is what a shell configured for semantic prompts emits.
func osc133(body string) string { return "\x1b]133;" + body + "\x07" }

func TestMarksRecordEachSemanticSequence(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("A") + "$ " + osc133("B") + "ls\r\n" + osc133("C") + "file\r\n" + osc133("D;0")))

	marks := term.Marks()
	var kinds []string
	for _, m := range marks {
		kinds = append(kinds, m.Kind.String())
	}
	got := strings.Join(kinds, ",")
	if got != "prompt,input,output,done" {
		t.Fatalf("recorded %q, want prompt,input,output,done", got)
	}
	last := marks[len(marks)-1]
	if last.Exit != 0 {
		t.Errorf("exit status = %d, want 0", last.Exit)
	}
}

// A failing command's status has to survive, since telling success from
// failure is most of the point.
func TestMarksCarryANonZeroExitStatus(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("C") + "boom\r\n" + osc133("D;127")))

	marks := term.Marks()
	last := marks[len(marks)-1]
	if last.Kind != MarkDone || last.Exit != 127 {
		t.Fatalf("last mark = %v exit %d, want done exit 127", last.Kind, last.Exit)
	}
}

// "D" with no status is genuinely different from "D;0" — the shell said
// nothing rather than saying success — and must not be flattened into it.
func TestMarkDoneWithoutStatusIsNotSuccess(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("D")))

	marks := term.Marks()
	if len(marks) != 1 || marks[0].Exit != -1 {
		t.Fatalf("marks = %+v, want one with exit -1", marks)
	}
}

// Shells and terminals append their own attributes after the letter.
// Ignoring the ones we don't use means a snippet written for another
// terminal still works rather than being rejected over an unknown key.
func TestMarksIgnoreUnknownAttributes(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("A;aid=7;cl=m") + osc133("D;3;aid=7")))

	marks := term.Marks()
	if len(marks) != 2 {
		t.Fatalf("recorded %d marks, want 2", len(marks))
	}
	if marks[0].Kind != MarkPrompt {
		t.Errorf("first mark = %v, want prompt", marks[0].Kind)
	}
	if marks[1].Exit != 3 {
		t.Errorf("exit = %d, want 3 despite the trailing attribute", marks[1].Exit)
	}
}

// The whole design rests on this: a mark keeps pointing at its own text
// as the buffer scrolls underneath it.
func TestMarkLineFollowsItsTextThroughScrolling(t *testing.T) {
	const rows = 5
	term := newMarkTerm(t, 40, rows)

	term.Write([]byte(osc133("A") + "MARKED\r\n"))
	// Push it well up into scrollback.
	term.Write([]byte(strings.Repeat("filler\r\n", 20)))

	marks := term.Marks()
	if len(marks) == 0 {
		t.Fatal("the mark was lost")
	}
	row := marks[0].Line

	term.Lock()
	hl := len(term.history)
	cols := term.cols
	var sb strings.Builder
	for x := 0; x < cols; x++ {
		var g Glyph
		if row < hl {
			g = term.history[row][x]
		} else {
			g = term.lines[row-hl][x]
		}
		sb.WriteRune(g.Char)
	}
	term.Unlock()

	if !strings.Contains(sb.String(), "MARKED") {
		t.Fatalf("mark points at %q, want the line it was recorded on", strings.TrimRight(sb.String(), " \x00"))
	}
}

// Marks whose lines have fallen out of a capped scrollback must be
// dropped rather than pointing somewhere arbitrary.
func TestMarksScrolledOutOfHistoryAreDropped(t *testing.T) {
	st := New(WithSize(40, 5), WithHistoryLimit(3)).(*terminal)
	st.Write([]byte(osc133("A") + "old\r\n"))
	st.Write([]byte(strings.Repeat("filler\r\n", 40)))

	for _, m := range st.Marks() {
		if m.Line < 0 {
			t.Fatalf("mark at negative row %d survived", m.Line)
		}
	}
}

// A full-screen program is not describing scrollback anyone will
// navigate, so a stray mark from one must not be recorded.
func TestMarksAreIgnoredOnTheAlternateScreen(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte("\x1b[?1049h")) // enter alt screen
	term.Write([]byte(osc133("A")))
	if got := len(term.Marks()); got != 0 {
		t.Fatalf("recorded %d marks on the alternate screen, want 0", got)
	}
}

// A resize slides lines off without archiving them, leaving nothing to
// relate a mark's number to where its text went — so they are forgotten
// rather than left pointing at the wrong line.
func TestMarksAreDroppedWhenAResizeDiscardsLines(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("A") + "one\r\n"))
	term.Write([]byte(strings.Repeat("x\r\n", 8)))
	term.Resize(40, 3) // forces a slide

	for _, m := range term.Marks() {
		if m.Line >= 3+term.HistoryLen() {
			t.Fatalf("stale mark at row %d after resize", m.Line)
		}
	}
}

// The B mark rides on PS1, so a shell re-prints it every time readline
// redraws the prompt — a pane resize is enough. Those redraws must not
// each count as another prompt: one history entry is built per B, so a
// duplicate made one command appear twice in the timeline, at the same
// timestamp with the same duration.
func TestARedrawnPromptDoesNotRecordASecondInput(t *testing.T) {
	term := newMarkTerm(t, 40, 10)

	// Prompt, then the same prompt re-printed over itself (carriage
	// return, no newline) the way a redraw does, then the command.
	term.Write([]byte(osc133("A") + "$ " + osc133("B")))
	term.Write([]byte("\r" + osc133("A") + "$ " + osc133("B")))
	term.Write([]byte("ls\r\n" + osc133("C") + "file\r\n" + osc133("D;0")))

	inputs := 0
	for _, m := range term.Marks() {
		if m.Kind == MarkInput {
			inputs++
		}
	}
	if inputs != 1 {
		t.Fatalf("a redrawn prompt recorded %d input marks, want 1", inputs)
	}
}

// Two prompts on two different lines are two prompts, which is the case
// the collapse above must not swallow.
func TestPromptsOnDifferentLinesStayDistinct(t *testing.T) {
	term := newMarkTerm(t, 40, 10)
	term.Write([]byte(osc133("A") + "$ " + osc133("B") + "ls\r\n" + osc133("C") + osc133("D;0")))
	term.Write([]byte(osc133("A") + "$ " + osc133("B") + "pwd\r\n" + osc133("C") + osc133("D;0")))

	inputs := 0
	for _, m := range term.Marks() {
		if m.Kind == MarkInput {
			inputs++
		}
	}
	if inputs != 2 {
		t.Fatalf("two commands recorded %d input marks, want 2", inputs)
	}
}
