package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/server"
)

// `termdock doctor` checks the things that fail *silently*.
//
// Almost every setting here is deliberately lenient: a line it doesn't
// understand is ignored so a typo can never stop a session starting.
// That is the right trade, and it has a cost — a misspelled theme, a
// shell that was never told to emit prompt marks, or a terminal that
// quietly quantises colours all present as "I set it and nothing
// happened", with nothing anywhere saying why. This is where to look.
//
// Every check reports what it found, not just a verdict, so the output
// is useful in a bug report from someone whose machine you cannot see.

type checkResult int

const (
	checkOK checkResult = iota
	checkWarn
	checkInfo
)

func (r checkResult) mark() string {
	switch r {
	case checkOK:
		return "  ok  "
	case checkWarn:
		return " warn "
	}
	return " note "
}

type doctorCheck struct {
	name   string
	result checkResult
	detail string
	fix    string // what to do about it, when there is something to do
}

func cmdDoctor() {
	cfg := config.Load()
	checks := []doctorCheck{
		checkConfigFile(cfg),
		checkTheme(cfg),
		checkTrueColor(cfg),
		checkShellIntegration(),
		checkShell(cfg),
		checkSocketDir(),
		checkSessions(),
	}

	warnings := 0
	fmt.Println("termdock doctor")
	fmt.Println()
	for _, c := range checks {
		fmt.Printf("[%s] %-22s %s\n", c.result.mark(), c.name, c.detail)
		if c.fix != "" {
			fmt.Printf("                                → %s\n", c.fix)
		}
		if c.result == checkWarn {
			warnings++
		}
	}
	fmt.Println()
	if warnings == 0 {
		fmt.Println("Nothing to fix.")
		return
	}
	fmt.Printf("%d thing(s) worth looking at.\n", warnings)
}

func checkConfigFile(cfg config.Config) doctorCheck {
	path := config.Path()
	if _, err := os.Stat(path); err != nil {
		return doctorCheck{"config file", checkInfo,
			"none at " + path,
			"optional — create it to set a theme, prefix key, and so on"}
	}
	return doctorCheck{"config file", checkOK, path, ""}
}

// checkTheme is the one that prompted this command: an unrecognised
// theme name is ignored in silence, so the colours simply stay default
// and nothing anywhere says the line was misspelled.
func checkTheme(cfg config.Config) doctorCheck {
	name := configuredTheme()
	switch {
	case name == "":
		return doctorCheck{"theme", checkInfo, "none set (default colours)",
			"`termdock themes` lists the built-in ones"}
	case !knownTheme(name):
		return doctorCheck{"theme", checkWarn,
			fmt.Sprintf("%q is not a built-in theme, so the line is being ignored", name),
			"check the spelling against `termdock themes`"}
	}
	return doctorCheck{"theme", checkOK, name, ""}
}

// checkTrueColor matters because the failure is subtle rather than
// visible: without 24-bit colour a theme's exact shades get rounded to
// the nearest of 256 palette slots, which looks like "the theme is
// slightly wrong" rather than like a configuration problem.
func checkTrueColor(cfg config.Config) doctorCheck {
	switch {
	case os.Getenv("TCELL_TRUECOLOR") == "disable":
		return doctorCheck{"24-bit colour", checkWarn,
			"disabled by TCELL_TRUECOLOR=disable — theme colours will be approximated",
			"unset it unless your terminal really is 256-colour only"}
	case os.Getenv("COLORTERM") != "":
		return doctorCheck{"24-bit colour", checkOK, "COLORTERM=" + os.Getenv("COLORTERM"), ""}
	}
	// termdock turns it on itself when a theme is set; say so rather
	// than reporting a problem that isn't one.
	return doctorCheck{"24-bit colour", checkOK,
		"COLORTERM unset; termdock enables it itself when a theme is in use", ""}
}

// checkShellIntegration looks for the snippet in the usual startup
// files. It cannot ask the running shell, so it reports what it can see
// and says so — a false "not set up" would be worse than an honest
// "couldn't find it here".
func checkShellIntegration() doctorCheck {
	home, err := os.UserHomeDir()
	if err != nil {
		return doctorCheck{"shell integration", checkInfo, "cannot read home directory", ""}
	}
	candidates := []string{".bashrc", ".bash_profile", ".zshrc", ".profile",
		filepath.Join(".config", "fish", "config.fish")}
	for _, rel := range candidates {
		data, err := os.ReadFile(filepath.Join(home, rel))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "termdock shell-init") {
			return doctorCheck{"shell integration", checkOK, "configured in ~/" + rel, ""}
		}
	}
	return doctorCheck{"shell integration", checkWarn,
		"no `termdock shell-init` line found in your shell startup files",
		"add: eval \"$(termdock shell-init)\" — enables command history, jumping between commands, and copying one command's output"}
}

func checkShell(cfg config.Config) doctorCheck {
	shell := cfg.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		return doctorCheck{"shell", checkWarn, "neither the config's `shell` nor $SHELL is set",
			"panes will fall back to a default"}
	}
	if _, err := os.Stat(shell); err != nil {
		return doctorCheck{"shell", checkWarn, fmt.Sprintf("%s is not executable or does not exist", shell),
			"new panes will fail to start; fix `shell` in the config"}
	}
	return doctorCheck{"shell", checkOK, shell, ""}
}

func checkSocketDir() doctorCheck {
	dir, err := server.Dir()
	if err != nil {
		return doctorCheck{"socket directory", checkWarn, err.Error(),
			"sessions cannot start without it"}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return doctorCheck{"socket directory", checkWarn, err.Error(), ""}
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		return doctorCheck{"socket directory", checkWarn,
			fmt.Sprintf("%s is mode %04o, not 0700 — other users on this machine may reach your sessions", dir, perm),
			fmt.Sprintf("chmod 700 %s", dir)}
	}
	return doctorCheck{"socket directory", checkOK, dir, ""}
}

func checkSessions() doctorCheck {
	infos, err := server.List()
	if err != nil {
		return doctorCheck{"sessions", checkInfo, "cannot list: " + err.Error(), ""}
	}
	if len(infos) == 0 {
		return doctorCheck{"sessions", checkInfo, "none running", ""}
	}
	names := make([]string, 0, len(infos))
	for _, in := range infos {
		names = append(names, in.Name)
	}
	return doctorCheck{"sessions", checkOK,
		fmt.Sprintf("%d running: %s", len(infos), strings.Join(names, ", ")), ""}
}

// configuredTheme is the name the config file asks for, read from the
// file rather than from the parsed Config — because Config.Theme is only
// filled in when the name was recognised, and the whole point here is to
// report the case where it was not.
func configuredTheme() string {
	data, err := os.ReadFile(config.Path())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "theme" {
			// Trailing "# comment" is stripped by the parser, so match it.
			name := fields[1]
			if i := strings.Index(name, "#"); i >= 0 {
				name = name[:i]
			}
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func knownTheme(name string) bool {
	for _, n := range config.ThemeNames() {
		if n == name {
			return true
		}
	}
	return false
}
