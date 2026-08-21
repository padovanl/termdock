package core

import (
	"strings"
	"testing"
	"time"
)

// runCmd plays one command through a pane with real elapsed time between
// its start and end marks, so the durations the timeline reports are
// measured rather than asserted into existence.
func runCmd(c *Core, id int, cmd string, took time.Duration, exit string) {
	p := c.panes[id]
	p.Term().Write([]byte(osc133("A") + "$ " + osc133("B") + cmd + "\r\n" + osc133("C")))
	time.Sleep(took)
	p.Term().Write([]byte("output\r\n" + osc133("D;"+exit)))
}

// Oldest first: a timeline read top to bottom should be the order things
// happened, which is the opposite of the history picker's ordering and
// deliberately so.
func TestTimelineIsOldestFirst(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	runCmd(c, id, "first-command", 10*time.Millisecond, "0")
	runCmd(c, id, "second-command", 10*time.Millisecond, "0")

	spans := c.collectTimeline()
	if len(spans) < 2 {
		t.Fatalf("collected %d spans, want 2", len(spans))
	}
	if !strings.Contains(spans[0].command, "first") {
		t.Errorf("first row is %q, want the earliest command", spans[0].command)
	}
	if !spans[0].start.Before(spans[1].start) {
		t.Error("rows are not in chronological order")
	}
}

// A command that is still going is the one you are usually asking
// about, so it has to appear rather than being skipped for lacking an
// end.
func TestTimelineIncludesAStillRunningCommand(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	runCmd(c, id, "finished", 5*time.Millisecond, "0")
	c.panes[id].Term().Write([]byte(osc133("A") + "$ " + osc133("B") + "tail -f log\r\n" + osc133("C")))

	c.enterTimeline()
	ov := c.timelineOverlay()
	if ov == nil {
		t.Fatal("no timeline")
	}
	joined := strings.Join(ov.Items, "\n")
	if !strings.Contains(joined, "tail -f log") {
		t.Fatalf("the running command is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "running") {
		t.Errorf("it should be marked as still going:\n%s", joined)
	}
}

// The durations must be real: a command that took twice as long has to
// get a visibly longer bar, or the picture is decorative rather than
// informative.
func TestTimelineBarLengthTracksDuration(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	runCmd(c, id, "quick", 10*time.Millisecond, "0")
	runCmd(c, id, "slow", 120*time.Millisecond, "0")

	spans := c.collectTimeline()
	if len(spans) < 2 {
		t.Fatalf("collected %d spans, want 2", len(spans))
	}
	var quick, slow timelineSpan
	for _, s := range spans {
		switch {
		case strings.Contains(s.command, "quick"):
			quick = s
		case strings.Contains(s.command, "slow"):
			slow = s
		}
	}
	qd := quick.end.Sub(quick.start)
	sd := slow.end.Sub(slow.start)
	if sd <= qd {
		t.Fatalf("the slow command measured %v, the quick one %v — durations are not real", sd, qd)
	}

	first, last := spans[0].start, slow.end
	qBar := strings.Count(timelineBar(first, last, quick.start, quick.end), "█")
	sBar := strings.Count(timelineBar(first, last, slow.start, slow.end), "█")
	if sBar <= qBar {
		t.Errorf("bars are %d (quick) and %d (slow) columns; the longer command should draw a longer bar", qBar, sBar)
	}
}

// A command too brief to fill a column still gets one: an empty row
// reads as "nothing happened here", which is the opposite of true.
func TestTimelineBarNeverRoundsACommandAway(t *testing.T) {
	origin := time.Now()
	last := origin.Add(time.Hour)
	instant := origin.Add(time.Second) // a blink, against an hour-long scale

	bar := timelineBar(origin, last, instant, instant)
	if strings.Count(bar, "█") < 1 {
		t.Fatalf("bar %q has no filled column — a brief command vanished", bar)
	}
}

// Without shell integration there is nothing to draw, and it must say
// so rather than opening an empty box.
func TestTimelineWithoutMarksExplainsItself(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enterTimeline()
	if c.mode == ModeTimeline {
		t.Error("opened an empty timeline instead of explaining")
	}
	if !strings.Contains(c.statusMsg, "shell-init") {
		t.Errorf("status %q should point at the fix", c.statusMsg)
	}
}
