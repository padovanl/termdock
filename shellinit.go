package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// `termdock shell-init` prints the shell snippet that makes semantic
// prompt marks (OSC 133) work — the thing that lets termdock tell a
// prompt from a command from its output, which in turn is what makes
// "jump to the previous command" and "copy that command's output"
// possible at all. See internal/vt10x/marks.go.
//
// It is printed rather than installed. The snippet goes in a file the
// user owns and has opinions about, and a program that edits your shell
// rc behind your back is a program you stop trusting; `eval "$(termdock
// shell-init bash)"` is one line they can read first and put where they
// want.

// shellSnippets is the emitted code, one per shell.
//
// bash and zsh differ in more than syntax here. zsh has real hooks
// (precmd/preexec) that fire in exactly the right places, so the marks
// land where they should with no games. bash has only PROMPT_COMMAND and
// the DEBUG trap, and the DEBUG trap fires for *every* command in a
// pipeline as well as for things the prompt itself runs — hence the
// guard that emits the "command started" mark once per prompt cycle.
// Getting that wrong is what makes hand-rolled versions of this mark the
// prompt's own subshells as commands.
var shellSnippets = map[string]string{
	"bash": `# termdock semantic prompt marks (OSC 133)
__termdock_preexec() {
  # The DEBUG trap fires for every command in a pipeline, and for the
  # commands PROMPT_COMMAND itself runs. Only the first one after a
  # prompt is the command the user actually typed.
  [[ -n "$__termdock_in_cmd" ]] && return
  [[ "$BASH_COMMAND" == "$PROMPT_COMMAND" ]] && return
  __termdock_in_cmd=1
  printf '\033]133;C\007'
}
__termdock_precmd() {
  local exit=$?
  # Close the previous command only if one was actually started, so a
  # bare Enter at the prompt doesn't report a command that never ran.
  if [[ -n "$__termdock_in_cmd" ]]; then
    printf '\033]133;D;%s\007' "$exit"
    unset __termdock_in_cmd
  fi
  printf '\033]133;A\007'
  return $exit
}
trap '__termdock_preexec' DEBUG
PROMPT_COMMAND="__termdock_precmd${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
# B marks the end of the prompt; appending it to PS1 is the only place
# that is true regardless of what the prompt itself contains.
PS1="${PS1}\[\033]133;B\007\]"
`,

	"zsh": `# termdock semantic prompt marks (OSC 133)
autoload -Uz add-zsh-hook
__termdock_precmd() {
  local exit=$?
  if [[ -n "$__termdock_in_cmd" ]]; then
    print -n "\e]133;D;$exit\a"
    unset __termdock_in_cmd
  fi
  print -n "\e]133;A\a"
}
__termdock_preexec() {
  __termdock_in_cmd=1
  print -n "\e]133;C\a"
}
add-zsh-hook precmd __termdock_precmd
add-zsh-hook preexec __termdock_preexec
PS1="${PS1}%{$(print -n "\e]133;B\a")%}"
`,

	"fish": `# termdock semantic prompt marks (OSC 133)
function __termdock_precmd --on-event fish_prompt
    printf '\033]133;A\007'
end
function __termdock_preexec --on-event fish_preexec
    printf '\033]133;C\007'
end
function __termdock_postexec --on-event fish_postexec
    printf '\033]133;D;%s\007' $status
end
`,
}

// cmdShellInit prints the snippet for the named shell, defaulting to
// whatever $SHELL says when none is given — the shell they are in is
// nearly always the one they mean.
func cmdShellInit(args []string) {
	name := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			name = a
			break
		}
	}
	if name == "" {
		name = filepath.Base(os.Getenv("SHELL"))
	}
	name = strings.ToLower(strings.TrimSpace(name))

	snippet, ok := shellSnippets[name]
	if !ok {
		known := make([]string, 0, len(shellSnippets))
		for k := range shellSnippets {
			known = append(known, k)
		}
		sort.Strings(known)
		fatal(fmt.Sprintf("no shell integration for %q; supported: %s",
			name, strings.Join(known, ", ")))
	}

	fmt.Print(snippet)
	// To stderr so `eval "$(termdock shell-init)"` stays clean while a
	// bare run still explains itself.
	fmt.Fprintf(os.Stderr, "\n# Add to your %s startup file:\n#   eval \"$(termdock shell-init %s)\"\n",
		name, name)
}
