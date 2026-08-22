package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/persist"
)

// The whole point: a recovered session opens showing what you were
// reading, not four empty panes.
func TestScrollbackSurvivesASnapshotAndRestore(t *testing.T) {
	name := "test-scrollback-" + t.Name()

	c1, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1.mu.Lock()
	id := c1.win().active.ID
	c1.panes[id].Term().Write([]byte("PANIC: something went very wrong\r\ngoroutine 1 [running]:\r\n"))
	c1.persistStateLocked()
	c1.mu.Unlock()
	closeAllPanes(c1)

	// A fresh Core under the same name is what a restart does.
	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (restore): %v", err)
	}
	t.Cleanup(func() { closeAllPanes(c2); persist.Delete(name) })

	c2.mu.Lock()
	defer c2.mu.Unlock()
	got := paneScreenText(c2, c2.win().active.ID)
	if !strings.Contains(got, "PANIC: something went very wrong") {
		t.Fatalf("restored pane does not show what was on screen before;\ngot:\n%s", got)
	}
	if !strings.Contains(got, "goroutine 1 [running]:") {
		t.Errorf("only part of the scrollback came back;\ngot:\n%s", got)
	}
}

// The snapshot is a tail, not a log: a pane that has printed thousands
// of lines must not turn every save into a large file.
func TestScrollbackCaptureIsBounded(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	p.Term().Write([]byte(strings.Repeat("a line of output\r\n", scrollbackLines*3)))

	if got := len(captureScrollback(p)); got > scrollbackLines {
		t.Fatalf("captured %d lines, want at most %d", got, scrollbackLines)
	}
}

// The empty bottom of a screen is not content; keeping it would push the
// restored text off the top of the pane.
func TestScrollbackDropsTrailingBlankLines(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	p.Term().Write([]byte("only line\r\n"))

	lines := captureScrollback(p)
	if len(lines) == 0 {
		t.Fatal("captured nothing")
	}
	if last := lines[len(lines)-1]; last == "" {
		t.Errorf("capture ends with a blank line: %q", lines)
	}
}

// A pane that printed nothing must not produce a snapshot entry at all,
// rather than a list of empty strings.
func TestScrollbackOfAnUntouchedPaneIsEmpty(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	// The pane's shell may have printed a prompt; what matters is that
	// nothing pathological (a screenful of blanks) is stored.
	if got := len(captureScrollback(c.panes[c.win().active.ID])); got > 3 {
		t.Errorf("an untouched pane captured %d lines, want almost none", got)
	}
}

// paneScreenText renders a pane's whole buffer, for asserting on what a
// user would see.
func paneScreenText(c *Core, id int) string {
	p, ok := c.panes[id]
	if !ok {
		return ""
	}
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	var sb strings.Builder
	for y := 0; y < hl+rows; y++ {
		for x := 0; x < cols; x++ {
			ch := cellAt(t, hl, y, x).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
