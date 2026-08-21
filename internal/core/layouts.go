package core

import (
	"fmt"

	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/pane"
	"github.com/padovanl/termdock/internal/persist"
)

// Saving and applying named layouts. See internal/persist/layouts.go for
// what is stored and why; this is the half that reads it out of a live
// session and builds one back.

// SaveLayout captures the session's current arrangement under a name.
func (c *Core) SaveLayout(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	l := persist.Layout{Name: name}
	for _, w := range c.windows {
		lw := persist.LayoutWindow{Root: c.layoutNode(w.root)}
		if w.renamed {
			lw.Name = w.Name
		}
		l.Windows = append(l.Windows, lw)
	}
	if len(l.Windows) == 0 {
		return fmt.Errorf("nothing to save: the session has no windows")
	}
	return persist.SaveLayout(l)
}

func (c *Core) layoutNode(n *layout.Node) persist.LayoutNode {
	if n.IsLeaf() {
		p := persist.LayoutPane{Name: c.paneNames[n.ID]}
		if live, ok := c.panes[n.ID]; ok {
			p.Cwd = live.Cwd()
		}
		return persist.LayoutNode{Pane: &p}
	}
	first := c.layoutNode(n.First)
	second := c.layoutNode(n.Second)
	return persist.LayoutNode{
		Split:  int(n.Split),
		Ratio:  n.Ratio,
		First:  &first,
		Second: &second,
	}
}

// ApplyLayout builds the saved arrangement as new windows in this
// session, and switches to the first of them.
//
// Added alongside what is already open rather than replacing it. A
// layout is a thing you reach for to *start* work, and having it close
// panes you had running — with no undo for a whole session's worth —
// would make it something you approach nervously. Closing the old
// windows afterwards is one keystroke each and entirely your decision.
func (c *Core) ApplyLayout(name string) error {
	l, err := persist.LoadLayout(name)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	firstNew := len(c.windows)
	for _, lw := range l.Windows {
		root, ok := c.buildLayoutNode(&lw.Root)
		if !ok {
			return fmt.Errorf("could not build layout %q: a pane failed to start", name)
		}
		c.nextWindowID++
		w := &Window{ID: c.nextWindowID, root: root, active: layout.FirstLeaf(root)}
		if lw.Name != "" {
			w.Name, w.renamed = lw.Name, true
		}
		c.windows = append(c.windows, w)
	}
	if len(c.windows) > firstNew {
		c.setActiveWindowIndex(firstNew)
	}
	c.relayoutLocked()
	c.persistStateLocked()
	c.statusMsg = fmt.Sprintf("applied layout %q (%d windows)", name, len(l.Windows))
	return nil
}

func (c *Core) buildLayoutNode(n *persist.LayoutNode) (*layout.Node, bool) {
	if n.First == nil || n.Second == nil {
		id := pane.NextID()
		var (
			p   *pane.Pane
			err error
		)
		cwd, cmd, name := "", "", ""
		if n.Pane != nil {
			cwd, cmd, name = n.Pane.Cwd, n.Pane.Command, n.Pane.Name
		}
		switch {
		case cmd != "":
			p, err = pane.NewWithCommand(id, 80, 24, cmd)
		default:
			p, err = pane.NewInDir(id, 80, 24, cwd)
		}
		if err != nil {
			return nil, false
		}
		c.panes[id] = p
		if name != "" {
			if c.paneNames == nil {
				c.paneNames = map[int]string{}
			}
			c.paneNames[id] = name
		}
		c.startPump(p)
		return layout.NewLeaf(id, p), true
	}
	first, ok := c.buildLayoutNode(n.First)
	if !ok {
		return nil, false
	}
	second, ok := c.buildLayoutNode(n.Second)
	if !ok {
		return nil, false
	}
	// The same guards restoring a snapshot uses, and for the same reason:
	// a layout file is editable by hand and shareable, so it is exactly
	// the kind of input that arrives with a zero ratio or a split type
	// that is neither vertical nor horizontal. layout.Compute has no case
	// for those and would leave both children at a zero-sized rect —
	// two real shells holding ptys, drawing nothing, reachable by
	// nothing. Falling back loses the arrangement, never the panes.
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	split := layout.SplitType(n.Split)
	if split != layout.Vertical && split != layout.Horizontal {
		split = layout.Vertical
	}
	node := &layout.Node{Split: split, Ratio: ratio, First: first, Second: second}
	first.Parent = node
	second.Parent = node
	return node, true
}
