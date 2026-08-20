package core

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// writeAndWaitEcho writes text (plus a newline) to p's pty and waits
// until it shows up in the pane's terminal buffer, so tests have
// something deterministic to search for instead of racing the shell.
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
		t := p.Term()
		t.Lock()
		cols, rows := t.Size()
		hl := t.HistoryLen()
		found := false
		for y := 0; y < hl+rows && !found; y++ {
			var sb strings.Builder
			for x := 0; x < cols; x++ {
				g := cellAt(t, hl, y, x)
				ch := g.Char
				if ch == 0 {
					ch = ' '
				}
				sb.WriteRune(ch)
			}
			if strings.Contains(sb.String(), text) {
				found = true
			}
		}
		t.Unlock()
		if found {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("text %q never showed up in pane %d's buffer within the deadline", text, paneID)
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
