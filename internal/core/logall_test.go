package core

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
)

func logAllTo(c *Core, dir string) {
	c.startInput("log-window", "", "", ModeNormal)
	c.input.buffer = []rune(dir)
	c.confirmInput()
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// One file per pane, in the directory asked for — the whole point being
// a directory you can hand to someone.
func TestLogWindowWritesOneFilePerPane(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.doSplit(layout.Horizontal)
	want := len(layout.Leaves(c.win().root))
	logAllTo(c, dir)
	c.mu.Unlock()

	if got := filesIn(t, dir); len(got) != want {
		t.Fatalf("wrote %d files (%v), want one per pane (%d)", len(got), got, want)
	}
}

// The names have to be the ones you recognise, or a directory of logs is
// useless an hour later.
func TestLogFileNamesUseSessionWindowAndPaneNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune("backend")
	c.confirmInput() // window named "backend"
	c.doSplit(layout.Vertical)
	renamePaneTo(c, "api")
	logAllTo(c, dir)
	c.mu.Unlock()

	names := strings.Join(filesIn(t, dir), " ")
	if !strings.Contains(names, "backend") {
		t.Errorf("files %q should carry the window name", names)
	}
	if !strings.Contains(names, "api") {
		t.Errorf("files %q should carry the pane's name", names)
	}
}

// A window or pane called "feat/login" must not send its log into a
// directory that doesn't exist.
func TestLogFileNamesAreSafeForTheFilesystem(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	c.startInput("rename", "", "", ModeNormal)
	c.input.buffer = []rune("feat/login")
	c.confirmInput()
	logAllTo(c, dir)
	c.mu.Unlock()

	for _, n := range filesIn(t, dir) {
		if strings.ContainsAny(n, `/\`) {
			t.Fatalf("file name %q contains a path separator", n)
		}
	}
}

// Two panes may carry the same name — nothing stops you calling both
// "logs" — and having one file quietly receive both panes' output would
// be worse than an ugly suffix.
func TestLogFileNamesDoNotCollide(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	renamePaneTo(c, "same")
	c.setActive(leaves[1])
	renamePaneTo(c, "same")
	logAllTo(c, dir)
	c.mu.Unlock()

	if got := filesIn(t, dir); len(got) != 2 {
		t.Fatalf("two identically named panes produced %v, want two distinct files", got)
	}
}

// Pressing it again stops, rather than being a one-way switch.
func TestLogWindowTogglesOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doSplit(layout.Vertical)
	logAllTo(c, dir)

	if !c.windowIsLogging(c.win()) {
		t.Fatal("the window should be logging")
	}
	c.promptWindowLogging() // second press
	if c.windowIsLogging(c.win()) {
		t.Error("a second press should stop it")
	}
	if !strings.Contains(c.statusMsg, "stopped") {
		t.Errorf("status %q should say it stopped", c.statusMsg)
	}
}

// A pane already being logged by hand must not be restarted, which
// would truncate the file someone deliberately opened.
func TestLogWindowLeavesAnAlreadyLoggingPaneAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")

	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.doSplit(layout.Vertical)

	c.toggleLogging() // one pane, by hand, to its own path
	existing, _ := c.panes[c.win().active.ID].LogPath()

	logAllTo(c, dir)
	if now, _ := c.panes[c.win().active.ID].LogPath(); now != existing {
		t.Errorf("the hand-started log moved from %q to %q", existing, now)
	}
}

// "~/logs" is what gets typed far more often than an absolute path.
func TestLogDirectoryExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	got, err := expandPath("~/some/where")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "some/where"); got != want {
		t.Fatalf("expandPath(~/some/where) = %q, want %q", got, want)
	}
}
