package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionFlag builds the real binary and runs it with each spelling
// of the version flag — main() dispatches on os.Args directly rather
// than going through a testable helper, so exercising the actual
// compiled CLI is the only way to pin down its argv parsing without
// restructuring main() just for a test. This is also exactly what
// goreleaser/Homebrew's own formula `test do ... end` block runs against
// a real install, so it's worth the same real-binary approach here.
func TestVersionFlag(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "termdock-test-bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	for _, flag := range []string{"-v", "--version", "version"} {
		out, err := exec.Command(bin, flag).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", flag, err, out)
		}
		if !strings.HasPrefix(string(out), "termdock version ") {
			t.Fatalf("%s: output = %q, want a \"termdock version \" prefix", flag, out)
		}
	}
}
