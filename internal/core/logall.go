package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/padovanl/termdock/internal/layout"
)

// Ctrl-B A starts logging *every* pane in the current window at once,
// into a directory you name, one file per pane.
//
// Ctrl-B L already logs a pane, but one at a time and to a path it
// chooses. That is the wrong shape for the case this exists for: you are
// about to reproduce something across six panes and want the lot on
// disk, in a directory you can hand to someone or attach to a ticket.
// Six keystrokes and six timestamped filenames in a state directory is
// not that.
//
// The files are named after the things you actually recognise — the
// session, the window, and the pane's own name where you gave it one
// (see renamePane) — because a directory of api.log, worker.log and
// db.log is worth something an hour later, and one of p3-20260821.log
// is not.

// startWindowLogging begins logging every pane of w into dir. Panes
// already logging are left alone rather than restarted, so pressing this
// after having started one by hand doesn't truncate it.
func (c *Core) startWindowLogging(w *Window, dir string) (started int, err error) {
	if dir == "" {
		return 0, fmt.Errorf("no directory given")
	}
	dir, err = expandPath(dir)
	if err != nil {
		return 0, err
	}
	// Created rather than demanded: being told "no such directory" when
	// you have just typed where you want the logs is a pointless round
	// trip.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("cannot use %s: %w", dir, err)
	}

	used := map[string]bool{}
	for i, leaf := range layout.Leaves(w.root) {
		p, ok := c.panes[leaf.ID]
		if !ok {
			continue
		}
		if _, already := p.LogPath(); already {
			continue
		}
		path := filepath.Join(dir, c.logFileName(w, leaf.ID, i+1, used))
		if err := p.StartLogging(path); err != nil {
			return started, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		started++
	}
	return started, nil
}

// stopWindowLogging stops every pane in w that is logging.
func (c *Core) stopWindowLogging(w *Window) int {
	stopped := 0
	for _, leaf := range layout.Leaves(w.root) {
		if p, ok := c.panes[leaf.ID]; ok {
			if _, logging := p.LogPath(); logging {
				p.StopLogging()
				stopped++
			}
		}
	}
	return stopped
}

// windowIsLogging reports whether any pane in w is being logged, which
// is what makes Ctrl-B A a toggle rather than a one-way switch.
func (c *Core) windowIsLogging(w *Window) bool {
	for _, leaf := range layout.Leaves(w.root) {
		if p, ok := c.panes[leaf.ID]; ok {
			if _, logging := p.LogPath(); logging {
				return true
			}
		}
	}
	return false
}

// logFileName builds an identifiable name: session, window, pane. The
// pane's own name is used when it has one, its position otherwise.
//
// used guards against collisions, which are entirely possible here since
// two panes may carry the same name and two windows the same title —
// nothing stops you calling both of them "logs". Silently having one
// file receive two panes' output would be worse than an ugly suffix.
func (c *Core) logFileName(w *Window, paneID, position int, used map[string]bool) string {
	pane := c.paneNames[paneID]
	if pane == "" {
		pane = fmt.Sprintf("pane%d", position)
	}
	base := fmt.Sprintf("%s-%s-%s",
		sanitizeForFilename(c.SessionName),
		sanitizeForFilename(c.windowDisplayName(w)),
		sanitizeForFilename(pane))

	name := base + ".log"
	for n := 2; used[name]; n++ {
		name = fmt.Sprintf("%s-%d.log", base, n)
	}
	used[name] = true
	return name
}

// unsafeForFilename matches anything that shouldn't go into a filename:
// path separators most of all, since a window called "feat/login" would
// otherwise send its log into a directory that doesn't exist.
var unsafeForFilename = regexp.MustCompile(`[^\w.-]+`)

func sanitizeForFilename(s string) string {
	s = unsafeForFilename.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(s, "._-")
	if s == "" {
		return "unnamed"
	}
	return s
}

// expandPath resolves a leading ~ and makes the path absolute, since
// what gets typed into a prompt is "~/logs" far more often than an
// absolute path.
func expandPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}

// promptWindowLogging is Ctrl-B A: stop if this window is already being
// logged, otherwise ask where to put the files.
func (c *Core) promptWindowLogging() {
	w := c.win()
	if c.windowIsLogging(w) {
		n := c.stopWindowLogging(w)
		c.statusMsg = fmt.Sprintf("stopped logging %d pane(s) in this window", n)
		return
	}
	c.startInput("log-window", "Log every pane in this window to: ", defaultLogDir(), ModeNormal)
}

// applyWindowLogging is confirmInput's "log-window" branch.
func (c *Core) applyWindowLogging(dir string) {
	if dir == "" {
		c.statusMsg = "no directory given — nothing is being logged"
		return
	}
	w := c.win()
	started, err := c.startWindowLogging(w, dir)
	if err != nil {
		c.statusMsg = "logging error: " + err.Error()
		return
	}
	resolved, _ := expandPath(dir)
	switch started {
	case 0:
		c.statusMsg = "every pane in this window was already being logged"
	default:
		c.statusMsg = fmt.Sprintf("logging %d pane(s) to %s", started, resolved)
	}
}

// defaultLogDir prefills the prompt with somewhere sensible, so the
// common case is Enter rather than typing a path.
func defaultLogDir() string {
	if dir, err := logsDir(); err == nil {
		return dir
	}
	return "."
}
