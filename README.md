# termdock

A tmux-style terminal multiplexer: run multiple shells in the same
terminal, split it into panes, and keep sessions running in the
background — you can detach and reattach later, even from a different
terminal, without losing anything running inside.

Written in Go, with no dependency on tmux/screen: it manages
pseudo-terminals (ptys) and a VT100 emulator (with scrollback) for each
pane directly.

## Architecture

`termdock` is split into a **server** and a **client**, like tmux:

- The **server** is a daemon process, detached from the terminal, that
  actually owns the panes (the shells, their ptys, the terminal emulator).
  It stays alive until you explicitly kill it, independent of any
  attached client.
- The **client** is what you see: it connects to the server over a unix
  socket, draws the screen, and forwards keys/mouse input. Closing it (or
  detaching with `Ctrl-B d`, or losing the connection) doesn't touch the
  panes: they stay alive on the server, ready for a new attach.

This is what makes detach/reattach possible.

## Build

```sh
go build -o termdock .
```

Requires Go 1.21+ and a POSIX system (Linux/macOS/WSL).

## Usage

```sh
termdock                        # attach to (or create) the default "main" session
termdock new [-s NAME]          # create a new session and attach to it
termdock attach [-t NAME]       # attach to an existing session
termdock ls                     # list active sessions
termdock kill-session -t NAME   # terminate a session (and all its panes)
```

Sessions survive the terminal closing: you can disconnect over SSH, close
the window, or press `Ctrl-B d`, and find everything exactly as you left
it with a plain `termdock attach`.

## Keys

Every command starts with the **`Ctrl-B` prefix**, followed by a key. Any
other key, when `Ctrl-B` wasn't just pressed, is forwarded straight to the
active pane.

| Key after `Ctrl-B` | Action |
|---|---|
| `v` or `%` | split the active pane **vertically** (side by side) |
| `s` or `"` | split the active pane **horizontally** (stacked) |
| `←/→/↑/↓` or `h/j/k/l` | move focus to the adjacent pane |
| `o` or `Tab` | cycle to the next pane |
| `z` | zoom: the active pane fills the whole screen (`z` again to undo) |
| `r` | **resize-mode**: subsequent arrows/hjkl resize the pane, any other key exits |
| `[` | enter **copy-mode** (scroll the scrollback, see below) |
| `y` | toggle **sync-panes**: keystrokes get sent to every pane in this window at once |
| `x` | close the active pane |
| `c` | create a new **window** (tab) |
| `n` / `p` | switch to the next / previous window |
| `0`-`9` | jump straight to window N |
| `,` | rename the current window |
| `&` | close the current window (and every pane in it) |
| `]` | paste the most recently copied (yanked) text into the active pane |
| `d` | **detach**: disconnect from the session, which keeps running in the background |
| `q` | quit termdock (closes the whole session and every window) |
| `?` | show the full key list in the status bar (until the next command) |
| `Ctrl-B` | double-press: sends a literal `Ctrl-B` to the active pane |

If a shell exits (e.g. with `exit`), its pane closes on its own; when a
window's last pane closes, the window itself closes; once the last window
closes, the session ends.

### Windows

Each window is a fully independent set of panes and its own split layout
— like a browser tab, or a tmux window. The status bar shows the window
list on the left, e.g. `[0:bash 1:vim! 2:htop*]`: `*` marks the one
you're looking at, `!` marks one that produced output while you weren't
(cleared the moment you switch to it). A window's name is automatically
the foreground command of its active pane, the same as pane titles, until
you rename it with `Ctrl-B ,`.

### Copy-mode (scrollback and copying)

Enter with `Ctrl-B [`. From there:

- `h/j/k/l` or arrows: move the cursor; `PgUp`/`PgDn`/`Ctrl-U`/`Ctrl-D`:
  page/half-page; `g`/`G`: top/bottom of the scrollback.
- `v`: start a selection from the cursor.
- `y` or `Enter`: copy the selection and exit copy-mode. The copied text
  is pushed to the real terminal via **OSC52**, so it lands in the system
  clipboard on terminals that support it (essentially every modern one:
  iTerm2, Windows Terminal, kitty, WezTerm, recent gnome-terminal...).
- `/`: search the scrollback (type the text, `Enter` jumps to the closest
  match searching forward). `n` / `N` repeat the last search forward /
  backward. Matching is a plain case-insensitive substring, no regex.
- `q` or `Esc`: exit without copying.

### Mouse

- Click a pane: gives it focus.
- Drag the border between two panes, side by side or stacked: resizes
  them.
- Wheel: if the pane has scrollback, automatically enters copy-mode and
  scrolls; scrolling back to the bottom exits it automatically.
- Drag in copy-mode: selects text and copies it on release.

### Pane titles and borders

Every pane is framed by a thin border, with its title embedded in the top
edge (e.g. `2:vim`) — the number is the pane's position within its window
(left-to-right, top-to-bottom), not an internal id, so it stays small and
predictable as you split and close panes. The title shows the foreground
command, not just the shell name, so you can tell what's running where at
a glance. (Linux only; elsewhere it always shows the shell name.) The
active pane's border is drawn in the accent color (`pane-active-bg`
below) so it's obvious which pane has focus.

### Status bar

The left side is minimal at rest (`Ctrl-B ?`) so there's room for the
right side: hostname and clock, tmux-style. Press the prefix and the left
side expands to the full key list for as long as you're mid-command;
`Ctrl-B ?` pins that same list in place until your next command, for
when you just want to read it.

## Scripting a session

Beyond the interactive keys, termdock has a small command-line interface
for driving a session from outside — a setup script that opens a project
in a few pre-arranged windows, a Makefile target that tails a log in a
split pane, anything you'd otherwise have to click through by hand. This
is what `tmux send-keys`/`new-window`/`split-window`/... are for tmux.

`TARGET` is `SESSION[:WINDOW[.PANE]]` — e.g. `main`, `main:1`, `main:1.4`.
Omitting `WINDOW` means "the active window"; omitting `PANE` means "that
window's active pane".

```sh
termdock send-keys -t TARGET text... [Enter]     # type text into a pane; trailing "Enter" submits it
termdock new-window -t SESSION [-n NAME] [cmd...] # new window, optionally running cmd instead of the shell
termdock split-window -t TARGET [-v|-s] [cmd...]  # -v side by side (default), -s stacked
termdock select-window -t SESSION:WINDOW          # make a window the visible one
termdock list-windows -t SESSION
termdock list-panes -t SESSION[:WINDOW]
```

```sh
# example: set up a 3-pane dev environment in one shot
termdock new-window -t main -n dev
termdock split-window -t main:dev -s 'npm run dev'
termdock split-window -t main:dev.1 -v 'npm test -- --watch'
termdock select-window -t main:dev
```

These commands work whether or not anyone is currently attached to the
session — they talk straight to the daemon over its socket, the same way
`termdock ls`/`kill-session` do, and don't require (or start) a client.

### Small terminals

The layout degrades gracefully instead of breaking, the same way tmux
keeps shrinking panes rather than refusing to redraw: panes shrink
proportionally down to zero size if there truly isn't room, the outer
border/margin around the whole pane area is dropped first to give a very
small terminal every last row and column of real content, and the status
bar is the first thing to go on a single-row terminal. Nothing crashes at
any size; existing splits just get harder to see the smaller you go,
exactly like a real multiplexer.

## Configuration

Optional config file at `$XDG_CONFIG_HOME/termdock/termdock.conf`
(falling back to `~/.config/termdock/termdock.conf`), or wherever
`$TERMDOCK_CONFIG` points. Plain `key value` lines, `#` for comments — no
command language, unlike tmux.conf. Everything is optional; a missing
file just means the defaults below.

```conf
# termdock.conf
prefix C-a             # prefix key, any Ctrl+letter (default C-b)
mouse on                # enable mouse support (default on)
history-limit 10000     # scrollback lines kept per pane (default 10000)
shell /bin/zsh           # shell for new panes (default $SHELL)
status-bg black          # status bar background (default black)
status-fg silver         # status bar foreground (default silver)
pane-active-bg teal       # active pane's border/title color (default teal)
```

Colors accept any W3C name tcell understands, or `#rrggbb` hex.
`prefix`/`history-limit`/`shell` are read by the **server**, so they take
effect when a session is *created* (`termdock new`), not on every attach;
`mouse` and the colors are read by the **client**, so they apply per
attach and can differ between two clients looking at the same session.

## Code layout

- `main.go` — CLI: subcommands (`new`/`attach`/`ls`/`kill-session`) and
  starting the background daemon.
- `cli.go` — the scripting commands (`send-keys`/`new-window`/...).
- `internal/config` — the optional config file: prefix key, mouse, colors,
  scrollback size, shell.
- `internal/pane` — one pane: a shell process in a pty + the VT100
  emulator that interprets its output.
- `internal/vt10x` — the terminal emulator ([vt10x](https://github.com/hinshun/vt10x),
  vendored and extended with a scrollback buffer, since upstream doesn't
  have one).
- `internal/layout` — the binary split tree (vertical/horizontal) that
  computes each pane's on-screen rectangle, and interactive resizing.
- `internal/core` — the session's brain, with no terminal attached:
  windows, panes, layout, copy-mode, mouse, resize-mode. Runs in the
  server.
- `internal/proto` — the messages exchanged between client and server
  over the socket.
- `internal/server` — the daemon: owns a session, accepts clients over a
  unix socket, broadcasts a frame whenever something changes.
- `internal/client` — the UI: connects to the server, draws with
  [tcell](https://github.com/gdamore/tcell), forwards keys and mouse.

## What's missing (on purpose, for now)

Still not there, to keep the scope manageable: remapping individual key
bindings (only the prefix key itself is configurable), regex search
(substring only), named/numbered paste buffers (just the one most-recent
register), and reflowing scrollback lines to a new width on resize (they
keep whatever width they were written at).
