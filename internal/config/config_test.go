package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesStatusSegments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte("status-segments git, battery\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)

	cfg := Load()
	want := []string{"git", "battery"}
	if len(cfg.StatusSegments) != len(want) {
		t.Fatalf("StatusSegments = %v, want %v", cfg.StatusSegments, want)
	}
	for i, s := range want {
		if cfg.StatusSegments[i] != s {
			t.Fatalf("StatusSegments = %v, want %v", cfg.StatusSegments, want)
		}
	}
}

func TestDefaultHasNoStatusSegments(t *testing.T) {
	if len(Default().StatusSegments) != 0 {
		t.Fatalf("expected status segments off by default, got %v", Default().StatusSegments)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Setenv("TERMDOCK_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.conf"))
	cfg := Load()
	if cfg.Prefix != Default().Prefix || cfg.HistoryLimit != Default().HistoryLimit {
		t.Fatalf("a missing config file should fall back to defaults, got %+v", cfg)
	}
}
