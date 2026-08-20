package core

import (
	"github.com/padovanl/termdock/internal/layout"
	"github.com/padovanl/termdock/internal/proto"

	"github.com/gdamore/tcell/v2"
)

// enterQuickJump shows a big digit over every pane in the active window
// — tmux's display-panes, bound to Ctrl-B Q (Ctrl-B q was already
// taken, by termdock's quit) — so jumping to a specific pane by number
// doesn't need cycling through them one at a time with Tab. Digits match
// each pane's own title-bar number (see Core.paneTitle), 1-9 only, the
// same practical cap tmux's own display-panes has.
func (c *Core) enterQuickJump() {
	w := c.win()
	if w.zoomed != nil {
		c.statusMsg = "exit zoom (prefix z) before quick-jump"
		return
	}
	if len(layout.Leaves(w.root)) < 2 {
		c.statusMsg = "only one pane in this window"
		return
	}
	c.mode = ModeQuickJump
}

// handleQuickJumpKey always leaves ModeQuickJump — matching tmux's
// display-panes, any key dismisses it — jumping first if r is a digit
// that names one of the currently tagged panes.
func (c *Core) handleQuickJumpKey(key tcell.Key, r rune) {
	c.mode = ModeNormal
	c.statusMsg = ""
	if r < '1' || r > '9' {
		return
	}
	target := int(r - '0')
	for i, l := range layout.Leaves(c.win().root) {
		if i+1 == target {
			c.setActive(l)
			return
		}
	}
}

// quickJumpFrame builds the client-facing tag list, or nil when
// quick-jump isn't active — up to 9 tags, one per pane, in the same
// left-to-right/top-to-bottom order (and matching number) as each
// pane's own title bar.
func (c *Core) quickJumpFrame() []proto.QuickJumpTag {
	if c.mode != ModeQuickJump {
		return nil
	}
	leaves := layout.Leaves(c.win().root)
	var tags []proto.QuickJumpTag
	for i, l := range leaves {
		if i >= 9 {
			break
		}
		tags = append(tags, proto.QuickJumpTag{Rect: proto.Rect(l.Rect), Digit: rune('1' + i)})
	}
	return tags
}
