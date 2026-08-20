package core

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// findAbsRow polls paneID's whole buffer (history + live grid), bottom
// row up, until it finds text somewhere and returns that absolute row.
// Bottom-up matters when text is also a substring of the shell's own
// echo of the command that produced it (which sits on an earlier row);
// polling (rather than a single scan) matters because writeAndWaitEcho
// only confirms the command was *typed*, not that it's finished
// *running* yet — there's a real gap between the two.
func findAbsRow(t *testing.T, c *Core, paneID int, text string) int {
	t.Helper()
	row := -1
	ok := waitFor(t, func() bool {
		c.mu.Lock()
		p, found := c.panes[paneID]
		c.mu.Unlock()
		if !found {
			return false
		}
		term := p.Term()
		term.Lock()
		defer term.Unlock()
		cols, rows := term.Size()
		hl := term.HistoryLen()
		for y := hl + rows - 1; y >= 0; y-- {
			var sb strings.Builder
			for x := 0; x < cols; x++ {
				ch := cellAt(term, hl, y, x).Char
				if ch == 0 {
					ch = ' '
				}
				sb.WriteRune(ch)
			}
			if strings.Contains(sb.String(), text) {
				row = y
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("text %q never showed up in pane %d's buffer", text, paneID)
	}
	return row
}

func TestCopyModeCharWiseSelectionYanksExactSpan(t *testing.T) {
	c := newTestCore(t)
	paneID := c.win().active.ID
	// The empty-string concatenation (abc''XYZ''def) is so the raw typed
	// command line never itself contains the contiguous substring
	// "abcXYZdef" — only printf's actual output does, once the shell
	// concatenates the adjacent quoted segments into one word — so
	// findAbsRow's bottom-up search can't accidentally match the input
	// echo instead of the real output line.
	writeAndWaitEcho(t, c, paneID, "printf '%s\\n' abc''XYZ''def")
	y := findAbsRow(t, c, paneID, "abcXYZdef")

	c.mu.Lock()
	c.enterCopyMode()
	c.copy.curX, c.copy.curY = 3, y // at 'X'
	c.handleCopyKey(tcell.KeyRune, 'v')
	c.copy.curX, c.copy.curY = 5, y // at 'Z'
	text, ok := c.yank()
	c.mu.Unlock()

	if !ok {
		t.Fatal("yank reported no selection")
	}
	if text != "XYZ" {
		t.Fatalf("char-wise yank = %q, want %q", text, "XYZ")
	}
}

// TestCopyModeLineWiseSelectionYanksFullLines checks that V ignores the
// column the selection started/ended at entirely — the whole of every
// line in range comes back, not just the span between two arbitrary
// columns the way v's character-wise selection would give.
func TestCopyModeLineWiseSelectionYanksFullLines(t *testing.T) {
	c := newTestCore(t)
	paneID := c.win().active.ID
	// Same empty-string-concatenation trick as the char-wise test above,
	// applied to all three words this time.
	writeAndWaitEcho(t, c, paneID, "printf '%s\\n' l''walpha l''wbeta l''wgamma")
	yAlpha := findAbsRow(t, c, paneID, "lwalpha")
	yGamma := findAbsRow(t, c, paneID, "lwgamma")
	if yGamma <= yAlpha {
		t.Fatalf("expected lwgamma's row (%d) after lwalpha's row (%d)", yGamma, yAlpha)
	}

	c.mu.Lock()
	c.enterCopyMode()
	c.copy.curX, c.copy.curY = 5, yAlpha // arbitrary mid-column; line-wise must ignore this
	c.handleCopyKey(tcell.KeyRune, 'V')
	lineWiseDuringSelection := c.copy.lineWise
	c.copy.curX, c.copy.curY = 1, yGamma // arbitrary mid-column
	text, ok := c.yank()
	c.mu.Unlock()

	if !lineWiseDuringSelection {
		t.Fatal("pressing V should start a line-wise selection")
	}
	if !ok {
		t.Fatal("yank reported no selection")
	}
	lines := strings.Split(text, "\n")
	if len(lines) != 3 {
		t.Fatalf("line-wise yank has %d lines, want 3: %q", len(lines), text)
	}
	for i, want := range []string{"lwalpha", "lwbeta", "lwgamma"} {
		if strings.TrimSpace(lines[i]) != want {
			t.Fatalf("line %d = %q, want %q", i, strings.TrimSpace(lines[i]), want)
		}
	}
}

// TestCopyModeVSwitchesModeKeepingAnchor: pressing 'v' then 'V' (or vice
// versa) while already selecting must switch character-wise/line-wise
// mode in place — keeping the same anchor and whatever the cursor's
// since moved to — not collapse the selection back down to a single
// point at the current cursor, which resetting the anchor here would do.
func TestCopyModeVSwitchesModeKeepingAnchor(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.enterCopyMode()
	c.handleCopyKey(tcell.KeyRune, 'v') // start char-wise, anchor = current cursor
	anchorX, anchorY := c.copy.anchorX, c.copy.anchorY
	c.moveCursorBy(0, 1) // move the cursor away from the anchor

	c.handleCopyKey(tcell.KeyRune, 'V') // switch to line-wise in place
	selecting, lineWise := c.copy.selecting, c.copy.lineWise
	gotAnchorX, gotAnchorY := c.copy.anchorX, c.copy.anchorY
	c.mu.Unlock()

	if !selecting || !lineWise {
		t.Fatalf("after 'V' while char-wise selecting: selecting=%v lineWise=%v, want true/true", selecting, lineWise)
	}
	if gotAnchorX != anchorX || gotAnchorY != anchorY {
		t.Fatalf("switching mode should keep the original anchor (%d,%d), got (%d,%d)", anchorX, anchorY, gotAnchorX, gotAnchorY)
	}

	c.mu.Lock()
	c.handleCopyKey(tcell.KeyRune, 'V') // same mode again -> exits selection
	stillSelecting := c.copy.selecting
	c.mu.Unlock()

	if stillSelecting {
		t.Fatal("pressing V again while already line-wise selecting should turn the selection off")
	}
}

func TestCopyModeSelectedRespectsLineWise(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.enterCopyMode()
	c.copy.curX, c.copy.curY = 10, 5
	c.handleCopyKey(tcell.KeyRune, 'V')
	c.copy.curX, c.copy.curY = 2, 7

	// Line-wise: every column on rows 5-7 counts as selected, including
	// columns outside [10,2] that a char-wise selection would exclude.
	insideRangeFarRightCol := c.selected(50, 6)
	outsideRowRange := c.selected(0, 4)
	c.mu.Unlock()

	if !insideRangeFarRightCol {
		t.Fatal("line-wise selection should include every column on an in-range row")
	}
	if outsideRowRange {
		t.Fatal("line-wise selection should still respect the row range")
	}
}
