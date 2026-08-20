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
| `z` | zoom: the active pane fills the whole screen — its border turns magenta and its title gets a `[Z]` tag, so it's obvious you're zoomed (`z` again to undo) |
| `r` | **resize-mode**: subsequent arrows/hjkl resize the pane, any other key exits |
| `[` | enter **copy-mode** (scroll the scrollback, see below) |
| `y` | toggle **sync-panes**: keystrokes get sent to every pane in this window at once |
| `x` | close the active pane |
| `c` | create a new **window** (tab) |
| `n` / `p` | switch to the next / previous window |
| `w` | **jump picker**: type to fuzzy-filter every window/pane, ↑↓/Tab to select, Enter to jump (see below) |
| `0`-`9` | jump straight to window N |
| `,` | rename the current window |
| `&` | close the current window and every pane in it (asks `y`/`n` first) |
| `]` | paste the most recently copied (yanked) text into the active pane |
| `d` | **detach**: disconnect from the session, which keeps running in the background |
| `q` | quit termdock (closes the whole session and every window) |
| `?` | open a scrollable keybinding reference (any key closes it) |
| `Ctrl-B` | double-press: sends a literal `Ctrl-B` to the active pane |

If a shell exits (e.g. with `exit`), its pane closes on its own; when a
window's last pane closes, the window itself closes; once the last window
closes, the session ends.

### Windows

Each window is a fully independent set of panes and its own split layout
— like a browser tab, or a tmux window. The status bar shows the window
list on the left as a strip of colored tabs, e.g. ` 0:bash  1:vim!
2:htop* `: the accent color marks the one you're looking at, orange marks
one that produced output while you weren't (`*`/`!` are still there in
the text too, in case colors aren't your thing). Click a tab to switch to
it, the same as a browser tab, and drag a tab sideways to reorder the
strip. A window's name is automatically the foreground command of its
active pane, the same as pane titles, until you rename it with `Ctrl-B
,`. Drag a pane's *title bar* onto a different window's tab to move that
pane there instead — the pane's process is untouched, only which
window's layout it belongs to changes, the same way dragging a browser
tab out into another window doesn't reload the page.

### Jump picker

`Ctrl-B w` opens a type-ahead picker listing every pane in every window of
the session — tmux splits this across `choose-window` and `choose-pane`,
each a static list you page through with j/k; termdock's is one list,
fuzzy-filtered as you type (characters just need to appear in order, not
consecutively — `depl` matches `1:deploy`), so getting to a specific pane
in a session with a dozen windows is a few keystrokes instead of hunting
through a list by eye. `↑`/`↓`/`Tab` move the selection, `Enter` jumps
straight to it — switching windows and focusing that exact pane in one
step — `Esc` cancels. The selected entry also gets a live preview box
next to the list, a small peek at that pane's actual content that updates
as you move the selection — no need to jump blind. With an empty query
(the moment it opens, or after clearing it) the list is ordered
most-recently-used first instead, so `Ctrl-B w` then `Enter` is a fast
"jump back to whatever I was just looking at," Alt-Tab style.

### Help

`Ctrl-B ?` opens a scrollable reference listing every keybinding —
`↑`/`↓`/`j`/`k`/`PgUp`/`PgDn` scroll, any other key closes it. It's the
same floating box the jump picker uses, just without the type-ahead
filter, so a long list stays readable instead of getting crammed into
(and clipped off of) a single status bar line.

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
- Click-drag on a pane's content: selects text and copies it on release —
  no need to enter copy-mode first, the same as any ordinary terminal.
  (A plain click with no drag still just focuses the pane.)
- Double-click a pane's title bar: zooms it (same as `Ctrl-B z`); double-click
  again to unzoom.
- Drag a pane's title bar onto a different window's tab: moves that pane
  into that window (see [Windows](#windows)).
- Drag the border between two panes, side by side or stacked: resizes
  them.
- Click a window tab in the status bar: switches to it. Drag it sideways
  to reorder the tab strip, the same as dragging a browser tab.
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
`Ctrl-B ?` opens the same list as a proper scrollable screen instead (see
[Help](#help)), for when you want to actually read it rather than catch
it in passing.

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

### Session persistence

A tmux session doesn't survive the server crashing or the machine
rebooting on its own — that's what plugins like tmux-resurrect are for.
termdock does this natively: every structural change (split, close,
rename, move) — and, in the background, every 30s besides, to catch a
pane's plain `cd` drifting where a structural-change-only save wouldn't
notice — writes a snapshot of the session's window/pane layout and each
pane's working directory to
`$XDG_STATE_HOME/termdock/SESSION.json` (falling back to
`~/.local/state/termdock/`). If the daemon for a session with that name
isn't already running when you `termdock new` it, and a snapshot exists,
it's restored: the same split layout and window names, a fresh shell
relaunched in each pane's last known directory. What's running inside
each pane (a build mid-compile, a REPL, `vim`'s undo history) is *not*
recovered — nothing can resurrect actual process state after a crash,
only put you back where you were about to be. A session that ends on
purpose (`Ctrl-B q`, its last pane exiting normally, `termdock
kill-session`) deletes its own snapshot on the way out, so it doesn't
resurrect itself the next time that name is reused; only an unclean end
leaves one behind to recover from.

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
- `internal/persist` — the on-disk session-snapshot format and file I/O
  for crash/reboot recovery (see [Session persistence](#session-persistence)).
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
