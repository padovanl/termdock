package core

import (
	"math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// TestSessionSurvivesRandomInput drives a whole session the way a person
// with a keyboard and a mouse would, only faster and without aim: every
// mode entered and left in any order, panes split and killed underneath
// the modes that index into them, the terminal resized to sizes nothing
// fits in, and the mouse clicked at coordinates that are not on screen.
//
// The daemon holds every session in one process, so a panic anywhere in
// here takes down every pane of every session at once. That makes "never
// panics, whatever the order" a property worth hammering at rather than
// reasoning about: the modes each keep indices into state the others are
// busy rewriting.
func TestSessionSurvivesRandomInput(t *testing.T) {
	c := newTestCore(t)
	t.Cleanup(func() { closeAllPanes(c) })

	seed := int64(20260822)
	if v := os.Getenv("TERMDOCK_FUZZ_SEED"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			seed = n
		}
	}
	t.Logf("seed %d (set TERMDOCK_FUZZ_SEED to reproduce a failure)", seed)
	rng := rand.New(rand.NewSource(seed))

	keys := []tcell.Key{
		tcell.KeyRune, tcell.KeyEnter, tcell.KeyEsc, tcell.KeyUp, tcell.KeyDown,
		tcell.KeyLeft, tcell.KeyRight, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyTab,
		tcell.KeyBackspace2, tcell.KeyHome, tcell.KeyEnd, tcell.KeyCtrlB,
	}
	// Every prefix-key command, plus junk runes that are bound to nothing.
	runes := []rune("vs%\"hjklozxrc np;wWgS/P!Q: RZmOHTLA,.$&]=dCq?y[123450abcdefiu~@^")

	sizes := [][2]int{{80, 24}, {40, 12}, {200, 60}, {20, 6}, {5, 3}, {1, 1}, {120, 40}}

	for i := 0; i < 4000; i++ {
		switch rng.Intn(10) {
		case 0:
			// A resize, including sizes too small for anything to fit.
			s := sizes[rng.Intn(len(sizes))]
			c.Resize(s[0], s[1])
		case 1:
			// A click anywhere, on screen or off it, with or without a
			// button, sometimes with shift.
			mod := tcell.ModNone
			if rng.Intn(4) == 0 {
				mod = tcell.ModShift
			}
			buttons := []tcell.ButtonMask{
				tcell.Button1, tcell.Button2, tcell.Button3,
				tcell.WheelUp, tcell.WheelDown, tcell.ButtonNone,
			}[rng.Intn(6)]
			c.HandleClientMsg(proto.ClientMsg{
				Kind: "mouse", MouseX: rng.Intn(140) - 5, MouseY: rng.Intn(50) - 5,
				MouseButtons: int32(buttons), MouseMod: int32(mod),
			})
		case 2:
			// The prefix, so the next key is taken as a command.
			c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(tcell.KeyCtrlB)})
		default:
			k := keys[rng.Intn(len(keys))]
			r := rune(0)
			if k == tcell.KeyRune {
				r = runes[rng.Intn(len(runes))]
			}
			c.HandleClientMsg(proto.ClientMsg{Kind: "key", KeyCode: int32(k), KeyRune: r})
		}

		// Building a frame is what every attached client does many times a
		// second, and it reads all the state the loop above is mangling.
		f := c.Frame()
		if f.Cols < 0 || f.Rows < 0 {
			t.Fatalf("iteration %d: frame reports a negative size %dx%d", i, f.Cols, f.Rows)
		}
		for _, p := range f.Panes {
			if p.Rect.W < 0 || p.Rect.H < 0 {
				t.Fatalf("iteration %d: pane %d has a negative size %v", i, p.ID, p.Rect)
			}
		}
		if c.PaneCount() == 0 {
			break // the session killed its last pane; nothing left to drive
		}
	}
}
