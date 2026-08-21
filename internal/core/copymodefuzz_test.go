package core

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/layout"
)

// TestCopyModeSurvivesAMovingBuffer: copy-mode holds absolute row
// indices into scrollback-plus-grid, and those go stale underneath it
// two ways — the pane gets resized, and the scrollback passes its limit
// so lines fall off the front and every index above shifts down. This
// drives both while every operation that reads by index runs, with the
// cursor and anchor deliberately shoved to absurd values on the way.
//
// It earns its place by having caught a real one: an out-of-range read
// here used to panic the daemon, taking every pane in every session with
// it. Reverting the bounds check in vt10x.State.Cell makes this fail
// within a few hundred iterations.
func TestCopyModeSurvivesAMovingBuffer(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaf := c.win().active
	pane := c.panes[leaf.ID]
	c.mu.Unlock()

	term := pane.Term()
	rng := rand.New(rand.NewSource(7))

	keys := []struct {
		k tcell.Key
		r rune
	}{
		{tcell.KeyRune, 'v'}, {tcell.KeyRune, 'V'}, {tcell.KeyRune, 'y'},
		{tcell.KeyRune, 'h'}, {tcell.KeyRune, 'j'}, {tcell.KeyRune, 'k'}, {tcell.KeyRune, 'l'},
		{tcell.KeyRune, 'g'}, {tcell.KeyRune, 'G'}, {tcell.KeyRune, 'n'}, {tcell.KeyRune, 'N'},
		{tcell.KeyPgUp, 0}, {tcell.KeyPgDn, 0}, {tcell.KeyCtrlU, 0}, {tcell.KeyCtrlD, 0},
		{tcell.KeyHome, 0}, {tcell.KeyEnd, 0},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("PANIC: %v", r)
		}
	}()

	for i := 0; i < 4000; i++ {
		switch rng.Intn(8) {
		case 0:
			c.Resize(4+rng.Intn(100), 2+rng.Intn(40))
		case 1:
			// Enough output to push the scrollback past its limit, so
			// lines fall off the front and every absolute index shifts.
			term.Write([]byte(strings.Repeat("filler line of text\r\n", 200)))
		case 2:
			c.mu.Lock()
			if c.mode != ModeCopy {
				c.enterCopyMode()
			}
			c.copy.searchTerm = "line"
			c.mu.Unlock()
		case 3:
			c.mu.Lock()
			c.scrollView(rng.Intn(200) - 100)
			c.mu.Unlock()
		case 4:
			c.mu.Lock()
			// Deliberately drag the cursor/anchor to absurd places.
			c.copy.curY = rng.Intn(30000) - 100
			c.copy.anchorY = rng.Intn(30000) - 100
			c.copy.curX = rng.Intn(400) - 10
			c.copy.anchorX = rng.Intn(400) - 10
			c.copy.top = rng.Intn(30000) - 100
			c.copy.selecting = rng.Intn(2) == 0
			c.copy.lineWise = rng.Intn(2) == 0
			c.mu.Unlock()
		default:
			k := keys[rng.Intn(len(keys))]
			c.mu.Lock()
			if c.mode != ModeCopy {
				c.enterCopyMode()
			}
			c.handleCopyKey(k.k, k.r)
			c.mu.Unlock()
		}
		_ = c.Frame()
	}
}
