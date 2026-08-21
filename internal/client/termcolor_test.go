package client

import (
	"io"
	"os"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/padovanl/termdock/internal/config"
)

// With no theme, termdock must not touch the emulator's own colours at
// all — recolouring someone's terminal because they launched a
// multiplexer would be well beyond what they asked for, and the reset on
// exit only runs if we changed something in the first place.
func TestNoThemeLeavesTheEmulatorsColorsAlone(t *testing.T) {
	cfg := config.Default()
	if _, ok := oscColor(cfg.PaneBG); ok {
		t.Error("default PaneBG should produce no OSC colour")
	}
	if _, ok := oscColor(cfg.PaneFG); ok {
		t.Error("default PaneFG should produce no OSC colour")
	}
}

func TestOSCColorFormatsRGBAndNamedColors(t *testing.T) {
	for _, tc := range []struct {
		in   tcell.Color
		want string
	}{
		{tcell.NewHexColor(0x300a24), "#300a24"},
		{tcell.NewHexColor(0x000000), "#000000"},
		{tcell.ColorRed, "#ff0000"},
	} {
		got, ok := oscColor(tc.in)
		if !ok || got != tc.want {
			t.Errorf("oscColor(%v) = %q, %v; want %q, true", tc.in, got, ok, tc.want)
		}
	}
}

// A themed config must ask for both, so the emulator's padding matches
// the panes rather than staying whatever the profile set.
func TestThemedConfigProducesBothOSCColors(t *testing.T) {
	cfg := config.Default()
	cfg.PaneBG = tcell.NewHexColor(0x191724)
	cfg.PaneFG = tcell.NewHexColor(0xe0def4)
	bg, okBG := oscColor(cfg.PaneBG)
	fg, okFG := oscColor(cfg.PaneFG)
	if !okBG || bg != "#191724" {
		t.Errorf("background OSC colour = %q, %v", bg, okBG)
	}
	if !okFG || fg != "#e0def4" {
		t.Errorf("foreground OSC colour = %q, %v", fg, okFG)
	}
}

// setTerminalColors/resetTerminalColors write straight to stdout, so
// capture it: an escape sequence that's subtly wrong wouldn't fail
// anything, it would just quietly not work (or print garbage into the
// user's terminal).
func TestTerminalColorSequencesWrittenToStdout(t *testing.T) {
	cfg := config.Default()
	cfg.PaneBG = tcell.NewHexColor(0x191724)
	cfg.PaneFG = tcell.NewHexColor(0xe0def4)

	if got := captureStdout(t, func() { setTerminalColors(cfg) }); got != "\x1b]10;#e0def4\a\x1b]11;#191724\a" {
		t.Errorf("set sequence = %q", got)
	}
	if got := captureStdout(t, func() { resetTerminalColors() }); got != "\x1b]110\a\x1b]111\a" {
		t.Errorf("reset sequence = %q", got)
	}
	// The no-theme case must emit nothing at all, not an empty OSC.
	if got := captureStdout(t, func() { setTerminalColors(config.Default()) }); got != "" {
		t.Errorf("unthemed config wrote %q to the terminal, want nothing", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}
