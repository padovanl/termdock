package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/padovanl/termdock/internal/pane"
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

func TestReadMemGracefullyReadsARealSystem(t *testing.T) {
	got := readMem()
	if got == "" {
		t.Skip("no /proc/meminfo — not running on Linux")
	}
	if !strings.HasPrefix(got, "🧠") || !strings.HasSuffix(got, "%") {
		t.Fatalf("unexpected mem segment format: %q", got)
	}
}

func TestReadCPUSampleOnARealSystem(t *testing.T) {
	s, ok := readCPUSample()
	if !ok {
		t.Skip("no /proc/stat — not running on Linux")
	}
	if s.total == 0 {
		t.Fatal("expected a non-zero total jiffy count on a real system")
	}
	if s.idle > s.total {
		t.Fatalf("idle (%d) shouldn't exceed total (%d)", s.idle, s.total)
	}
}

func TestCPUPercentComputesFromDelta(t *testing.T) {
	prev := cpuSample{idle: 100, total: 1000}
	cur := cpuSample{idle: 150, total: 1200} // +200 total, +50 idle -> 150/200 busy = 75%
	if got := cpuPercent(prev, cur); got != "🖥️75%" {
		t.Fatalf("cpuPercent() = %q, want %q", got, "🖥️75%")
	}
}

func TestCPUPercentNoDeltaIsEmpty(t *testing.T) {
	s := cpuSample{idle: 50, total: 500}
	if got := cpuPercent(s, s); got != "" {
		t.Fatalf("cpuPercent with no elapsed time = %q, want \"\"", got)
	}
}

// TestRefreshSegmentsCPUNeedsTwoSamples: the very first refresh has
// nothing to diff against yet (see segmentCache.haveCPU), so it must
// report no CPU reading; the second one, some real time later, should.
func TestRefreshSegmentsCPUNeedsTwoSamples(t *testing.T) {
	if _, ok := readCPUSample(); !ok {
		t.Skip("no /proc/stat — not running on Linux")
	}
	c := newTestCore(t)

	c.mu.Lock()
	c.refreshSegments()
	firstCPU := c.segCache.cpu
	c.mu.Unlock()

	time.Sleep(50 * time.Millisecond)

	c.mu.Lock()
	c.refreshSegments()
	secondCPU := c.segCache.cpu
	c.mu.Unlock()

	if firstCPU != "" {
		t.Fatalf("the first-ever refresh has nothing to diff against, expected \"\", got %q", firstCPU)
	}
	if secondCPU == "" {
		t.Fatal("the second refresh should have a real delta to compute a CPU percentage from")
	}
}

func TestStatusSegmentsTextIncludesCPUAndMem(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	c.statusSegments = []string{"cpu", "mem"}
	c.segCache = segmentCache{at: time.Now(), cpu: "🖥️10%", mem: "🧠20%"}
	got := c.statusSegmentsText()
	c.mu.Unlock()

	if got != "🖥️10% | 🧠20%" {
		t.Fatalf("statusSegmentsText() = %q, want %q", got, "🖥️10% | 🧠20%")
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
