package core

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/proto"
)

// Ctrl-B T draws the session as a timeline: every command that ran,
// where, when it started, how long it took and how it ended, oldest at
// the top — with a bar showing each one's span so overlapping work is
// visible as overlap.
//
// No multiplexer offers this, and the reason is not that nobody wanted
// it: a terminal receives an undifferentiated stream of characters and
// has no idea a command even happened, let alone when it started. The
// OSC 133 marks give termdock exactly those two timestamps per command
// (see internal/vt10x/marks.go), which is what makes the view possible
// at all rather than merely unimplemented.
//
// What it is for: "the build was still running when I started the
// migration, wasn't it" — the question you ask after something went
// wrong and the answer is spread across four panes' scrollback.

type timelineState struct {
	scroll int
}

// timelineSpan is one command's occupancy of the session's time.
type timelineSpan struct {
	command string
	pane    string
	start   time.Time
	end     time.Time // zero while it is still running
	exit    int
}

// collectTimeline gathers every command with a known start, oldest
// first. Commands still running are included with a zero end — they are
// exactly the ones you are usually asking about.
func (c *Core) collectTimeline() []timelineSpan {
	var out []timelineSpan
	for _, e := range c.collectHistory() {
		if e.at.IsZero() {
			continue
		}
		s := timelineSpan{command: e.command, pane: e.pane, exit: e.exit}
		if e.dur > 0 {
			s.end = e.at
			s.start = e.at.Add(-e.dur)
		} else {
			s.start = e.at
		}
		out = append(out, s)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].start.Before(out[b].start) })
	return out
}

func (c *Core) enterTimeline() {
	if len(c.collectTimeline()) == 0 {
		c.statusMsg = noMarksHint
		return
	}
	c.mode = ModeTimeline
	c.timeline = timelineState{}
}

func (c *Core) handleTimelineKey(key tcell.Key, r rune) {
	switch {
	case key == tcell.KeyEsc || r == 'q' || key == tcell.KeyEnter:
		c.mode = ModeNormal
		c.timeline = timelineState{}
	case key == tcell.KeyUp || r == 'k':
		c.timeline.scroll = maxi(0, c.timeline.scroll-1)
	case key == tcell.KeyDown || r == 'j':
		c.timeline.scroll++
	case key == tcell.KeyPgUp:
		c.timeline.scroll = maxi(0, c.timeline.scroll-10)
	case key == tcell.KeyPgDn:
		c.timeline.scroll += 10
	}
}

// timelineBarWidth is how many columns the span bars get. Fixed, so
// every row's bar is on the same scale and comparing two of them by eye
// means something.
const timelineBarWidth = 28

func (c *Core) timelineOverlay() *proto.Overlay {
	if c.mode != ModeTimeline {
		return nil
	}
	spans := c.collectTimeline()
	if len(spans) == 0 {
		return nil
	}

	// One scale for the whole view: from the first command's start to
	// the last activity, so a bar's position and length both mean
	// something across rows.
	first := spans[0].start
	last := first
	for _, s := range spans {
		end := s.end
		if end.IsZero() {
			end = time.Now()
		}
		if end.After(last) {
			last = end
		}
	}
	cmdW := 0
	for _, s := range spans {
		if l := len([]rune(s.command)); l > cmdW {
			cmdW = l
		}
	}
	if cmdW > 34 {
		cmdW = 34
	}

	items := make([]string, len(spans))
	// Where each row's bar sits, so the client can draw that run in the
	// theme's accent. The prefix is fixed-width by construction — a
	// timestamp, two spaces, the command padded to cmdW, two spaces — so
	// the offset is the same on every row and can be computed once.
	barAt := len("15:04:05") + 2 + cmdW + 2
	accent := make([][2]int, len(spans))
	for i, s := range spans {
		end := s.end
		running := end.IsZero()
		if running {
			end = time.Now()
		}
		items[i] = fmt.Sprintf("%s  %-*s  %s  %s",
			s.start.Format("15:04:05"),
			cmdW, truncate(s.command, cmdW),
			timelineBar(first, last, s.start, end),
			timelineNote(s, end.Sub(s.start), running))
		accent[i] = [2]int{barAt, timelineBarWidth}
	}

	return &proto.Overlay{
		Title:      "session timeline — oldest first, ↑↓/PgUp/PgDn scroll, esc close",
		Selectable: false,
		Items:      items,
		Selected:   c.timeline.scroll,
		Accent:     accent,
	}
}

// timelineBar draws one command's span on the scale shared by every row,
// so two bars can be compared by eye and overlapping work reads as
// overlap.
//
// A command too brief to fill a column still gets one. Rounding it away
// to nothing would draw an empty row, which reads as "no command here" —
// the opposite of true, and precisely the sort of quiet lie a
// visualisation must not tell.
func timelineBar(origin time.Time, total, start, end time.Time) string {
	span := total.Sub(origin)
	if span <= 0 {
		span = time.Second
	}
	col := func(t time.Time) int {
		f := float64(t.Sub(origin)) / float64(span)
		return clampi(int(f*float64(timelineBarWidth)), 0, timelineBarWidth-1)
	}
	from, to := col(start), col(end)
	if to < from {
		to = from
	}
	var b strings.Builder
	for i := 0; i < timelineBarWidth; i++ {
		switch {
		case i >= from && i <= to:
			b.WriteRune('█')
		default:
			b.WriteRune('·')
		}
	}
	return b.String()
}

// timelineNote is the trailing summary: how long, how it ended, where.
func timelineNote(s timelineSpan, d time.Duration, running bool) string {
	var parts []string
	switch {
	case running:
		parts = append(parts, "running")
	case d >= time.Second:
		parts = append(parts, d.Round(time.Second).String())
	default:
		parts = append(parts, d.Round(time.Millisecond).String())
	}
	if s.exit > 0 {
		parts = append(parts, fmt.Sprintf("✗%d", s.exit))
	}
	parts = append(parts, s.pane)
	return strings.Join(parts, "  ")
}
