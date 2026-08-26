package main

import "testing"

// parseTarget reads the "-t" spec that every scripting verb takes, and
// send-keys types whatever it is given into whatever it resolves to. A
// spec that resolves to the wrong pane is therefore worse than one that
// fails, which is what these pin down.
func TestParseTarget(t *testing.T) {
	for _, tc := range []struct {
		spec    string
		session string
		winIdx  int
		winName string
		pane    int
	}{
		{"main", "main", -1, "", 0},
		{"main:", "main", -1, "", 0},
		{"main:2", "main", 2, "", 0},
		{"main:0", "main", 0, "", 0},
		{"main:2.3", "main", 2, "", 3},
		{"main:build", "main", -1, "build", 0},
		{"main:build.2", "main", -1, "build", 2},
		// A trailing dot names no pane, which means the window's active one.
		{"main:2.", "main", 2, "", 0},
		// A window name containing a dot survives only up to the first one,
		// same as tmux; the rest is read as the pane.
		{"main:my.win", "main", -1, "my", 0},
		// Colons past the first belong to the window part.
		{"main:a:b", "main", -1, "a:b", 0},
		// A negative number is not an index. -1 is the "no window given"
		// sentinel, so treating it as one would quietly select the active
		// window; as a name it fails to resolve, which is the honest answer.
		{"main:-1", "main", -1, "-1", 0},
		{"main:-3", "main", -1, "-3", 0},
	} {
		s, wi, wn, p := parseTarget(tc.spec)
		if s != tc.session || wi != tc.winIdx || wn != tc.winName || p != tc.pane {
			t.Errorf("parseTarget(%q) = (%q, %d, %q, %d), want (%q, %d, %q, %d)",
				tc.spec, s, wi, wn, p, tc.session, tc.winIdx, tc.winName, tc.pane)
		}
	}
}

// A negative window index must never come back as the sentinel, whatever
// the number: that is the case where the command would act on a window
// nobody named.
func TestParseTargetNeverTurnsANegativeIntoTheActiveWindow(t *testing.T) {
	for _, spec := range []string{"s:-1", "s:-2", "s:-99", "s:-1.1"} {
		_, idx, name, _ := parseTarget(spec)
		if idx != -1 {
			t.Errorf("parseTarget(%q) gave window index %d", spec, idx)
		}
		if name == "" {
			t.Errorf("parseTarget(%q) left both the index and the name empty, which resolves to the active window", spec)
		}
	}
}

// extractTarget pulls "-t X" out of an argument list and hands back what
// is left, which is the command's own arguments.
func TestExtractTarget(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		target string
		rest   []string
	}{
		{[]string{"-t", "main", "hello"}, "main", []string{"hello"}},
		{[]string{"hello", "-t", "main"}, "main", []string{"hello"}},
		{[]string{"--target", "main:1", "a", "b"}, "main:1", []string{"a", "b"}},
		{[]string{"hello"}, "", []string{"hello"}},
		// A dangling -t names nothing, and must not eat past the end.
		{[]string{"hello", "-t"}, "", []string{"hello", "-t"}},
	} {
		target, rest := extractTarget(tc.args)
		if target != tc.target {
			t.Errorf("extractTarget(%q) target = %q, want %q", tc.args, target, tc.target)
		}
		if len(rest) != len(tc.rest) {
			t.Errorf("extractTarget(%q) rest = %q, want %q", tc.args, rest, tc.rest)
			continue
		}
		for i := range rest {
			if rest[i] != tc.rest[i] {
				t.Errorf("extractTarget(%q) rest = %q, want %q", tc.args, rest, tc.rest)
				break
			}
		}
	}
}
