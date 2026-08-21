package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/config"
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

// TestThemesCommandListsEveryBuiltInTheme runs the real binary the same
// way TestVersionFlag does. The point of the subcommand is that the
// program itself can answer "what are the valid theme names" — a
// misspelled "theme" line is deliberately ignored in silence, so the
// only alternative is guessing — which makes it worth checking that it
// prints every name config actually knows, not just some of them.
func TestThemesCommandListsEveryBuiltInTheme(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "termdock-test-bin")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	out, err := exec.Command(bin, "themes").CombinedOutput()
	if err != nil {
		t.Fatalf("themes: %v\n%s", err, out)
	}
	for _, name := range config.ThemeNames() {
		if !strings.Contains(string(out), name) {
			t.Errorf("theme %q missing from `termdock themes` output:\n%s", name, out)
		}
	}
}

// The config file's own documentation spells the theme names out (it
// used to just say "see ThemeNames", which is a Go identifier and no
// help to anyone reading it); this keeps that list from going stale.
func TestConfigDocMentionsEveryTheme(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("internal", "config", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	header, _, _ := strings.Cut(string(doc), "package config")
	for _, name := range config.ThemeNames() {
		if !strings.Contains(header, name) {
			t.Errorf("theme %q is not named in config.go's settings documentation", name)
		}
	}
}
