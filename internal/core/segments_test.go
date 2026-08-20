package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"termdock/internal/pane"
)

func TestStatusSegmentsEmptyWhenDisabled(t *testing.T) {
	c := newTestCore(t)
	// c.statusSegments left nil — the default, off state.
	if got := c.statusSegmentsText(); got != "" {
		t.Fatalf("expected \"\" with no segments enabled, got %q", got)
	}
}

func TestReadBatteryGracefullyEmptyWithoutOne(t *testing.T) {
	// This sandbox/CI runner almost certainly has no battery; readBattery
	// must degrade to "" rather than erroring or panicking, the same way
	// pane.Cwd() does off Linux. If it DOES find a real one, just check
	// the format is sane instead of asserting "".
	got := readBattery()
	if got == "" {
		return
	}
	if !strings.HasPrefix(got, "🔋") && !strings.HasPrefix(got, "⚡") {
		t.Fatalf("unexpected battery segment format: %q", got)
	}
}

func TestGitBranchSegmentInARealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "f")
	run("commit", "-q", "-m", "initial")

	c := newTestCore(t)
	c.mu.Lock()
	p, err := pane.NewInDir(pane.NextID(), 80, 24, dir)
	if err != nil {
		c.mu.Unlock()
		t.Fatalf("NewInDir: %v", err)
	}
	c.panes[p.ID] = p
	// currentGitBranch only reads c.panes[c.win().active.ID] — repointing
	// the existing leaf's ID at the git-repo pane exercises exactly that
	// path without needing a real split just to get a second leaf.
	c.win().active.ID = p.ID
	c.mu.Unlock()
	defer p.Close()

	c.mu.Lock()
	branch := c.currentGitBranch()
	c.mu.Unlock()

	if branch == "" {
		t.Skip("pane.Cwd() returned \"\" — likely not on Linux (see cwd_other.go); git segment can't work without it")
	}
	if branch != " feature-x" {
		t.Fatalf("currentGitBranch() = %q, want %q", branch, " feature-x")
	}
}

func TestStatusSegmentsTextJoinsEnabledOnes(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.statusSegments = []string{"git", "battery"}
	// Stamp segCache "fresh" with fake values: statusSegmentsText only
	// recomputes when the cache is stale (see segmentCacheTTL), so
	// setting .at to now makes it use these as-is, exercising just the
	// join/formatting logic in isolation from currentGitBranch/readBattery
	// themselves.
	c.segCache = segmentCache{at: time.Now(), git: " main", batt: "🔋80%"}
	got := c.statusSegmentsText()
	c.mu.Unlock()

	if got != " main | 🔋80%" {
		t.Fatalf("statusSegmentsText() = %q, want %q", got, " main | 🔋80%")
	}
}
