package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

// The point of a session-wide history is that it crosses panes, which
// each shell's own history fundamentally cannot.
func TestHistoryGathersCommandsFromEveryPane(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	writeSession(c, leaves[0].ID, cmdRun{"make build", []string{"ok"}, "0"})
	writeSession(c, leaves[1].ID, cmdRun{"kubectl get pods", []string{"none"}, "0"})

	var found []string
	for _, e := range c.collectHistory() {
		found = append(found, e.command)
	}
	joined := strings.Join(found, " | ")
	if !strings.Contains(joined, "make build") || !strings.Contains(joined, "kubectl get pods") {
		t.Fatalf("history = %q; want commands from both panes", joined)
	}
}

// It records how each command ended, which is the thing a shell history
// cannot tell you — "the one that worked" versus three attempts before.
func TestHistoryRecordsExitStatus(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id,
		cmdRun{"deploy staging", []string{"boom"}, "1"},
		cmdRun{"deploy prod", []string{"fine"}, "0"},
	)

	byCmd := map[string]int{}
	for _, e := range c.collectHistory() {
		byCmd[e.command] = e.exit
	}
	if got, ok := byCmd["deploy staging"]; !ok || got != 1 {
		t.Errorf("failed command recorded exit %d (present=%v), want 1", got, ok)
	}
	if got, ok := byCmd["deploy prod"]; !ok || got != 0 {
		t.Errorf("successful command recorded exit %d (present=%v), want 0", got, ok)
	}
}

// Newest first: what you want is nearly always something you just ran.
func TestHistoryIsNewestFirst(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id,
		cmdRun{"older-command", []string{"a"}, "0"},
		cmdRun{"newer-command", []string{"b"}, "0"},
	)

	items := c.collectHistory()
	if len(items) < 2 {
		t.Fatalf("collected %d commands, want at least 2", len(items))
	}
	if !strings.Contains(items[0].command, "newer") {
		t.Errorf("first entry is %q, want the most recent command", items[0].command)
	}
}

// A command run repeatedly must not fill the picker with copies of
// itself, or the list is useless exactly when it is longest.
func TestHistoryDeduplicatesRepeatedCommands(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	for i := 0; i < 5; i++ {
		writeSession(c, id, cmdRun{"go test ./...", []string{"ok"}, "0"})
	}
	writeSession(c, id, cmdRun{"git status", []string{"clean"}, "0"})

	c.enterHistoryPicker()
	seen := map[string]int{}
	for _, idx := range c.history.filtered {
		seen[c.history.items[idx].command]++
	}
	if seen["go test ./..."] != 1 {
		t.Errorf("the repeated command appears %d times, want once", seen["go test ./..."])
	}
	if seen["git status"] != 1 {
		t.Errorf("the other command appears %d times, want once", seen["git status"])
	}
}

// Confirming types the command in without running it: the list is full
// of things that already happened, some of which failed, and firing one
// straight off a fuzzy match is how the wrong directory gets deleted.
func TestHistoryConfirmTypesWithoutRunning(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id, cmdRun{"rm -rf build", []string{""}, "0"})
	c.enterHistoryPicker()
	c.confirmHistory()

	if !strings.Contains(c.statusMsg, "press enter") {
		t.Errorf("status %q should make clear it was typed, not run", c.statusMsg)
	}
	if c.mode != ModeNormal {
		t.Errorf("mode = %v, want back to normal", c.mode)
	}
}

// Without shell integration there is nothing to list, and it must name
// the fix rather than opening an empty box.
func TestHistoryWithoutMarksExplainsItself(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterHistoryPicker()
	if c.mode == ModeHistory {
		t.Error("opened an empty picker instead of explaining")
	}
	if !strings.Contains(c.statusMsg, "shell-init") {
		t.Errorf("status %q should point at the fix", c.statusMsg)
	}
}

// The command must come back without the prompt in front of it, which
// is only possible because the B mark records the column where the
// prompt stops — nothing else on the line distinguishes the two.
func TestHistoryStripsThePrompt(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	// A long, realistic prompt, so a naive "take the whole line" would
	// be obviously wrong.
	p := c.panes[id]
	p.Term().Write([]byte(
		osc133("A") + "luca@host:/very/long/path/somewhere$ " + osc133("B") +
			"git commit -m 'fix'\r\n" + osc133("C") + "done\r\n" + osc133("D;0")))

	items := c.collectHistory()
	if len(items) == 0 {
		t.Fatal("no commands collected")
	}
	got := items[0].command
	if got != "git commit -m 'fix'" {
		t.Fatalf("command = %q, want it without the prompt", got)
	}
}
