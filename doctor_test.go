package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/padovanl/termdock/internal/config"
)

// The check that prompted the whole command: a misspelled theme is
// ignored in silence, so nothing anywhere says the line was wrong.
func TestDoctorFlagsAnUnknownTheme(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte("theme drakula\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)

	if got := configuredTheme(); got != "drakula" {
		t.Fatalf("configuredTheme() = %q, want the misspelled name as written", got)
	}
	if knownTheme("drakula") {
		t.Error("a misspelled name must not count as known")
	}
	if !knownTheme("dracula") {
		t.Error("a real theme should count as known")
	}
}

// A trailing comment is stripped by the config parser, so the check has
// to strip it too or it reports a perfectly good theme as unknown.
func TestDoctorIgnoresATrailingComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte("theme nord # my favourite\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)

	if got := configuredTheme(); got != "nord" {
		t.Fatalf("configuredTheme() = %q, want %q", got, "nord")
	}
}

// A commented-out theme line is not a theme.
func TestDoctorSkipsCommentedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte("# theme dracula\nmouse on\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)

	if got := configuredTheme(); got != "" {
		t.Fatalf("configuredTheme() = %q, want none", got)
	}
}

// The whole point is telling someone what to do about it, so a warning
// without a fix is a warning that wasted their time.
func TestEveryDoctorWarningCarriesAFix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte("theme nonesuch\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)
	t.Setenv("TCELL_TRUECOLOR", "disable")

	for _, c := range []doctorCheck{
		checkTheme(config.Load()),
		checkTrueColor(config.Load()),
		checkShellIntegration(),
	} {
		if c.result == checkWarn && strings.TrimSpace(c.fix) == "" {
			t.Errorf("check %q warns but suggests nothing", c.name)
		}
	}
}
