package core

import (
	"strings"

	"github.com/padovanl/termdock/internal/pane"
)

// Crash recovery already brought back the layout and each pane's working
// directory (see persist.go), which is most of the value — but the panes
// came back empty. That is a strange half-restore to live with: the
// session looks right and every pane has forgotten what it was showing,
// so the error you were reading when the machine went down is gone.
//
// So the tail of each pane's screen is snapshotted too, and written back
// into the fresh shell on restore. What comes back is text, not a live
// program: the same honest limit the rest of persistence has. tmux needs
// tmux-resurrect for the layout alone, and even that does not bring the
// contents back.

// scrollbackLines is how many lines per pane are kept. Enough to still
// see the error and the command above it, small enough that a session of
// a dozen panes stays a snapshot rather than a log — these are written
// continuously, so the cost is paid over and over.
const scrollbackLines = 200

// captureScrollback reads the tail of a pane's buffer as plain text,
// oldest line first.
//
// Text, deliberately, not cells with their colours. A snapshot is read
// back by *writing it into a new terminal*, and replaying arbitrary
// styling into a shell that is already printing its own prompt is a good
// way to leave a pane in a colour it never chose. Legible content is the
// goal; exact reproduction is not achievable anyway, since the program
// that drew it is gone.
func captureScrollback(p *pane.Pane) []string {
	t := p.Term()
	t.Lock()
	defer t.Unlock()
	cols, rows := t.Size()
	hl := t.HistoryLen()
	total := hl + rows
	if total <= 0 || cols <= 0 {
		return nil
	}
	start := maxi(0, total-scrollbackLines)

	var out []string
	for y := start; y < total; y++ {
		var sb strings.Builder
		for x := 0; x < cols; x++ {
			ch := cellAt(t, hl, y, x).Char
			if ch == 0 {
				ch = ' '
			}
			sb.WriteRune(ch)
		}
		out = append(out, strings.TrimRight(sb.String(), " "))
	}
	// Trailing blank lines are the empty bottom of the screen, not
	// content; keeping them would push the restored text off the top.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// restoreScrollback writes saved lines back into a freshly created
// pane's terminal.
//
// Written to the *emulator*, not to the pty: sending it to the shell
// would have the shell try to run it. This puts the text on the pane's
// screen exactly as if it had been printed there, which is what it is.
//
// A trailing newline is deliberately not added: the shell is about to
// print its own prompt, and this way it lands on its own line under the
// restored text instead of one line further down.
func restoreScrollback(p *pane.Pane, lines []string) {
	if len(lines) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("\r\n")
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l)
	}
	b.WriteString("\r\n")
	p.Term().Write([]byte(b.String()))
}
