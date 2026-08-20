package core

import (
	"strconv"
	"strings"

	"github.com/padovanl/termdock/internal/layout"
)

// enterCommandPrompt opens a ":"-prefixed command line, bound to
// Ctrl-B : — tmux's own command-prompt binding, same key. It reuses the
// same one-line text-entry primitive rename/search already share (see
// input.go); confirmInput's "cmd" case below is what actually runs the
// typed line. Unlike the external CLI scripting surface (cli.go /
// internal/core/cli.go), which always needs an explicit -t
// SESSION[:WINDOW[.PANE]] target since it's invoked from outside any
// session, a command typed here always means "in the window I'm looking
// at right now" — there's nothing else it could sensibly mean.
func (c *Core) enterCommandPrompt() {
	c.startInput("cmd", ":", "", ModeNormal)
}

// runCommand parses and executes one command-prompt line. Assumes c.mu
// is already held: it's only ever called from confirmInput, itself
// called from handleInputKey under handleKey's lock — unlike the CLI*
// methods in cli.go, which lock for themselves since a scripted command
// arrives on its own connection, outside any attached client's input
// path.
func (c *Core) runCommand(line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	name, args := fields[0], fields[1:]
	switch name {
	case "new-window":
		wname := ""
		if len(args) >= 2 && args[0] == "-n" {
			wname, args = args[1], args[2:]
		}
		if _, err := c.newWindowOpts(wname, strings.Join(args, " ")); err != nil {
			c.statusMsg = err.Error()
		}

	case "split-window":
		axis := layout.Vertical
		if len(args) > 0 && (args[0] == "-v" || args[0] == "-s") {
			if args[0] == "-s" {
				axis = layout.Horizontal
			}
			args = args[1:]
		}
		if _, err := c.doSplitIn(c.win(), axis, strings.Join(args, " ")); err != nil {
			c.statusMsg = err.Error()
		}

	case "select-window":
		if len(args) != 1 {
			c.statusMsg = "usage: select-window IDX"
			return
		}
		idx, err := strconv.Atoi(args[0])
		if err != nil {
			c.statusMsg = "usage: select-window IDX"
			return
		}
		c.selectWindowIndex(idx)

	case "rename-window":
		if len(args) == 0 {
			c.statusMsg = "usage: rename-window NAME"
			return
		}
		w := c.win()
		w.Name = strings.Join(args, " ")
		w.renamed = true
		c.persistStateLocked()

	case "send-keys":
		if len(args) == 0 {
			c.statusMsg = "usage: send-keys TEXT... [Enter]"
			return
		}
		enter := false
		if args[len(args)-1] == "Enter" {
			enter = true
			args = args[:len(args)-1]
		}
		if p, ok := c.panes[c.win().active.ID]; ok {
			p.Write([]byte(strings.Join(args, " ")))
			if enter {
				p.Write([]byte("\r"))
			}
		}

	case "kill-pane":
		c.killActive()

	case "break-pane":
		c.breakPaneToNewWindow()

	case "respawn-pane":
		c.respawnActivePane()

	default:
		c.statusMsg = "unknown command: " + name
	}
}
