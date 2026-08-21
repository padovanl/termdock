package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/persist"
)

func TestSessionSurvivesRestartViaSnapshot(t *testing.T) {
	name := "test-restart-" + t.Name()

	c1, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1.mu.Lock()
	c1.doSplit(layout.Vertical)   // window 0: 2 panes side by side
	c1.doSplit(layout.Horizontal) // window 0: 3 panes (right side also split)
	c1.newWindowOpts("logs", "")  // window 1: 1 pane, explicitly named
	c1.mu.Unlock()

	// New(), called again with the same session name, should find the
	// snapshot newWindowOpts/doSplitIn already wrote (persistStateLocked
	// runs after every structural change — see persist.go) and rebuild
	// from it instead of starting fresh. Deliberately read it back
	// *before* touching c1's panes at all: closing them asynchronously
	// triggers their own detach/re-persist (handlePaneExit -> ... ->
	// persistStateLocked) on the pump goroutines, which would race with
	// — and could overwrite — the very snapshot this is trying to
	// verify. This is simulating a crash (the snapshot surviving because
	// nothing got a chance to clean up), not a graceful shutdown, so
	// there's nothing to wait for here in the first place.
	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (restore): %v", err)
	}
	defer closeAllPanes(c2)
	defer closeAllPanes(c1)

	if len(c2.windows) != 2 {
		t.Fatalf("expected 2 restored windows, got %d", len(c2.windows))
	}
	w0, w1 := c2.windows[0], c2.windows[1]
	if got := len(layout.Leaves(w0.root)); got != 3 {
		t.Errorf("window 0: expected 3 restored panes, got %d", got)
	}
	if w0.root.Split != layout.Vertical {
		t.Errorf("window 0 root split = %v, want Vertical", w0.root.Split)
	}
	if got := len(layout.Leaves(w1.root)); got != 1 {
		t.Errorf("window 1: expected 1 restored pane, got %d", got)
	}
	if !w1.renamed || w1.Name != "logs" {
		t.Errorf("window 1 = %+v, want renamed=true Name=logs", w1)
	}

	// Every restored leaf must actually have a live pane registered
	// (buildNodeFromSnapshot's whole point is to relaunch real shells,
	// not just reconstruct the tree shape).
	for _, w := range c2.windows {
		for _, l := range layout.Leaves(w.root) {
			if _, ok := c2.panes[l.ID]; !ok {
				t.Errorf("restored leaf id=%d has no live pane in c2.panes", l.ID)
			}
		}
	}
}

func TestGracefulShutdownDeletesSnapshot(t *testing.T) {
	name := "test-graceful-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := persist.Load(name); !ok {
		t.Fatal("expected a snapshot to exist right after New (newWindowOpts persists)")
	}

	c.Shutdown()

	if _, ok := persist.Load(name); ok {
		t.Fatal("Shutdown should delete the snapshot — a graceful end shouldn't resurrect itself")
	}
}

// TestQuitDeletesSnapshot is the regression test for the loudest bug of
// the lot: quitting a two-pane session and starting one with the same
// name again brought the two panes back instead of the fresh single pane
// a new session should have. Ctrl-B q runs requestQuit, which marks the
// session closed and *then* lets the server's Exited() watcher call
// Shutdown — and Shutdown used to return early on exactly that flag,
// skipping the snapshot delete that is its whole reason for existing.
func TestQuitDeletesSnapshot(t *testing.T) {
	name := "test-quit-deletes-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.confirmQuit()
	c.handleConfirmKey('y') // requestQuit: closes every pane, marks the session closed
	c.mu.Unlock()

	c.Shutdown() // what the server does once Exited() fires

	if _, ok := persist.Load(name); ok {
		t.Fatal("a confirmed quit must not leave a snapshot behind — the next session of this name would silently restore the layout just quit out of")
	}

	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (after quit): %v", err)
	}
	defer closeAllPanes(c2)
	if n := len(layout.Leaves(c2.windows[0].root)); n != 1 {
		t.Fatalf("a session started after quitting should open with 1 pane, got %d", n)
	}
}

// TestNoSnapshotWrittenAfterClose covers the other half of that fix.
// Closing panes makes their pump goroutines run handlePaneExit, which
// ends up back in persistStateLocked — so without a guard, a late exit
// can write the snapshot straight back out after Shutdown deleted it.
func TestNoSnapshotWrittenAfterClose(t *testing.T) {
	name := "test-late-persist-" + t.Name()

	c, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.mu.Lock()
	c.doSplit(layout.Vertical)
	c.mu.Unlock()

	c.Shutdown()

	c.mu.Lock()
	c.persistStateLocked() // stands in for a pump goroutine getting here late
	c.mu.Unlock()
	c.PersistState() // and for the server's periodic snapshot racing shutdown

	if _, ok := persist.Load(name); ok {
		t.Fatal("nothing should be able to write a snapshot once the session is closed")
	}
}

func TestFreshSessionWhenNoSnapshot(t *testing.T) {
	c := newTestCore(t)

	if len(c.windows) != 1 || len(layout.Leaves(c.windows[0].root)) != 1 {
		t.Fatalf("expected the normal single-window single-pane default, got %d windows", len(c.windows))
	}
}

// TestRestoreSurvivesACorruptSnapshot: the snapshot is read back every
// time a session of that name starts, so anything in it that breaks the
// restore breaks the session permanently — it can never start, so it can
// never replace the file that stops it starting. Every one of these has
// to end with a usable session.
func TestRestoreSurvivesACorruptSnapshot(t *testing.T) {
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "termdock")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}

	deep := func(n int) string {
		var b strings.Builder
		b.WriteString(`{"SessionName":"x","Windows":[{"Root":`)
		for i := 0; i < n; i++ {
			b.WriteString(`{"Split":1,"Ratio":0.5,"First":`)
		}
		b.WriteString(`{"Cwd":"/tmp"}`)
		for i := 0; i < n; i++ {
			b.WriteString(`,"Second":{"Cwd":"/tmp"}}`)
		}
		b.WriteString(`}]}`)
		return b.String()
	}

	cases := map[string]string{
		"truncated":      `{"SessionName":"x","Windows":[{"Root":`,
		"not-json":       `this is not json at all`,
		"null-root":      `{"SessionName":"x","Windows":[{"Root":null}]}`,
		"empty-windows":  `{"SessionName":"x","Windows":[]}`,
		"infinite-ratio": `{"SessionName":"x","Windows":[{"Root":{"Split":1,"Ratio":1e999,"First":{"Cwd":"/tmp"},"Second":{"Cwd":"/tmp"}}}]}`,
		"negative-ratio": `{"SessionName":"x","Windows":[{"Root":{"Split":1,"Ratio":-5,"First":{"Cwd":"/tmp"},"Second":{"Cwd":"/tmp"}}}]}`,
		"unusable-cwd":   `{"SessionName":"x","Windows":[{"Root":{"Cwd":"/no/such/dir/anywhere"}}]}`,
		"nested":         deep(3),
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			session := "corrupt-snapshot-" + name
			if err := os.WriteFile(filepath.Join(dir, session+".json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := New(session, 80, 24)
			if err != nil {
				t.Fatalf("New refused to start at all: %v", err)
			}
			defer closeAllPanes(c)

			if len(c.windows) == 0 {
				t.Fatal("no windows: the session would be unusable")
			}
			// Every pane must have real geometry — a zero-sized rect means
			// a shell running where nobody can see or reach it.
			f := c.Frame()
			if len(f.Panes) == 0 {
				t.Fatal("the frame has no panes to draw")
			}
			// Every pane in these fits comfortably in 80x24, so a
			// zero-sized rect here means the layout went wrong rather
			// than the terminal being too small — which layout.Compute
			// deliberately allows, squeezing panes rather than refusing
			// to draw (see TestRestoreOfMorePanesThanFitStaysReachable).
			for _, p := range f.Panes {
				if p.Rect.W <= 0 || p.Rect.H <= 0 {
					t.Errorf("pane %d restored with an empty rect %+v — invisible and unreachable", p.ID, p.Rect)
				}
			}
		})
	}
}

// TestRestoreOfMorePanesThanFitStaysReachable covers the other side of
// that: a layout saved on a wide monitor and restored on a narrow
// terminal genuinely can't give every pane room. They're squeezed to
// nothing rather than dropped — which is what tmux does too — so the
// thing that matters is that they all still exist and can be reached,
// since zooming one is how you get at it.
func TestRestoreOfMorePanesThanFitStaysReachable(t *testing.T) {
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "termdock")
	os.MkdirAll(dir, 0700)
	session := "too-many-panes"

	var b strings.Builder
	const depth = 12
	b.WriteString(`{"SessionName":"x","Windows":[{"Root":`)
	for i := 0; i < depth; i++ {
		b.WriteString(`{"Split":1,"Ratio":0.5,"First":`)
	}
	b.WriteString(`{"Cwd":"/tmp"}`)
	for i := 0; i < depth; i++ {
		b.WriteString(`,"Second":{"Cwd":"/tmp"}}`)
	}
	b.WriteString(`}]}`)
	if err := os.WriteFile(filepath.Join(dir, session+".json"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := New(session, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeAllPanes(c)

	c.mu.Lock()
	leaves := layout.Leaves(c.win().root)
	c.mu.Unlock()
	if len(leaves) != depth+1 {
		t.Fatalf("restored %d panes, want all %d", len(leaves), depth+1)
	}

	// Each one has a live pane behind it, and zooming reaches it.
	for _, l := range leaves {
		c.mu.Lock()
		_, live := c.panes[l.ID]
		c.mu.Unlock()
		if !live {
			t.Errorf("pane %d restored into the tree with no process behind it", l.ID)
		}
	}
	c.mu.Lock()
	c.toggleZoomOn(leaves[len(leaves)-1])
	c.mu.Unlock()
	f := c.Frame()
	if len(f.Panes) != 1 || f.Panes[0].Rect.W <= 0 || f.Panes[0].Rect.H <= 0 {
		t.Errorf("zooming a squeezed pane should give it the whole area, got %+v", f.Panes)
	}
}

// TestRestoreCoercesAnImpossibleSplit is the specific one that bit: an
// orientation that is neither vertical nor horizontal is not a leaf
// either, so layout.Compute has no case for it and leaves both children
// sized zero — two real shells drawing nothing, written back out to the
// snapshot so the window stays broken every restart.
func TestRestoreCoercesAnImpossibleSplit(t *testing.T) {
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "termdock")
	os.MkdirAll(dir, 0700)
	session := "impossible-split"
	body := `{"SessionName":"x","Windows":[{"Root":{"Split":99,"Ratio":0.5,"First":{"Cwd":"/tmp"},"Second":{"Cwd":"/tmp"}}}]}`
	if err := os.WriteFile(filepath.Join(dir, session+".json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := New(session, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer closeAllPanes(c)

	c.mu.Lock()
	split := c.win().root.Split
	c.mu.Unlock()
	if split != layout.Vertical && split != layout.Horizontal {
		t.Fatalf("root split = %v, want it coerced to a real orientation", split)
	}
	for _, p := range c.Frame().Panes {
		if p.Rect.W <= 0 || p.Rect.H <= 0 {
			t.Errorf("pane %d has an empty rect %+v", p.ID, p.Rect)
		}
	}
}
