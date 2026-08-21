package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Optional status-bar segments (status-segments in termdock.conf) —
// tmux-powerline/Catppuccin-and-friends territory, minus needing a
// plugin manager: a small, fixed set of built-ins the user opts into by
// name, off by default so the status bar stays exactly as minimal as
// it's always been unless asked otherwise.

// segmentCacheTTL bounds how often segments are recomputed: "git" shells
// out to a real git process, so recomputing on every Frame() build
// (dozens of times a second while actively typing) would mean spawning
// a process that often. A couple of seconds of staleness on a status
// bar segment is imperceptible; a process spawn on every keystroke isn't.
const segmentCacheTTL = 2 * time.Second

// segmentSubprocessTimeout bounds the *worst case* added latency: this
// runs while c.mu is held (statusSegmentsText is called from statusLine,
// called from Frame), so an unbounded exec could stall the entire
// session, not just the status bar, if git ever hung.
const segmentSubprocessTimeout = 200 * time.Millisecond

type segmentCache struct {
	at   time.Time
	git  string
	batt string
	cpu  string
	mem  string

	// cpuSample is the raw /proc/stat reading the *cpu* segment was
	// computed from, carried forward across refreshes (NOT reset with
	// the rest of the cache) since a CPU percentage needs two samples
	// to compute a delta from — see readCPUSample/cpuPercent. The very
	// first refresh after "cpu" is enabled has nothing to diff against
	// yet, so it shows nothing until the second one, segmentCacheTTL
	// later.
	cpuSample cpuSample
	haveCPU   bool
}

// Each segment carries a short word as well as its icon — "🧠 mem 43%",
// not "🧠43%". The icon alone is a rebus: a brain could be memory or
// load, a monitor could be CPU or a display, and the two sit next to
// each other reading as a row of pictures. The word costs four columns
// and removes the guessing. A git branch names itself, so it doesn't
// need one.
//
// statusSegmentsText renders every enabled segment, refreshing the cache
// first if it's stale. Returns "" if no segments are enabled or none of
// the enabled ones have anything to show (e.g. "git" outside a repo).
func (c *Core) statusSegmentsText() string {
	if len(c.statusSegments) == 0 {
		return ""
	}
	if time.Since(c.segCache.at) > segmentCacheTTL {
		c.refreshSegments()
	}
	var parts []string
	for _, name := range c.statusSegments {
		switch name {
		case "git":
			if c.segCache.git != "" {
				parts = append(parts, c.segCache.git)
			}
		case "battery":
			if c.segCache.batt != "" {
				parts = append(parts, c.segCache.batt)
			}
		case "cpu":
			if c.segCache.cpu != "" {
				parts = append(parts, c.segCache.cpu)
			}
		case "mem":
			if c.segCache.mem != "" {
				parts = append(parts, c.segCache.mem)
			}
		}
	}
	return strings.Join(parts, " | ")
}

func (c *Core) refreshSegments() {
	sample, ok := readCPUSample()
	cpuText := ""
	if ok && c.segCache.haveCPU {
		cpuText = cpuPercent(c.segCache.cpuSample, sample)
	}
	c.segCache = segmentCache{
		at:        time.Now(),
		git:       c.currentGitBranch(),
		batt:      readBattery(),
		cpu:       cpuText,
		mem:       readMem(),
		cpuSample: sample,
		haveCPU:   ok,
	}
}

// currentGitBranch shells out to `git -C <active pane's cwd> rev-parse
// --abbrev-ref HEAD`, bounded by segmentSubprocessTimeout. "" outside a
// repo, with a detached HEAD, or wherever pane.Cwd() itself can't
// resolve a directory (non-Linux — see internal/pane/cwd_other.go).
func (c *Core) currentGitBranch() string {
	p, ok := c.panes[c.win().active.ID]
	if !ok {
		return ""
	}
	dir := p.Cwd()
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), segmentSubprocessTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return ""
	}
	return " " + branch
}

// readBattery reads the first battery it finds under
// /sys/class/power_supply (Linux only — the path simply won't exist
// elsewhere, so this degrades to "" the same way pane.Cwd() does rather
// than needing its own platform split).
func readBattery() string {
	const base = "/sys/class/power_supply/"
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "BAT") {
			continue
		}
		capBytes, err := os.ReadFile(base + e.Name() + "/capacity")
		if err != nil {
			continue
		}
		pct := strings.TrimSpace(string(capBytes))
		// "+" for charging: one column, unambiguous, and unlike a battery
		// emoji it cannot throw the status bar's alignment out (see
		// TestStatusSegmentsAreWidthPredictable).
		icon := "\uf240" // a battery
		if statusBytes, err := os.ReadFile(base + e.Name() + "/status"); err == nil &&
			strings.TrimSpace(string(statusBytes)) == "Charging" {
			icon = "\uf0e7" // a bolt
		}
		return icon + " bat " + pct + "%"
	}
	return ""
}

// cpuSample is one reading of /proc/stat's aggregate "cpu" line: total
// jiffies and idle jiffies since boot. A single sample says nothing on
// its own — CPU usage only means anything as a delta between two
// samples over an interval (see cpuPercent), which is exactly why it's
// carried forward in segmentCache instead of computed fresh each time.
type cpuSample struct {
	idle, total uint64
}

// readCPUSample reads and parses /proc/stat's first line (Linux only;
// ok is false wherever it doesn't exist, degrading like readBattery/
// pane.Cwd do rather than needing a platform split of their own).
func readCPUSample() (cpuSample, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}, false
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	// "cpu user nice system idle iowait irq softirq steal guest guest_nice"
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuSample{}, false
	}
	var s cpuSample
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		s.total += v
		if i == 3 { // idle is the 4th field
			s.idle = v
		}
	}
	return s, true
}

// cpuPercent computes overall CPU usage over the interval between two
// samples: the fraction of jiffies that weren't idle.
func cpuPercent(prev, cur cpuSample) string {
	dTotal := cur.total - prev.total
	dIdle := cur.idle - prev.idle
	if dTotal == 0 || dIdle > dTotal {
		return "" // clock hasn't advanced, or a counter wrapped — nothing sane to report
	}
	pct := 100 * (dTotal - dIdle) / dTotal
	return fmt.Sprintf("\uf2db cpu %d%%", pct)
}

// readMem reads /proc/meminfo for the fraction of memory in use —
// MemTotal minus MemAvailable (the "how much could a new process
// actually get" estimate the kernel itself computes, not just free
// mem), the same figure `free -h`'s "available" column is based on.
// Linux only, same degrade-to-"" convention as readBattery/readCPUSample.
func readMem() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return ""
	}
	var total, avail uint64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, _ = strconv.ParseUint(fields[1], 10, 64)
		case "MemAvailable:":
			avail, _ = strconv.ParseUint(fields[1], 10, 64)
		}
	}
	if total == 0 || avail > total {
		return ""
	}
	pct := 100 * (total - avail) / total
	return fmt.Sprintf("\uf1c0 mem %d%%", pct)
}
