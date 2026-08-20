package core

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"termdock/internal/persist"
)

// toggleLogging starts or stops mirroring the active pane's raw output
// to a file on disk — bound to Ctrl-B L, termdock's built-in equivalent
// of the tmux-logging plugin / `pipe-pane -o 'cat >>file'`, with no
// external tooling or hand-written pipe command needed. Logs land under
// $XDG_STATE_HOME/termdock/logs (falling back to
// ~/.local/state/termdock/logs, the same base directory session
// snapshots use — see internal/persist.Dir), named after the session,
// window, and pane so multiple logs never collide, with a timestamp so
// starting a new log for the same pane later doesn't overwrite the last
// one.
func (c *Core) toggleLogging() {
	w := c.win()
	p, ok := c.panes[w.active.ID]
	if !ok {
		return
	}
	if path, logging := p.LogPath(); logging {
		p.StopLogging()
		c.statusMsg = "stopped logging (saved to " + path + ")"
		return
	}
	dir, err := logsDir()
	if err != nil {
		c.statusMsg = "logging error: " + err.Error()
		return
	}
	name := fmt.Sprintf("%s-w%d-p%d-%s.log", c.SessionName, c.windowIndex(w), w.active.ID, time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := p.StartLogging(path); err != nil {
		c.statusMsg = "logging error: " + err.Error()
		return
	}
	c.statusMsg = "logging pane to " + path
}

// logsDir returns (creating it, mode 0700, if needed) the directory
// pane logs are written to.
func logsDir() (string, error) {
	base, err := persist.Dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
