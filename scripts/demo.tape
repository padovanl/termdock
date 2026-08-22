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
# vhs's parser has no nested quoting, so anything needing quotes is
# prepared in the hidden setup below and invoked by name.

Output docs/demo.gif

Set Shell bash
Set FontSize 15
Set Width 1200
Set Height 700
Set Padding 12
Set Theme "Dracula"
Set TypingSpeed 45ms

Hide
Type "cd /tmp && rm -rf tdemo && mkdir tdemo && cd tdemo" Enter
# The shell integration, and a couple of commands with quotes in them,
# staged as files so the tape itself stays quote-free.
Type "termdock shell-init bash > init.sh 2>/dev/null" Enter
Type "printf 'for i in 1 2 3; do echo job $i finished; sleep 0.4; done\n' > jobs.sh" Enter
Type "printf 'echo building...; sleep 0.6; echo ok\n' > build.sh" Enter
Type "export PS1='$ '" Enter
Type "clear" Enter
Show

# ── A workspace, and the shell taught to mark its prompts ─────────────
Type "termdock new -s demo" Enter
Sleep 2s
Type "source init.sh" Enter
Sleep 600ms
Type "clear" Enter
Sleep 400ms

# Split, and give the panes names — so every screen after this reads as
# words instead of as bash, bash, bash.
Ctrl+b
Type "v"
Sleep 800ms
Ctrl+b
Type "."
Sleep 500ms
Type "worker"
Enter
Sleep 600ms

Type "source /tmp/tdemo/init.sh" Enter
Sleep 500ms
Type "clear" Enter
Sleep 300ms
Type "bash /tmp/tdemo/jobs.sh" Enter
Sleep 2200ms

Ctrl+b
Left
Sleep 500ms
Ctrl+b
Type "."
Sleep 400ms
Type "api"
Enter
Sleep 600ms

Type "bash /tmp/tdemo/build.sh" Enter
Sleep 1800ms

# A command that fails: its exit status appears on the pane's title.
# Nothing else in this class of tool knows a command even happened.
Type "false" Enter
Sleep 2000ms

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
Sleep 2600ms
Escape
Sleep 700ms

# ── Close a pane by mistake — and take it back ───────────────────────
Ctrl+b
Type "x"
Sleep 1400ms
Ctrl+b
Type "Z"
Sleep 2000ms

# ── Detach. The work carries on without the terminal ─────────────────
Ctrl+b
Type "d"
Sleep 1400ms
Type "termdock ls" Enter
Sleep 2200ms

Hide
Type "termdock kill-session -t demo" Enter
Sleep 600ms
