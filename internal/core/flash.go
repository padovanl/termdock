package core

import (
	"fmt"
	"time"
)

// A flash is a message that takes over the status bar briefly and then
// gets out of the way.
//
// c.statusMsg is not enough on its own. Every modal screen puts its own
// instructions in the status bar for as long as it is open (see
// statusHint), so a message set from inside one is written and never
// seen: saving a setting to the config file said so, underneath "↑↓
// move, enter to type a value, S save to file, esc close", which won.
// Nothing appeared to happen, from the one screen where knowing that the
// file was written is the whole point.
//
// So a flash outranks the mode's own hint, and only for a few seconds:
// the hint is what you need for as long as the screen is open, and the
// confirmation is what you need immediately after acting.

// flashFor is how long a flash holds the status bar. Long enough to read
// a path in it without hurrying, short enough that the instructions it
// covers are not gone when you look back for them.
const flashFor = 4 * time.Second

// flash puts msg in the status bar for flashFor.
func (c *Core) flash(format string, args ...any) {
	c.flashMsg = fmt.Sprintf(format, args...)
	c.flashUntil = time.Now().Add(flashFor)
	// Set as well, not instead: outside a modal screen the status bar
	// shows statusMsg anyway, and leaving it behind afterwards is the
	// existing behaviour for every other message.
	c.statusMsg = c.flashMsg

	// A frame is only built when something asks for one. Without this the
	// message would sit there until the next keypress or the 15-second
	// clock tick, whichever came first, and "a few seconds" would be a
	// duration nothing honoured. markDirty is a non-blocking send, so
	// calling it from the timer's goroutine is safe and cannot stall it.
	time.AfterFunc(flashFor, c.markDirty)
}

// activeFlash is the message currently owed the status bar, or "" when
// none is live.
func (c *Core) activeFlash() string {
	if c.flashMsg == "" || time.Now().After(c.flashUntil) {
		return ""
	}
	return c.flashMsg
}
