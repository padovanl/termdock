package core

import (
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/persist"
)

// renamePaneTo drives the same prompt Ctrl-B . opens.
func renamePaneTo(c *Core, name string) {
	c.startInput("rename-pane", "", "", ModeNormal)
	c.input.buffer = []rune(name)
	c.confirmInput()
}

// The complaint this answers: six panes all called "bash". A name the
// user chose has to win over the process name.
func TestRenamedPaneShowsItsNameInTheTitle(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	id := c.win().active.ID
	renamePaneTo(c, "api server")
	c.mu.Unlock()

	f := c.Frame()
	var title string
	for _, p := range f.Panes {
		if p.ID == id {
			title = p.Title
		}
	}
	if !strings.Contains(title, "api server") {
		t.Fatalf("pane title = %q, want the name it was given", title)
	}
	if strings.Contains(title, "bash") {
		t.Errorf("pane title = %q, still showing the process name", title)
	}
}

// Naming a pane is only worth doing if it also makes the pane findable,
// so the jump picker has to use it too.
func TestRenamedPaneIsFindableInThePicker(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doSplit(layout.Vertical)
	renamePaneTo(c, "database")

	c.enterPicker()
	var labels []string
	for _, it := range c.picker.items {
		labels = append(labels, it.label)
	}
	if !strings.Contains(strings.Join(labels, " | "), "database") {
		t.Fatalf("picker shows %v, want the renamed pane findable by its name", labels)
	}
}

// Clearing it puts the pane back to being named after what is running,
// which is the obvious meaning of confirming an empty prompt.
func TestEmptyNameClearsThePaneName(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.win().active.ID

	renamePaneTo(c, "temporary")
	if c.paneNames[id] != "temporary" {
		t.Fatal("the name was not set")
	}
	renamePaneTo(c, "")
	if _, still := c.paneNames[id]; still {
		t.Error("an empty name should clear it, not store an empty string")
	}
	if !strings.Contains(c.statusMsg, "cleared") {
		t.Errorf("status %q should say it was cleared", c.statusMsg)
	}
}

// Names are per pane, not per window: naming one must not rename its
// neighbour.
func TestPaneNamesAreIndependent(t *testing.T) {
	c := newTestCore(t)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.doSplit(layout.Vertical)
	leaves := layout.Leaves(c.win().root)
	c.setActive(leaves[0])
	renamePaneTo(c, "first")
	c.setActive(leaves[1])
	renamePaneTo(c, "second")

	if got := c.paneNames[leaves[0].ID]; got != "first" {
		t.Errorf("first pane is named %q", got)
	}
	if got := c.paneNames[leaves[1].ID]; got != "second" {
		t.Errorf("second pane is named %q", got)
	}
}

// A name you took the trouble to choose has to survive a crash, or the
// recovered session is back to six panes called "bash".
func TestPaneNameSurvivesRestore(t *testing.T) {
	name := "test-panename-" + t.Name()

	c1, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c1.mu.Lock()
	renamePaneTo(c1, "worker")
	c1.persistStateLocked()
	c1.mu.Unlock()
	closeAllPanes(c1)

	c2, err := New(name, 80, 24)
	if err != nil {
		t.Fatalf("New (restore): %v", err)
	}
	t.Cleanup(func() { closeAllPanes(c2); persist.Delete(name) })

	c2.mu.Lock()
	defer c2.mu.Unlock()
	if got := c2.paneNames[c2.win().active.ID]; got != "worker" {
		t.Fatalf("restored pane name = %q, want %q", got, "worker")
	}
}
