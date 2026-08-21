package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/padovanl/termdock/internal/vt10x"
)

// What a terminal can do with OSC 133 marks, once the shell tells it
// where the prompts are (see internal/vt10x/marks.go):
//
//   - copy-mode's { and } jump between commands, instead of scrolling by
//     eye looking for the last prompt;
//   - Ctrl-B O copies the whole output of the last command — exactly
//     that, not "roughly this screenful";
//   - a pane whose last command failed says so in its title, with the
//     exit status and how long it took.
//
// None of it is possible without knowing which lines are prompt, which
// are the command, and which are its output, which is precisely what a
// terminal cannot work out for itself from a stream of characters. tmux
// has no notion of any of this.
//
// It degrades honestly: with no shell integration configured there are
// no marks, and each of these says so rather than guessing.

// paneMarks reads a pane's semantic marks, or nil if it has none.
func (c *Core) paneMarks(paneID int) []vt10x.Mark {
	p, ok := c.panes[paneID]
	if !ok {
		return nil
	}
	return p.Term().Marks()
}

// jumpToMark moves the copy-mode cursor to the nearest prompt strictly
// before (dir<0) or after (dir>0) where it is now — copy-mode's { and }.
func (c *Core) jumpToMark(dir int) {
	marks := c.paneMarks(c.copy.paneID)
	if len(marks) == 0 {
		c.statusMsg = noMarksHint
		return
	}
	_, rows, total, ok := c.copyPaneDims()
	if !ok {
		return
	}

	best := -1
	for _, m := range marks {
		if m.Kind != vt10x.MarkPrompt {
			continue
		}
		switch {
		case dir < 0 && m.Line < c.copy.curY:
			if m.Line > best { // the closest one above
				best = m.Line
			}
		case dir > 0 && m.Line > c.copy.curY:
			if best < 0 || m.Line < best { // the closest one below
				best = m.Line
			}
		}
	}
	if best < 0 {
		if dir < 0 {
			c.statusMsg = "no earlier command"
		} else {
			c.statusMsg = "no later command"
		}
		return
	}

	c.copy.curY = clampi(best, 0, maxi(0, total-1))
	c.copy.curX = 0
	// Put the prompt near the top rather than wherever it lands, so the
	// command's output is what fills the screen — the thing you jumped
	// back to read.
	c.copy.top = c.copy.curY
	c.clampTop(rows, total)
	c.statusMsg = ""
}

// noMarksHint is what every semantic command says when the shell hasn't
// been told to emit marks. It names the fix rather than just reporting
// the absence, since "nothing happened" with no explanation is how a
// feature gets written off as broken.
const noMarksHint = "no command marks in this pane — run `termdock shell-init` for the shell snippet"

// commandOutputAt returns the text printed by the command whose block
// contains row, and a label for it. A "block" runs from one prompt to
// the next, so this answers "the command I am looking at" rather than
// always the newest one — which matters directly: jumping back five
// commands with { and then copying is the whole point, and copying the
// newest one there would be silently wrong.
//
// The bounds are the marks themselves: the C that started the output and
// the D that ended it, or the bottom of the buffer when the command is
// still running, so asking mid-build gives what it has printed so far.
func (c *Core) commandOutputAt(paneID, row int) (text, label string, ok bool) {
	marks := c.paneMarks(paneID)
	if len(marks) == 0 {
		return "", "", false
	}

	// The block is the last prompt at or above row, up to the next one.
	blockStart, blockEnd := 0, len(marks)
	for i, m := range marks {
		if m.Kind != vt10x.MarkPrompt {
			continue
		}
		if m.Line <= row {
			blockStart = i
		} else {
			blockEnd = i
			break
		}
	}

	start, end := -1, -1
	for _, m := range marks[blockStart:blockEnd] {
		switch m.Kind {
		case vt10x.MarkOutput:
			if start < 0 {
				start = m.Line
			}
		case vt10x.MarkDone:
			end = m.Line
		}
	}
	if start < 0 {
		return "", "", false
	}

	p, found := c.panes[paneID]
	if !found {
		return "", "", false
	}
	t := p.Term()
	t.Lock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	if end < 0 || end > hl+rows {
		end = hl + rows // still running: take everything printed so far
	}
	var b strings.Builder
	for y := start; y < end; y++ {
		var line strings.Builder
		for x := 0; x < cols; x++ {
			ch := cellAt(t, hl, y, x).Char
			if ch == 0 {
				ch = ' '
			}
			line.WriteRune(ch)
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	t.Unlock()

	out := strings.Trim(b.String(), "\n")
	if out == "" {
		return "", "", false
	}
	return out, fmt.Sprintf("%d lines", strings.Count(out, "\n")+1), true
}

// copyLastOutput is Ctrl-B O: put a command's whole output on the
// clipboard and in the paste registers, the same places a copy-mode yank
// goes.
//
// Which command depends on where you are looking. In copy-mode it is the
// one the cursor sits in, so walking back with { and then copying does
// what it plainly looks like it should; otherwise it is the most recent,
// which is what "the last command" means at a live prompt.
func (c *Core) copyLastOutput() Result {
	id := c.win().active.ID
	row := 1 << 30 // past the end: the newest block
	if c.copy.active && c.copy.paneID == id {
		row = c.copy.curY
	}
	text, label, ok := c.commandOutputAt(id, row)
	if !ok {
		if len(c.paneMarks(c.win().active.ID)) == 0 {
			c.statusMsg = noMarksHint
		} else {
			c.statusMsg = "no command output to copy yet"
		}
		return Result{}
	}
	c.pushRegister(text)
	c.statusMsg = "copied the last command's output (" + label + ")"
	return Result{Clipboard: text, HasClipboard: true}
}

// lastCommandStatus summarizes how the pane's last finished command went,
// for its title: nothing at all when it succeeded (the common case, and
// a title is not the place for noise), the exit status when it failed,
// and how long it took when that was long enough to be worth knowing.
func (c *Core) lastCommandStatus(paneID int) string {
	marks := c.paneMarks(paneID)
	var done *vt10x.Mark
	var started time.Time
	for i := len(marks) - 1; i >= 0; i-- {
		if marks[i].Kind == vt10x.MarkDone {
			done = &marks[i]
			// The C before it is when the command actually began.
			for j := i - 1; j >= 0; j-- {
				if marks[j].Kind == vt10x.MarkOutput {
					started = marks[j].At
					break
				}
			}
			break
		}
	}
	if done == nil {
		return ""
	}

	var parts []string
	if done.Exit > 0 {
		parts = append(parts, fmt.Sprintf("✗%d", done.Exit))
	}
	if !started.IsZero() {
		if d := done.At.Sub(started); d >= slowCommand {
			parts = append(parts, d.Round(time.Second).String())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, " ") + "]"
}

// slowCommand is how long a command has to run before its duration is
// worth putting in the title. Below this it is noise: every `ls` would
// carry a "0s".
const slowCommand = 3 * time.Second
