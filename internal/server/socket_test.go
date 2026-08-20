package server

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSocketPathRejectsNamesThatEscapeTheSocketDir: a session name is
// pasted straight into a socket path (and the daemon's log path next to
// it), and filepath.Join resolves whatever it's handed. "proj/api" aimed
// at a subdirectory nothing creates, so starting the session failed with
// a confusing message about an internal log file; "../escaped" actually
// climbed out of the sockets directory and left a *running* daemon whose
// socket List() never looks at, so `termdock ls` couldn't see it.
func TestSocketPathRejectsNamesThatEscapeTheSocketDir(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../escaped", "proj/api", "a\\b", "nul\x00byte"} {
		if _, err := SocketPath(name); err == nil {
			t.Errorf("SocketPath(%q) should be rejected", name)
		}
		if err := ValidateSessionName(name); err == nil {
			t.Errorf("ValidateSessionName(%q) should be rejected", name)
		}
	}
}

func TestSocketPathAcceptsOrdinaryNames(t *testing.T) {
	dir, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	for _, name := range []string{"main", "0", "my-project", "work_2", "dev.api", ".hidden"} {
		got, err := SocketPath(name)
		if err != nil {
			t.Errorf("SocketPath(%q): %v", name, err)
			continue
		}
		if want := filepath.Join(dir, name+".sock"); got != want {
			t.Errorf("SocketPath(%q) = %q, want %q", name, got, want)
		}
		// Whatever the name, the socket has to stay a direct child of the
		// sockets directory — that's the property List()/kill-session rely on.
		if parent := filepath.Dir(got); parent != strings.TrimSuffix(dir, "/") {
			t.Errorf("SocketPath(%q) landed in %q, want it directly inside %q", name, parent, dir)
		}
	}
}
