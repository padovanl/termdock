// Package persist defines termdock's on-disk session-snapshot format —
// each window's split layout and every pane's working directory — and
// the plain file I/O to read and write it. Nothing here can capture a
// pane's actual running process (a build mid-compile, a REPL's history);
// what survives a crash or a reboot is the session's shape: the same
// trade-off tmux-resurrect makes for tmux, not a stronger guarantee.
package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Node mirrors internal/layout.Node, keeping only what's needed to
// rebuild the split tree and relaunch a shell in the right place — no
// pane IDs, process handles, or computed Rects, all of which are
// meaningless across a restart.
type Node struct {
	Split int // matches internal/layout.SplitType: 0 leaf, 1 vertical, 2 horizontal
	Ratio float64
	Cwd   string `json:",omitempty"` // leaf only
	Name  string `json:",omitempty"` // leaf only: a name the user gave this pane
	// Scrollback is the tail of what the pane had on screen, oldest line
	// first, as plain text. Restored by writing it back into the fresh
	// pane so a recovered session opens showing what you were reading —
	// see Core.restoreScrollback for why it is text and not cells.
	Scrollback []string `json:",omitempty"`
	First      *Node    `json:",omitempty"`
	Second     *Node    `json:",omitempty"`
}

// Window is one snapshotted window: its display name (if the user set
// one) and its pane tree.
type Window struct {
	Name      string
	Renamed   bool
	SyncPanes bool
	Root      Node
}

// Snapshot is one session's full saved state.
type Snapshot struct {
	SessionName string
	Windows     []Window
}

// Dir returns (creating it, mode 0700, if needed) the directory session
// snapshots are kept in: $XDG_STATE_HOME/termdock, falling back to
// ~/.local/state/termdock — the standard location for state that should
// outlive a single run but isn't user configuration (that's
// internal/config's job) or a runtime-only socket (internal/server's).
func Dir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "termdock")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func path(sessionName string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionName+".json"), nil
}

// Save writes snap for sessionName, atomically (write-then-rename) so a
// process dying mid-write can't leave a half-written file for the next
// Load to choke on. Meant to be called best-effort: a caller ignoring the
// returned error is the expected usage, not an oversight — losing
// crash-recovery fidelity is a regret, never something that should
// disrupt the session actually running right now.
func Save(sessionName string, snap Snapshot) error {
	p, err := path(sessionName)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Load reads back sessionName's last Saved snapshot. ok is false if
// there is none, or it's unreadable or corrupt — treated identically to
// "none", since a stale or half-written file should never block starting
// a fresh session under that name.
func Load(sessionName string) (Snapshot, bool) {
	p, err := path(sessionName)
	if err != nil {
		return Snapshot{}, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Snapshot{}, false
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, false
	}
	return snap, true
}

// Delete removes sessionName's snapshot, if any. Called when a session
// ends on purpose (Ctrl-B q, its last pane exiting normally, kill-session)
// so an intentional shutdown doesn't resurrect itself the next time that
// name is used — only an unclean end (crash, reboot) should leave a
// snapshot behind to recover from.
func Delete(sessionName string) {
	if p, err := path(sessionName); err == nil {
		os.Remove(p)
	}
}
