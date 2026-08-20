package core

import (
	"context"
	"os"
	"os/exec"
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
}

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
		}
	}
	return strings.Join(parts, " | ")
}

func (c *Core) refreshSegments() {
	c.segCache = segmentCache{
		at:   time.Now(),
		git:  c.currentGitBranch(),
		batt: readBattery(),
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
		icon := "🔋"
		if statusBytes, err := os.ReadFile(base + e.Name() + "/status"); err == nil &&
			strings.TrimSpace(string(statusBytes)) == "Charging" {
			icon = "⚡"
		}
		return icon + pct + "%"
	}
	return ""
}
