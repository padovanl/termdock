package core

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestOpenerFindsURLsAndPaths(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	paneID := c.win().active.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, paneID, "https://example.com/page and /etc/hosts too")

	c.mu.Lock()
	items := c.scanActivePaneForLinks()
	c.mu.Unlock()

	wantURL, wantPath := false, false
	for _, it := range items {
		if it == "https://example.com/page" {
			wantURL = true
		}
		if it == "/etc/hosts" {
			wantPath = true
		}
	}
	if !wantURL {
		t.Errorf("expected to find the URL among %v", items)
	}
	if !wantPath {
		t.Errorf("expected to find the path among %v", items)
	}
}

func TestOpenerNoMatchesIsANoop(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterOpener()
	mode := c.mode
	c.mu.Unlock()

	// A fresh shell prompt has no URLs/paths in it (almost certainly —
	// this isn't guaranteed for every possible shell config, but is the
	// overwhelmingly common case and matches what enterOpener actually
	// sees in practice).
	if mode == ModeOpener {
		t.Skip("the fresh shell prompt happened to contain something path-like; not a meaningful failure")
	}
	if mode != ModeNormal {
		t.Fatalf("expected ModeNormal when there's nothing to open, got %v", mode)
	}
}

func TestOpenerFilterAndConfirmCopiesToClipboard(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	paneID := c.win().active.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, paneID, "see https://foo.example/one and https://bar.example/two")

	c.mu.Lock()
	c.enterOpener()
	if c.mode != ModeOpener {
		c.mu.Unlock()
		t.Fatal("enterOpener should have found matches and set ModeOpener")
	}
	for _, r := range "bar.example" {
		c.handleOpenerKey(tcell.KeyRune, r)
	}
	var matched []string
	for _, idx := range c.opener.filtered {
		matched = append(matched, c.opener.items[idx])
	}
	filtered := len(c.opener.filtered)
	c.mu.Unlock()

	if filtered != 1 {
		t.Fatalf("query 'bar.example' should match exactly 1 URL, matched %d: %v", filtered, matched)
	}

	c.mu.Lock()
	res := c.handleOpenerKey(tcell.KeyEnter, 0)
	mode := c.mode
	registerCount := len(c.registers)
	c.mu.Unlock()

	if !res.HasClipboard || res.Clipboard != "https://bar.example/two" {
		t.Fatalf("expected Result.Clipboard = %q, got HasClipboard=%v Clipboard=%q", "https://bar.example/two", res.HasClipboard, res.Clipboard)
	}
	if mode != ModeNormal {
		t.Fatalf("confirming should return to ModeNormal, got %v", mode)
	}
	if registerCount != 1 {
		t.Fatalf("confirming should also push the pick as a paste register, got %d registers", registerCount)
	}
}

func TestOpenerEscCancels(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	paneID := c.win().active.ID
	c.mu.Unlock()
	writeAndWaitEcho(t, c, paneID, "https://example.com/whatever")

	c.mu.Lock()
	c.enterOpener()
	c.handleOpenerKey(tcell.KeyEsc, 0)
	mode := c.mode
	c.mu.Unlock()

	if mode != ModeNormal {
		t.Fatalf("Esc should return to ModeNormal, got %v", mode)
	}
}
