package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
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

func writeConfig(t *testing.T, contents string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "termdock.conf")
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)
}

func TestThemeSetsBundledColors(t *testing.T) {
	writeConfig(t, "theme dracula\n")
	cfg := Load()
	want := themes["dracula"]
	if cfg.StatusBG != want.statusBG || cfg.StatusFG != want.statusFG || cfg.PaneActiveBG != want.paneActiveBG {
		t.Fatalf("theme dracula: got StatusBG=%v StatusFG=%v PaneActiveBG=%v, want the dracula preset", cfg.StatusBG, cfg.StatusFG, cfg.PaneActiveBG)
	}
}

func TestUnknownThemeIsIgnored(t *testing.T) {
	writeConfig(t, "theme not-a-real-theme\n")
	cfg := Load()
	def := Default()
	if cfg.StatusBG != def.StatusBG || cfg.StatusFG != def.StatusFG || cfg.PaneActiveBG != def.PaneActiveBG {
		t.Fatalf("an unknown theme name should leave the defaults untouched, got %+v", cfg)
	}
}

// TestExplicitColorOverridesThemeRegardlessOfOrder checks both file
// orderings: a theme is meant to be a baseline that an explicit
// status-bg/status-fg/pane-active-bg line always wins over, whether that
// line comes before or after the theme line — applyTheme's overridden
// map is what makes this order-independent rather than "last line in
// the file wins," which would silently break if a user reordered their
// config.
func TestExplicitColorOverridesThemeRegardlessOfOrder(t *testing.T) {
	custom := tcell.NewHexColor(0x123456)

	t.Run("color after theme", func(t *testing.T) {
		writeConfig(t, "theme dracula\nstatus-bg #123456\n")
		cfg := Load()
		if cfg.StatusBG != custom {
			t.Fatalf("StatusBG = %v, want the explicit #123456 override", cfg.StatusBG)
		}
		if cfg.PaneActiveBG != themes["dracula"].paneActiveBG {
			t.Fatalf("PaneActiveBG should still come from the theme, got %v", cfg.PaneActiveBG)
		}
	})

	t.Run("color before theme", func(t *testing.T) {
		writeConfig(t, "status-bg #123456\ntheme dracula\n")
		cfg := Load()
		if cfg.StatusBG != custom {
			t.Fatalf("StatusBG = %v, want the explicit #123456 override to survive a later theme line", cfg.StatusBG)
		}
		if cfg.PaneActiveBG != themes["dracula"].paneActiveBG {
			t.Fatalf("PaneActiveBG should still come from the theme, got %v", cfg.PaneActiveBG)
		}
	})
}

func TestThemeNamesAreAllApplicable(t *testing.T) {
	for _, name := range ThemeNames() {
		if _, ok := themes[name]; !ok {
			t.Errorf("ThemeNames() lists %q, but it has no entry in the themes map", name)
		}
	}
	if len(ThemeNames()) != len(themes) {
		t.Errorf("ThemeNames() lists %d names, but themes has %d entries", len(ThemeNames()), len(themes))
	}
}
