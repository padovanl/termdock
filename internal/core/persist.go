package core

import (
	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/persist"
)

// persistStateLocked best-effort snapshots the session's current
// window/pane layout and every pane's working directory to disk (see
// internal/persist). Called after structural changes — split, close,
// rename, move, a divider drag's release — never on every frame, so it
// costs nothing while just typing; server.go also calls the exported
// PersistState periodically to catch cwd drift from plain `cd` commands,
// which aren't a layout change and so don't otherwise trigger a save.
// Assumes c.mu is already held, same convention as relayoutLocked.
func (c *Core) persistStateLocked() {
	if c.closed {
		// The session is on its way out and Shutdown has deleted (or is
		// about to delete) its snapshot. Closing panes makes their pump
		// goroutines run handlePaneExit, which lands right back here — so
		// without this a late exit could write the snapshot straight back
		// out after the delete, resurrecting exactly the layout the user
		// just quit out of.
		return
	}
	snap := persist.Snapshot{SessionName: c.SessionName}
	for _, w := range c.windows {
		snap.Windows = append(snap.Windows, persist.Window{
			Name:      w.Name,
			Renamed:   w.renamed,
			SyncPanes: w.syncPanes,
			Root:      c.snapshotNode(w.root),
		})
	}
	persist.Save(c.SessionName, snap)
}

// PersistState is persistStateLocked's self-locking form, for periodic
// snapshots driven from outside core.
func (c *Core) PersistState() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.persistStateLocked()
}

func (c *Core) snapshotNode(n *layout.Node) persist.Node {
	if n.IsLeaf() {
		cwd := ""
		if p, ok := c.panes[n.ID]; ok {
			cwd = p.Cwd()
		}
		return persist.Node{Cwd: cwd}
	}
	first := c.snapshotNode(n.First)
	second := c.snapshotNode(n.Second)
	return persist.Node{
		Split:  int(n.Split),
		Ratio:  n.Ratio,
		First:  &first,
		Second: &second,
	}
}

// restoreFromSnapshot rebuilds c's windows from a previously saved
// layout, launching a fresh shell in each pane's last known working
// directory rather than trying to recreate its actual process state,
// which nothing can resurrect after a crash. Returns false (leaving c
// untouched) if the snapshot is empty or every window in it failed to
// restore, so New can fall back to its normal single-pane default.
func (c *Core) restoreFromSnapshot(snap persist.Snapshot) bool {
	cols, rows := max(c.cols, 1), max(c.rows-c.statusRows(), 1)
	for _, ws := range snap.Windows {
		root, ok := c.buildNodeFromSnapshot(&ws.Root, cols, rows)
		if !ok {
			continue // every shell in this window failed to start; skip it, don't abort the whole restore
		}
		w := &Window{ID: c.nextWindowID, root: root, active: layout.FirstLeaf(root), syncPanes: ws.SyncPanes}
		if ws.Renamed {
			w.Name = ws.Name
			w.renamed = true
		}
		c.nextWindowID++
		c.windows = append(c.windows, w)
	}
	if len(c.windows) == 0 {
		return false
	}
	c.activeWindow = 0
	return true
}

func (c *Core) buildNodeFromSnapshot(ns *persist.Node, cols, rows int) (*layout.Node, bool) {
	if ns.First == nil || ns.Second == nil {
		id := pane.NextID()
		p, err := pane.NewInDir(id, cols, rows, ns.Cwd)
		if err != nil {
			return nil, false
		}
		c.panes[id] = p
		c.startPump(p)
		return layout.NewLeaf(id, p), true
	}
	first, ok := c.buildNodeFromSnapshot(ns.First, cols, rows)
	if !ok {
		return nil, false
	}
	second, ok := c.buildNodeFromSnapshot(ns.Second, cols, rows)
	if !ok {
		return nil, false
	}
	ratio := ns.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5 // guards against a corrupt/hand-edited snapshot producing a zero-width child
	}
	n := &layout.Node{
		Split:  layout.SplitType(ns.Split),
		Ratio:  ratio,
		First:  first,
		Second: second,
	}
	first.Parent = n
	second.Parent = n
	return n, true
}
