package config

import (
	"os/exec"
	"strings"
	"testing"
)

// The popup opens, runs its command and closes when the command exits.
// So a popup-command that isn't a program produces a popup that flashes
// and vanishes with no error anywhere — indistinguishable from the
// feature being broken, which is how it was reported. The value has to
// be refused while there is still a screen to refuse it on.
func TestPopupCommandMustBeSomethingRunnable(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH to test against")
	}
	for _, tc := range []struct {
		cmd     string
		wantErr bool
		why     string
	}{
		{"", false, "empty means \"a shell\""},
		{"sh", false, "a program on PATH"},
		{"sh -c 'echo hi'", false, "arguments are the program's business, not ours"},
		{"definitely-not-a-real-program-xyz", true, "not on PATH"},
		{"/nonexistent/path/to/thing", true, "a path that isn't there"},
		{"/", true, "a directory"},
	} {
		err := CheckPopupCommand(tc.cmd)
		if tc.wantErr && err == nil {
			t.Errorf("CheckPopupCommand(%q) accepted it, want a refusal (%s)", tc.cmd, tc.why)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("CheckPopupCommand(%q) = %v, want accepted (%s)", tc.cmd, err, tc.why)
		}
	}
}

// A refusal has to name the program it could not find, otherwise it says
// no without saying what to change.
func TestPopupCommandRefusalNamesTheProgram(t *testing.T) {
	err := CheckPopupCommand("definitely-not-a-real-program-xyz --flag")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-program-xyz") {
		t.Errorf("refusal %q should name the program", err)
	}
	if strings.Contains(err.Error(), "--flag") {
		t.Errorf("refusal %q should blame the program, not its arguments", err)
	}
}

// Every setting that has to be typed needs to say what it will take.
// The ones with a fixed list don't: you step through those and read
// them. This is the gap that let a free-text row look like a field you
// could write anything into.
func TestTypedSettingsSayWhatTheyAccept(t *testing.T) {
	for _, s := range Settings() {
		if len(s.Choices()) > 0 {
			continue
		}
		if s.Hint == "" {
			t.Errorf("setting %q is typed but has no Hint saying what it accepts", s.Key)
		}
	}
}

// This setting used to be a bool, so "on" is sitting in config files
// that already exist. It has to keep meaning what it meant.
func TestStatusIconsStillAcceptsTheOldOnOff(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"on", "nerd"},
		{"off", "off"},
		{"unicode", "unicode"},
		{"nerd", "nerd"},
	} {
		cfg := Default()
		if err := Set(&cfg, "status-icons", tc.in); err != nil {
			t.Errorf("set status-icons %q: %v", tc.in, err)
			continue
		}
		if got := Get(&cfg, "status-icons"); got != tc.want {
			t.Errorf("set status-icons %q -> %q, want %q", tc.in, got, tc.want)
		}
	}
	cfg := Default()
	if err := Set(&cfg, "status-icons", "yes-please"); err == nil {
		t.Error("status-icons accepted a value that is not one of its choices")
	}
}
