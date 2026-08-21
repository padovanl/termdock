package core

import (
	"strings"
	"testing"
)

// osc133 is what a shell configured by `termdock shell-init` emits.
func osc133(body string) string { return "\x1b]133;" + body + "\x07" }

// writeSession plays a plausible shell transcript into a pane: a prompt,
// a command, its output, and the status it exited with.
func writeSession(c *Core, id int, cmds ...struct {
	cmd    string
	output []string
	exit   string
}) {
	p := c.panes[id]
	var b strings.Builder
	for _, s := range cmds {
		b.WriteString(osc133("A") + "$ " + osc133("B") + s.cmd + "\r\n")
		b.WriteString(osc133("C"))
		for _, line := range s.output {
			b.WriteString(line + "\r\n")
		}
		b.WriteString(osc133("D;" + s.exit))
	}
	p.Term().Write([]byte(b.String()))
}

type cmdRun = struct {
	cmd    string
	output []string
	exit   string
}

// Copying "the last command's output" has to mean exactly that: the
// lines that command printed, not the screenful around them.
func TestCopyLastOutputTakesExactlyThatCommandsOutput(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id,
		cmdRun{"echo one", []string{"FIRST-OUTPUT"}, "0"},
		cmdRun{"echo two", []string{"WANTED-A", "WANTED-B"}, "0"},
	)

	text, _, ok := c.lastCommandOutput(id)
	if !ok {
		t.Fatal("no output found despite marks being present")
	}
	if !strings.Contains(text, "WANTED-A") || !strings.Contains(text, "WANTED-B") {
		t.Errorf("output %q is missing the last command's lines", text)
	}
	if strings.Contains(text, "FIRST-OUTPUT") {
		t.Errorf("output %q leaked the previous command's lines", text)
	}
	if strings.Contains(text, "echo two") {
		t.Errorf("output %q includes the command line itself, not just its output", text)
	}
}

// With no shell integration there are no marks, and the feature must
// say so — naming the fix, rather than silently doing nothing.
func TestSemanticCommandsExplainWhenThereAreNoMarks(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.copyLastOutput()
	if !strings.Contains(c.statusMsg, "shell-init") {
		t.Errorf("status %q should point at the fix", c.statusMsg)
	}
}

// A failing command must be visible on the pane's title; a successful
// one must not add noise to it.
func TestLastCommandStatusShowsFailuresOnly(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id, cmdRun{"true", []string{"fine"}, "0"})
	if got := c.lastCommandStatus(id); got != "" {
		t.Errorf("a successful command tagged the title with %q, want nothing", got)
	}

	writeSession(c, id, cmdRun{"false", []string{"boom"}, "3"})
	if got := c.lastCommandStatus(id); !strings.Contains(got, "3") {
		t.Errorf("a failed command tagged the title with %q, want the exit status in it", got)
	}
}

// Jumping walks between prompts, so "go back to the previous command"
// lands on a prompt rather than an arbitrary line.
func TestJumpToMarkMovesBetweenPrompts(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id,
		cmdRun{"one", []string{"a"}, "0"},
		cmdRun{"two", []string{"b"}, "0"},
		cmdRun{"three", []string{"c"}, "0"},
	)
	c.enterCopyMode()

	// From the bottom, walking back must visit strictly decreasing rows.
	_, _, total, ok := c.copyPaneDims()
	if !ok {
		t.Fatal("no pane dimensions")
	}
	c.copy.curY = total - 1

	var visited []int
	for i := 0; i < 3; i++ {
		before := c.copy.curY
		c.jumpToMark(-1)
		if c.copy.curY == before {
			break
		}
		visited = append(visited, c.copy.curY)
	}
	if len(visited) < 2 {
		t.Fatalf("walked back to %v, want at least two distinct prompts", visited)
	}
	for i := 1; i < len(visited); i++ {
		if visited[i] >= visited[i-1] {
			t.Errorf("jumping back did not move upwards: %v", visited)
		}
	}

	// ...and forward again from the top.
	before := c.copy.curY
	c.jumpToMark(1)
	if c.copy.curY <= before {
		t.Errorf("jumping forward went from %d to %d, want a later row", before, c.copy.curY)
	}
}

// At the ends there is nowhere to go, and it must say so rather than
// moving somewhere arbitrary.
func TestJumpToMarkAtTheEndsSaysSo(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	writeSession(c, id, cmdRun{"only", []string{"x"}, "0"})
	c.enterCopyMode()
	c.copy.curY = 0

	c.jumpToMark(-1)
	if !strings.Contains(c.statusMsg, "no earlier") {
		t.Errorf("status %q should say there is nothing earlier", c.statusMsg)
	}
}

// A command still running has no D yet; asking then should give what it
// has printed so far rather than nothing at all.
func TestOutputOfAStillRunningCommandIsWhatItHasPrinted(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	p := c.panes[id]
	p.Term().Write([]byte(osc133("A") + "$ " + osc133("B") + "build\r\n" +
		osc133("C") + "PARTIAL-LINE\r\n"))

	text, _, ok := c.lastCommandOutput(id)
	if !ok || !strings.Contains(text, "PARTIAL-LINE") {
		t.Fatalf("output = %q, ok=%v; want what the running command has printed", text, ok)
	}
}
