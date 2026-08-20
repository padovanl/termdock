package core

import (
	"strings"
	"testing"

	"termdock/internal/layout"
)

// waitForPaneText polls paneID's own terminal buffer for text — the
// same idea as writeAndWaitEcho (search_test.go), but for commands that
// already wrote the input themselves (via runCommand's send-keys), so
// there's nothing left to write here, only to wait for.
func waitForPaneText(t *testing.T, c *Core, paneID int, text string) {
	t.Helper()
	ok := waitFor(t, func() bool {
		c.mu.Lock()
		p, ok := c.panes[paneID]
		c.mu.Unlock()
		if !ok {
			return false
		}
		term := p.Term()
		term.Lock()
		defer term.Unlock()
		cols, rows := term.Size()
		hl := term.HistoryLen()
		for y := 0; y < hl+rows; y++ {
			var sb strings.Builder
			for x := 0; x < cols; x++ {
				ch := cellAt(term, hl, y, x).Char
				if ch == 0 {
					ch = ' '
				}
				sb.WriteRune(ch)
			}
			if strings.Contains(sb.String(), text) {
				return true
			}
		}
		return false
	})
	if !ok {
		t.Fatalf("text %q never showed up in pane %d's buffer", text, paneID)
	}
}

func TestCommandPromptRoundTrip(t *testing.T) {
	c := newTestCore(t)

	c.mu.Lock()
	c.enterCommandPrompt()
	modeAfterEnter := c.mode
	promptAfterEnter := c.input.prompt
	c.input.buffer = []rune("new-window -n fromcmd")
	c.confirmInput()
	windowCount := len(c.windows)
	newName := c.windowDisplayName(c.win())
	modeAfter := c.mode
	c.mu.Unlock()

	if modeAfterEnter != ModeInput || promptAfterEnter != ":" {
		t.Fatalf("enterCommandPrompt: mode=%v prompt=%q, want ModeInput \":\"", modeAfterEnter, promptAfterEnter)
	}
	if windowCount != 2 {
		t.Fatalf("expected a new window from the command prompt, have %d", windowCount)
	}
	if newName != "fromcmd" {
		t.Fatalf("new window name = %q, want %q", newName, "fromcmd")
	}
	if modeAfter != ModeNormal {
		t.Fatalf("mode after confirming a command = %v, want ModeNormal", modeAfter)
	}
}

func TestRunCommandSplitWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	before := len(layout.Leaves(c.win().root))
	c.runCommand("split-window -s")
	after := len(layout.Leaves(c.win().root))
	c.mu.Unlock()
	if after != before+1 {
		t.Fatalf("split-window: had %d panes, now %d", before, after)
	}
}

func TestRunCommandSelectWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.newWindowOpts("second", "")
	c.runCommand("select-window 0")
	idx := c.activeWindow
	c.mu.Unlock()
	if idx != 0 {
		t.Fatalf("select-window 0: activeWindow = %d, want 0", idx)
	}
}

func TestRunCommandSelectWindowBadArgs(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.runCommand("select-window not-a-number")
	msg := c.statusMsg
	c.mu.Unlock()
	if !strings.HasPrefix(msg, "usage: select-window") {
		t.Fatalf("statusMsg = %q, want a usage message", msg)
	}
}

func TestRunCommandRenameWindow(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.runCommand("rename-window my new name")
	name := c.windowDisplayName(c.win())
	c.mu.Unlock()
	if name != "my new name" {
		t.Fatalf("rename-window: name = %q, want %q", name, "my new name")
	}
}

func TestRunCommandSendKeys(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	paneID := c.win().active.ID
	c.runCommand("send-keys echo unique-cmdprompt-marker Enter")
	c.mu.Unlock()
	waitForPaneText(t, c, paneID, "unique-cmdprompt-marker")
}

func TestRunCommandKillPane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	before := len(layout.Leaves(c.win().root))
	c.runCommand("kill-pane")
	after := len(layout.Leaves(c.win().root))
	c.mu.Unlock()
	if after != before-1 {
		t.Fatalf("kill-pane: had %d panes, now %d", before, after)
	}
}

func TestRunCommandBreakPane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	windowsBefore := len(c.windows)
	c.runCommand("break-pane")
	windowsAfter := len(c.windows)
	c.mu.Unlock()
	if windowsAfter != windowsBefore+1 {
		t.Fatalf("break-pane: had %d windows, now %d", windowsBefore, windowsAfter)
	}
}

func TestRunCommandRespawnPane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	oldID := c.win().active.ID
	c.runCommand("respawn-pane")
	newID := c.win().active.ID
	c.mu.Unlock()
	if newID == oldID {
		t.Fatal("respawn-pane should replace the active pane's ID")
	}
}

func TestRunCommandUnknown(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.runCommand("not-a-real-command")
	msg := c.statusMsg
	c.mu.Unlock()
	if !strings.HasPrefix(msg, "unknown command:") {
		t.Fatalf("statusMsg = %q, want an \"unknown command:\" prefix", msg)
	}
}

func TestRunCommandEmptyLineIsANoop(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.statusMsg = "sentinel"
	c.runCommand("   ")
	after := c.statusMsg
	c.mu.Unlock()
	if after != "sentinel" {
		t.Fatalf("blank command line should be a no-op, statusMsg became %q", after)
	}
}
