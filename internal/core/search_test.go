package core

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/pane"
)

// writeAndWaitEcho writes text (plus a newline) to p's pty and waits
// until it shows up in the pane's terminal buffer, so tests have
// something deterministic to search for instead of racing the shell.
//
// It matches against the buffer with row boundaries removed, not row by
// row, because the shell's echo of the typed command is *wrapped* to the
// pane width: with a long prompt (the test binary runs deep inside the
// repo, and $PWD is in the default bash prompt), "echo some-marker"
// lands on screen as "…/internal/core$ echo some-" + "marker" across two
// rows, and never appears contiguously on any single one. Row-by-row
// matching happened to work anyway only via the pty's own raw echo of
// the input line, which sits unwrapped on its own row — but that only
// shows up before bash's readline takes the terminal over, i.e. for the
// first command or two of a session, so a test whose 4th command was the
// one it waited on would time out.
func writeAndWaitEcho(t *testing.T, c *Core, paneID int, text string) {
	t.Helper()
	c.mu.Lock()
	p, ok := c.panes[paneID]
	c.mu.Unlock()
	if !ok {
		t.Fatalf("pane %d not found", paneID)
	}
	p.Write([]byte(text + "\r"))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(paneTextUnwrapped(p), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("text %q never showed up in pane %d's buffer within the deadline;\nbuffer was:\n%s",
		text, paneID, paneTextUnwrapped(p))
}

// paneTextUnwrapped renders a pane's whole buffer (history + live grid)
// as one string with each row's trailing blanks dropped and no separator
// between rows, so text the terminal wrapped across a row boundary reads
// back as the single logical line it was typed as. Only suitable for
// looking for a distinctive marker — with no separators, adjacent
// unrelated rows do run together.
func paneTextUnwrapped(p *pane.Pane) string {
	term := p.Term()
	term.Lock()
	defer term.Unlock()
	cols, rows := term.Size()
	hl := term.HistoryLen()
	var sb strings.Builder
	for y := 0; y < hl+rows; y++ {
		var row strings.Builder
		for x := 0; x < cols; x++ {
			ch := cellAt(term, hl, y, x).Char
			if ch == 0 {
				ch = ' '
			}
			row.WriteRune(ch)
		}
		sb.WriteString(strings.TrimRight(row.String(), " "))
	}
	return sb.String()
}

func TestGlobalSearchFindsTextAndJumpsToIt(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.newWindow() // window 1
	win1ID := c.win().root.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, win1ID, "findme-unique-marker-xyz")

	c.mu.Lock()
	c.selectWindowIndex(0) // move away, so confirming the search has to switch back
	c.enterGlobalSearch()
	if c.mode != ModeSearch {
		t.Fatalf("enterGlobalSearch should set ModeSearch, got %v", c.mode)
	}
	for _, r := range "findme-unique-marker" {
		c.handleSearchKey(tcell.KeyRune, r)
	}
	resultCount := len(c.search.results)
	c.mu.Unlock()

	if resultCount == 0 {
		t.Fatal("expected at least one match for the marker text")
	}

	c.mu.Lock()
	c.confirmSearch()
	mode := c.mode
	activeWindow := c.activeWindow
	copyPaneID := c.copy.paneID
	inCopyMode := c.mode == ModeCopy
	c.mu.Unlock()

	if mode != ModeCopy || !inCopyMode {
		t.Fatalf("confirming a search result should land in copy-mode, got mode=%v", mode)
	}
	if activeWindow != 1 {
		t.Fatalf("expected to jump to window 1 (where the marker was written), got window %d", activeWindow)
	}
	if copyPaneID != win1ID {
		t.Fatalf("expected copy-mode on pane %d, got %d", win1ID, copyPaneID)
	}
}

func TestGlobalSearchEmptyQueryHasNoResults(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterGlobalSearch()
	c.refilterSearch() // empty query
	n := len(c.search.results)
	c.mu.Unlock()

	if n != 0 {
		t.Fatalf("an empty query should return no results (not everything), got %d", n)
	}
}

func TestGlobalSearchRegexPattern(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	paneID := c.win().active.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, paneID, "echo error-42")

	c.mu.Lock()
	c.enterGlobalSearch()
	for _, r := range `err(or)?-\d+` {
		c.handleSearchKey(tcell.KeyRune, r)
	}
	n := len(c.search.results)
	c.mu.Unlock()

	if n == 0 {
		t.Fatal("regex pattern err(or)?-\\d+ should have matched the printed \"error-42\" line")
	}
}

// TestGlobalSearchInvalidRegexFallsBackToLiteralSubstring: "(broken" is
// not valid regex syntax on its own (an unbalanced group) — compileSearch
// must fall back to a literal, case-insensitive substring match instead
// of erroring out or silently matching nothing.
func TestGlobalSearchInvalidRegexFallsBackToLiteralSubstring(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	paneID := c.win().active.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, paneID, "echo 'marker-(broken-text'")

	c.mu.Lock()
	c.enterGlobalSearch()
	for _, r := range "(broken" {
		c.handleSearchKey(tcell.KeyRune, r)
	}
	n := len(c.search.results)
	c.mu.Unlock()

	if n == 0 {
		t.Fatal("an invalid regex query should fall back to a literal substring match instead of finding nothing")
	}
}

func TestGlobalSearchEscCancels(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterGlobalSearch()
	c.handleSearchKey(tcell.KeyEsc, 0)
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("Esc should return to ModeNormal, got %v", mode)
	}
}
