package pane

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewInDirSetsCwd(t *testing.T) {
	dir, err := os.MkdirTemp("", "termdock-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)
	// Resolve symlinks (e.g. /tmp -> /private/tmp on macOS, or WSL's
	// mount quirks) so the comparison below isn't fooled by a path that
	// refers to the same directory but isn't byte-identical.
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}

	p, err := NewInDir(NextID(), 80, 24, dir)
	if err != nil {
		t.Fatalf("NewInDir: %v", err)
	}
	defer p.Close()

	got := p.Cwd()
	if got == "" {
		t.Skip("Cwd() returned \"\" — likely running on a platform without /proc (see cwd_other.go)")
	}
	if got != real {
		t.Fatalf("Cwd() = %q, want %q", got, real)
	}
}
