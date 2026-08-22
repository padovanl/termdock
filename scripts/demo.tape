# termdock's demo GIF, recorded with vhs (https://github.com/charmbracelet/vhs):
#
#   vhs scripts/demo.tape
#
# What it shows, and why these and not others: every beat has to be
# legible in a few seconds with no narration, and has to be something
# tmux cannot do. That rules out most of the keybindings — splitting a
# pane is not a selling point — and it rules out anything whose value
# lives in the explanation rather than the picture.
#
# The order is a story rather than a feature list: build a workspace,
# see the things nothing else can show you, then recover from a mistake
# and walk away with the work still running.
#
# 1920x1080 at font size 24 gives about 128x34 cells: wide enough for
# three panes that are each still readable once the GIF is scaled down
# to the width of a README, which is the only size anyone sees it at.
#
# vhs's parser has no nested quoting, so anything needing quotes is
# prepared in the hidden setup below and invoked by name.

Output docs/demo.gif

Set Shell bash
Set FontSize 24
Set Width 1920
Set Height 1080
Set Padding 14
Set Theme "Dracula"
Set TypingSpeed 40ms

Hide
Type "cd /tmp && rm -rf tdemo && mkdir tdemo && cd tdemo" Enter
# The shell integration, and the commands with quotes in them, staged as
# files so the tape itself stays quote-free.
Type "termdock shell-init bash > init.sh 2>/dev/null" Enter
Type "printf 'for i in 1 2 3 4; do echo job $i finished; sleep 0.4; done\n' > jobs.sh" Enter
Type "printf 'echo building...; sleep 0.9; echo ok\n' > build.sh" Enter
Type "printf 'while true; do echo [$(date +%%H:%%M:%%S)] request served; sleep 0.7; done\n' > tail.sh" Enter
Type "export PS1='$ '" Enter
# A config of its own, so recording this never touches a real one. The
# status segments and their no-font-needed icons are on from the start.
Type "printf 'status-segments cpu,mem\nstatus-icons unicode\n' > td.conf" Enter
Type "export TERMDOCK_CONFIG=/tmp/tdemo/td.conf" Enter
Type "clear" Enter
Show

# ── A workspace, and the shell taught to mark its prompts ─────────────
Type "termdock new -s demo" Enter
Sleep 2s
Type "source init.sh" Enter
Sleep 500ms
Type "clear" Enter
Sleep 400ms

# Three panes, each given a name — so every screen after this reads as
# words instead of as bash, bash, bash.
Ctrl+b
Type "v"
Sleep 700ms
Ctrl+b
Type "."
Sleep 400ms
Type "worker"
Enter
Sleep 500ms
Type "source /tmp/tdemo/init.sh" Enter
Sleep 400ms
Type "clear" Enter
Sleep 300ms

Ctrl+b
Type "s"
Sleep 700ms
Ctrl+b
Type "."
Sleep 400ms
Type "logs"
Enter
Sleep 500ms
Type "source /tmp/tdemo/init.sh" Enter
Sleep 400ms
Type "clear" Enter
Sleep 300ms
Type "bash /tmp/tdemo/tail.sh" Enter
Sleep 1200ms

Ctrl+b
Up
Sleep 400ms
Type "bash /tmp/tdemo/jobs.sh" Enter
Sleep 1800ms

Ctrl+b
Left
Sleep 500ms
Ctrl+b
Type "."
Sleep 400ms
Type "api"
Enter
Sleep 500ms
Type "bash /tmp/tdemo/build.sh" Enter
Sleep 1800ms

# A command that fails: its exit status appears on the pane's title.
# Nothing else in this class of tool knows a command even happened.
Type "false" Enter
Sleep 1800ms

# ── Retheme the whole session live, from a command prompt ────────────
Ctrl+b
Type ":"
Sleep 700ms
Type "set theme nord"
Enter
Sleep 2000ms

# Every setting, with its current value, editable in place.
Ctrl+b
Type "C"
Sleep 2400ms
Escape
Sleep 800ms

# ── Every pane at once, live, not a list of names ─────────────────────
Ctrl+b
Type "g"
Sleep 2200ms
Right
Sleep 1200ms
Escape
Sleep 600ms

# ── Jump anywhere, with a live minimap of where you are going ─────────
Ctrl+b
Type "w"
Sleep 1800ms
Down
Sleep 1400ms
Escape
Sleep 600ms

# ── Every command run in the session, any pane, with how it ended ─────
Ctrl+b
Type "H"
Sleep 2400ms
Escape
Sleep 600ms

# ── And when each one ran, on one shared scale ────────────────────────
Ctrl+b
Type "T"
Sleep 2800ms
Escape
Sleep 700ms

# ── One pane, full screen, and back — without disturbing the others ───
Ctrl+b
Type "z"
Sleep 1600ms
Ctrl+b
Type "z"
Sleep 1000ms

# ── Close a pane by mistake — and take it back ───────────────────────
Ctrl+b
Type "x"
Sleep 1600ms
Ctrl+b
Type "Z"
Sleep 2200ms

# ── Detach. The work carries on without the terminal ─────────────────
Ctrl+b
Type "d"
Sleep 1400ms
Type "termdock ls" Enter
Sleep 2400ms

Hide
Type "termdock kill-session -t demo" Enter
Sleep 600ms
