package vt10x

import (
	"strconv"
	"strings"
	"time"
)

// OSC 133 is the de-facto "semantic prompt" protocol: the shell tells the
// terminal where each prompt starts, where the command the user typed
// ends, and where that command's output begins and ends, with its exit
// status. Terminals that understand it can do things a terminal
// otherwise cannot — jump between commands, select one command's output
// exactly, show whether it succeeded — because they finally know which
// of the bytes on screen are the prompt, which are the command, and
// which are its output. Without it a terminal only ever sees an
// undifferentiated stream of characters.
//
// The sequences, as emitted by a shell configured for it:
//
//	OSC 133 ; A ST          a fresh prompt starts here
//	OSC 133 ; B ST          the prompt ends; what follows is what you type
//	OSC 133 ; C ST          the command started running; output follows
//	OSC 133 ; D [; exit] ST the command finished, optionally with its status
//
// termdock owns its emulator, so it can record these itself rather than
// needing the terminal underneath to cooperate — which is the reason it
// can offer this at all where tmux cannot.

// MarkKind is which of the four OSC 133 markers a Mark records.
type MarkKind uint8

const (
	MarkPrompt MarkKind = iota // A: a prompt begins
	MarkInput                  // B: the prompt ended, typing begins
	MarkOutput                 // C: the command is running, output begins
	MarkDone                   // D: the command finished
)

func (k MarkKind) String() string {
	switch k {
	case MarkPrompt:
		return "prompt"
	case MarkInput:
		return "input"
	case MarkOutput:
		return "output"
	case MarkDone:
		return "done"
	}
	return "?"
}

// Mark is one recorded OSC 133 marker.
type Mark struct {
	Kind MarkKind
	// Line is which line it sits on. Callers of Marks get it in the same
	// absolute-row space as Cell and HistoryLen — 0 is the oldest line
	// still in scrollback — while internally it is a counter that never
	// shifts, so scrolling does not have to renumber anything.
	Line int
	// Exit is the command's exit status for MarkDone, or -1 when the
	// shell didn't report one.
	Exit int
	At   time.Time
}

// handleSemanticPrompt decodes the arguments of an OSC 133 sequence —
// everything after the "133" — and records the mark they name.
//
// Only the leading letter is acted on. Shells and terminals have grown a
// tail of optional key=value attributes after it (aid=, cl=, and others,
// varying by implementation); ignoring what we don't use means a shell
// snippet written for some other terminal still works here rather than
// being rejected wholesale over an attribute nobody needs.
func (t *State) handleSemanticPrompt(fields []string) {
	if len(fields) == 0 {
		return
	}
	switch fields[0] {
	case "A":
		t.addMark(MarkPrompt, -1)
	case "B":
		t.addMark(MarkInput, -1)
	case "C":
		t.addMark(MarkOutput, -1)
	case "D":
		// "D" alone means finished with no status reported, which is
		// different from finishing with status 0 and is recorded as such.
		exit := -1
		if len(fields) > 1 {
			if n, err := strconv.Atoi(strings.TrimSpace(fields[1])); err == nil {
				exit = n
			}
		}
		t.addMark(MarkDone, exit)
	}
}

// maxMarks caps how many are retained. A mark is tiny, but a session
// left running for weeks would otherwise accumulate without bound; well
// past the point where the lines they point at have scrolled away.
const maxMarks = 4096

// addMark records a marker at the cursor's current line. Ignored on the
// alternate screen: a full-screen program (vim, htop) that happened to
// emit one is not describing scrollback anybody will navigate.
func (t *State) addMark(kind MarkKind, exit int) {
	if t.mode&ModeAltScreen != 0 {
		return
	}
	t.marks = append(t.marks, Mark{
		Kind: kind,
		Line: t.scrolledOff + t.cur.Y,
		Exit: exit,
		At:   time.Now(),
	})
	if excess := len(t.marks) - maxMarks; excess > 0 {
		t.marks = t.marks[excess:]
	}
}

// dropMarks forgets every mark. Used where lines leave the screen
// *without* being archived to scrollback — a resize that slides content
// up, a scroll inside a custom region — after which no fixed offset
// relates a mark's line number to where its text actually ended up.
// Losing them is cheap and self-correcting: the next command records a
// fresh set. Silently misplacing them would not be.
func (t *State) dropMarks() {
	t.marks = nil
}

// Marks returns the recorded marks whose lines are still in the buffer,
// translated into the absolute-row space Cell and HistoryLen use.
func (t *State) Marks() []Mark {
	t.Lock()
	defer t.Unlock()
	return t.marksLocked()
}

func (t *State) marksLocked() []Mark {
	// The oldest line still retained is this far along the counter;
	// anything below it has been dropped from scrollback.
	oldest := t.scrolledOff - len(t.history)
	out := make([]Mark, 0, len(t.marks))
	for _, m := range t.marks {
		row := m.Line - oldest
		if row < 0 {
			continue
		}
		m.Line = row
		out = append(out, m)
	}
	return out
}
