package config

import (
	"os"
	"path/filepath"
	"sort"
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

func TestLoadParsesBindOverrides(t *testing.T) {
	writeConfig(t, "bind M jump-picker\nbind Space cycle-layout\n")
	cfg := Load()
	want := map[rune]string{'M': "jump-picker", ' ': "cycle-layout"}
	if len(cfg.BindOverrides) != len(want) {
		t.Fatalf("BindOverrides = %v, want %v", cfg.BindOverrides, want)
	}
	for r, act := range want {
		if cfg.BindOverrides[r] != act {
			t.Fatalf("BindOverrides[%q] = %q, want %q", r, cfg.BindOverrides[r], act)
		}
	}
}

func TestLoadIgnoresMalformedBindLines(t *testing.T) {
	writeConfig(t, "bind toolong jump-picker\nbind M\nbind\n")
	cfg := Load()
	if len(cfg.BindOverrides) != 0 {
		t.Fatalf("malformed bind lines should all be ignored, got %v", cfg.BindOverrides)
	}
}

func TestDefaultHasNoBindOverrides(t *testing.T) {
	if len(Default().BindOverrides) != 0 {
		t.Fatalf("expected no bind overrides by default, got %v", Default().BindOverrides)
	}
}

func TestLoadParsesFocusEvents(t *testing.T) {
	writeConfig(t, "focus-events on\n")
	if cfg := Load(); !cfg.FocusEvents {
		t.Fatal("expected FocusEvents = true")
	}
}

func TestDefaultHasFocusEventsOff(t *testing.T) {
	if Default().FocusEvents {
		t.Fatal("expected FocusEvents off by default")
	}
}

func TestLoadParsesPopupCommand(t *testing.T) {
	writeConfig(t, "popup-command lazygit --arg\n")
	cfg := Load()
	if cfg.PopupCommand != "lazygit --arg" {
		t.Fatalf("PopupCommand = %q, want %q", cfg.PopupCommand, "lazygit --arg")
	}
}

func TestDefaultHasEmptyPopupCommand(t *testing.T) {
	if Default().PopupCommand != "" {
		t.Fatalf("expected an empty PopupCommand by default, got %q", Default().PopupCommand)
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

// ThemeNames is now derived from the themes map, so "every name has an
// entry" can no longer fail by construction. What is still worth
// pinning down is that every listed name actually *does* something:
// a theme whose colors were left zero would be printed by "termdock
// themes" as a real choice and then quietly change nothing.
func TestEveryThemeNameAppliesRealColors(t *testing.T) {
	if len(ThemeNames()) == 0 {
		t.Fatal("no built-in themes at all")
	}
	for _, name := range ThemeNames() {
		cfg := loadFrom(t, "theme "+name+"\n")
		def := Default()
		if cfg.StatusBG == def.StatusBG && cfg.StatusFG == def.StatusFG && cfg.PaneActiveBG == def.PaneActiveBG {
			t.Errorf("theme %q left every color at the default — it is listed but does nothing", name)
		}
	}
}

// Every theme must colour the panes, not just termdock's own chrome —
// that is what makes a theme look like a theme rather than a differently
// coloured status bar on your emulator's background.
func TestEveryThemeSetsPaneColors(t *testing.T) {
	for _, name := range ThemeNames() {
		cfg := loadFrom(t, "theme "+name+"\n")
		if cfg.PaneBG == tcell.ColorDefault || cfg.PaneFG == tcell.ColorDefault {
			t.Errorf("theme %q leaves pane bg/fg at the terminal default", name)
		}
		// The status bar sits on top of the panes, so a theme whose bar is
		// the same colour as the panes has no bar to speak of.
		if cfg.StatusBG == cfg.PaneBG {
			t.Errorf("theme %q uses the same colour for the status bar and the panes", name)
		}
	}
}

// pane-bg/pane-fg must stay overridable, including back to "default"
// (the emulator's own), which is the opt-out for someone who wants a
// theme's chrome but their own terminal background.
func TestExplicitPaneColorsBeatTheTheme(t *testing.T) {
	cfg := loadFrom(t, "theme dracula\npane-bg default\npane-fg red\n")
	if cfg.PaneBG != tcell.ColorDefault {
		t.Errorf("PaneBG = %v, want the explicit ColorDefault to win over the theme", cfg.PaneBG)
	}
	if cfg.PaneFG != tcell.ColorRed {
		t.Errorf("PaneFG = %v, want red", cfg.PaneFG)
	}
	if cfg.PaneActiveBG != themes["dracula"].paneActiveBG {
		t.Error("the rest of the theme should still apply")
	}
}

// Two themes with the same three colors would both be listed by
// "termdock themes" as real choices while being impossible to tell
// apart — most likely a copy-paste when adding one.
func TestThemesAreVisuallyDistinct(t *testing.T) {
	seen := map[theme]string{}
	for _, name := range ThemeNames() {
		th := themes[name]
		if other, dup := seen[th]; dup {
			t.Errorf("theme %q has exactly the same colors as %q", name, other)
		}
		seen[th] = name
	}
}

// ThemeNames must stay sorted: it is user-facing output ("termdock
// themes"), and map iteration order is random.
func TestThemeNamesAreSorted(t *testing.T) {
	names := ThemeNames()
	if !sort.StringsAreSorted(names) {
		t.Errorf("ThemeNames() is not sorted: %v", names)
	}
}

// loadFrom writes body to a throwaway config file and loads it.
func loadFrom(t *testing.T, body string) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "termdock.conf")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TERMDOCK_CONFIG", path)
	return Load()
}

// TestBoolSettingsAreCaseInsensitive: "mouse ON" used to compare against
// the literal lowercase "on" and fall through to false, silently turning
// the mouse off — the opposite of what the line says, and not something
// anyone would think to blame the config file for.
func TestBoolSettingsAreCaseInsensitive(t *testing.T) {
	for _, val := range []string{"on", "ON", "On", "true", "TRUE", "yes", "1"} {
		if cfg := loadFrom(t, "focus-events "+val+"\n"); !cfg.FocusEvents {
			t.Errorf("focus-events %q should enable it", val)
		}
		if cfg := loadFrom(t, "mouse "+val+"\n"); !cfg.Mouse {
			t.Errorf("mouse %q should enable it", val)
		}
	}
	for _, val := range []string{"off", "OFF", "false", "no", "0"} {
		if cfg := loadFrom(t, "mouse "+val+"\n"); cfg.Mouse {
			t.Errorf("mouse %q should disable it", val)
		}
	}
}

// TestUnrecognizedBoolKeepsTheDefault holds this package to its own
// documented contract (see Load): a bad value falls back to the default
// for that setting rather than quietly meaning "off".
func TestUnrecognizedBoolKeepsTheDefault(t *testing.T) {
	cfg := loadFrom(t, "mouse enable-please\n")
	if !cfg.Mouse {
		t.Error("an unrecognized mouse value should leave the default (on) alone, not disable the mouse")
	}
	cfg = loadFrom(t, "focus-events maybe\n")
	if cfg.FocusEvents {
		t.Error("an unrecognized focus-events value should leave the default (off) alone")
	}
}

func TestValidColorsStillApply(t *testing.T) {
	cfg := loadFrom(t, "status-bg red\nstatus-fg #00ff00\npane-active-bg default\n")
	if cfg.StatusBG != tcell.ColorRed {
		t.Errorf("status-bg = %v, want red", cfg.StatusBG)
	}
	if cfg.StatusFG != tcell.GetColor("#00ff00") {
		t.Errorf("status-fg = %v, want #00ff00", cfg.StatusFG)
	}
	if cfg.PaneActiveBG != tcell.ColorDefault {
		t.Errorf("pane-active-bg = %v, want the terminal default (asked for explicitly)", cfg.PaneActiveBG)
	}
}

// TestTypoedColorKeepsTheDefault: tcell.GetColor answers ColorDefault for
// a name it doesn't know, so assigning it blindly turned a typo into
// "whatever the terminal does" rather than the documented default.
func TestTypoedColorKeepsTheDefault(t *testing.T) {
	cfg := loadFrom(t, "status-bg dracula-purple\n")
	if cfg.StatusBG != Default().StatusBG {
		t.Errorf("status-bg after a typo = %v, want the default %v", cfg.StatusBG, Default().StatusBG)
	}
}

// TestTypoedColorDoesNotBlockATheme: a color the file set explicitly wins
// over a "theme" line, but a *typo* isn't a deliberate setting and must
// not stop the theme from filling that slot in.
func TestTypoedColorDoesNotBlockATheme(t *testing.T) {
	themed := loadFrom(t, "theme dracula\n")
	typoed := loadFrom(t, "theme dracula\nstatus-bg nonsense-color\n")
	if typoed.StatusBG != themed.StatusBG {
		t.Errorf("status-bg = %v with a typo'd override, want the theme's %v", typoed.StatusBG, themed.StatusBG)
	}

	// ...while a real one still takes precedence over the theme.
	explicit := loadFrom(t, "theme dracula\nstatus-bg red\n")
	if explicit.StatusBG != tcell.ColorRed {
		t.Errorf("an explicit status-bg should still beat the theme, got %v", explicit.StatusBG)
	}
}

// repeat-time differs from every other numeric setting in that 0 is a
// meaningful value (it disables repeating) rather than a rejected one.
func TestLoadParsesRepeatTimeIncludingZero(t *testing.T) {
	for _, tc := range []struct {
		line string
		want int
	}{
		{"repeat-time 250", 250},
		{"repeat-time 0", 0},
		{"repeat-time nonsense", 1000}, // unparseable: keep the default
		{"repeat-time -5", 1000},       // negative: same
	} {
		path := filepath.Join(t.TempDir(), "termdock.conf")
		if err := os.WriteFile(path, []byte(tc.line+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TERMDOCK_CONFIG", path)
		if got := Load().RepeatTime; got != tc.want {
			t.Errorf("%q -> RepeatTime = %d, want %d", tc.line, got, tc.want)
		}
	}
}
