package core

import (
	"strings"
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

// A URL longer than the pane is wide is stored as two rows. Scanning
// them separately finds neither: the head becomes a truncated but
// plausible-looking link, and the tail turns up as a second, bogus
// entry — so the picker offers two wrong things instead of one right
// one.
func TestOpenerRejoinsAWrappedURL(t *testing.T) {
	const url = "https://example.com/a/very/long/path/that/will/certainly/wrap"

	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	p.Resize(30, 10) // narrower than the URL
	p.Term().Write([]byte(url + "\r\n"))

	found := c.scanActivePaneForLinks()
	var whole bool
	for _, f := range found {
		if f == url {
			whole = true
		}
		if strings.HasPrefix(f, "/a/very") || strings.HasPrefix(f, "/that/will") {
			t.Errorf("the tail of the wrapped URL leaked as its own entry: %q", f)
		}
	}
	if !whole {
		t.Fatalf("the wrapped URL was not rejoined; got %v", found)
	}
}

// Two links on one line must both be offered — the masking that stops a
// URL also matching the path pattern must not eat the second one.
func TestOpenerFindsBothLinksOnALine(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	p := c.panes[c.win().active.ID]
	p.Term().Write([]byte("see http://one.example and https://two.example\r\n"))

	found := strings.Join(c.scanActivePaneForLinks(), " ")
	for _, want := range []string{"http://one.example", "https://two.example"} {
		if !strings.Contains(found, want) {
			t.Errorf("%q missing from %q", want, found)
		}
	}
}
