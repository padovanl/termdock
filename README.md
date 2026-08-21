# 🐳 termdock

[![CI](https://github.com/padovanl/termdock/actions/workflows/ci.yml/badge.svg)](https://github.com/padovanl/termdock/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Reference](https://img.shields.io/badge/go-1.21%2B-00ADD8?logo=go)](go.mod)

A terminal multiplexer in the tmux/screen tradition — split your terminal
into panes, run multiple shells side by side, and keep everything running
in the background so you can detach and reattach later, even from a
different machine, without losing a thing.

![termdock in action: splitting into panes, the live pane overview (Ctrl-B g), and the floating popup terminal (Ctrl-B P), all in the Dracula color theme with the git branch segment in the status bar](docs/demo.gif)

Written from scratch in Go, with **no dependency on tmux or screen**: it
manages pseudo-terminals (ptys) and its own VT100 emulator (with
scrollback) for every pane directly.

It also does a handful of things tmux doesn't, by default, at all — 🔍 a
fuzzy jump picker, 🖼️ a live pane overview, 🔎 search across every pane's
scrollback at once, 🔄 session switching without detaching, 🪟 a floating
popup terminal, 🔗 a link/path picker, 🔔 background activity notification,
📝 built-in pane logging, and 💾 crash/reboot recovery among them. See
[✨ What makes it different](#-what-makes-it-different-from-tmux).

## ✨ What makes it different from tmux

| | termdock | tmux |
|---|---|---|
| 🔍 **Jump to a window/pane** | one type-ahead, fuzzy-filtered list of every pane, ranked most-recently-used | separate `choose-window` / `choose-pane`, paged with j/k, no fuzzy filter |
| 🖼️ **See every pane at once** | a live-thumbnail grid overview (`Ctrl-B g`) | no equivalent — one pane at a time |
| 🔎 **Search the scrollback** | one search across *every* pane in *every* window | copy-mode `/` only ever searches the one pane you're already in |
| 🔄 **Switch sessions** | fuzzy-picker, no detach required (`Ctrl-B S`) | detach and reattach, or `choose-tree` |
| 👀 **Screen-share / pair** | a real read-only attach mode (`termdock attach -r`) | everyone who attaches can type |
| 📋 **Paste history** | a fuzzy picker over your last 20 yanks (`Ctrl-B =`) | `choose-buffer`, same idea, no fuzzy filter |
| 🖱️ **Select text** | click-drag on any pane, anywhere, any time | needs `mouse on` + drag-to-select behavior varies by config |
| 🖱️ **Reorder/move things** | drag a window tab to reorder it, drag a pane's title onto a tab to relocate it | `swap-window`/`join-pane`, typed out by index |
| 💾 **Survive a crash or reboot** | automatic — layout + working directories snapshotted continuously, restored on next launch | tmux needs the external `tmux-resurrect` plugin |
| 🔢 **Pane/window numbering** | always a clean, positional `1, 2, 3…` | numbers can leave gaps after closing one, unless `renumber-windows` is set |
| ❓ **Keybinding help** | a real scrollable screen | crammed into (and clipped by) the status line |
| ⚠️ **Closing a whole window** | asks `y`/`n` first | gone immediately, no confirmation |
| 🖼️ **Panel borders** | always-on, junction-aware box drawing with the active pane highlighted | thin borders, style is you-configure-it |
| 🎯 **Zoom feedback** | the zoomed pane's border turns magenta and gets a `[Z]` tag | no visual cue you're zoomed beyond the layout itself |
| 🪟 **Scratch terminal** | a floating popup pane (`Ctrl-B P`) toggled over whatever you're doing, no layout change | tmux needs a dedicated popup session and a scripted `display-popup` binding |
| 🔗 **Open a URL/path** | fuzzy-pick any link or path visible on screen, copied straight to your clipboard (`Ctrl-B u`) | tmux needs the external `tmux-open`/`tmux-fpp` plugins |
| 🔔 **Background activity** | one real terminal bell (`\a`) the moment a background window's `*` first lights up, not per line | tmux's `monitor-activity`/`visual-activity` needs both set explicitly |
| ✂️ **Break a pane out** | `Ctrl-B !` moves the active pane into its own new window | tmux's `break-pane`, same idea, same key |
| 📊 **Status bar segments** | opt-in git branch / battery segments, no extra process | tmux needs `#()` shell hooks in `status-right` |
| 🔢 **Jump to a pane by number** | `Ctrl-B Q` badges every pane with a digit, press it to jump | tmux's `display-panes`, same idea, `Ctrl-B q` (termdock's `q` already means quit) |
| 💻 **Command prompt** | `Ctrl-B :` runs the same verbs as the scripting CLI, no target needed | tmux's `command-prompt`, same key, but needs an explicit target most of the time |
| 🧱 **Preset layouts** | `Ctrl-B Space` cycles tiled / even-columns / even-rows | tmux's `next-layout`, same key, same idea |
| 🔁 **Respawn a pane** | `Ctrl-B R` restarts the shell in place, no confirmation needed (same as `x`) | tmux's `respawn-pane` has no default binding and needs `-k` to force a live pane |
| 📝 **Log a pane to a file** | `Ctrl-B L` toggles it, no path to type, `[REC]` on the title | tmux needs the external tmux-logging plugin, or a hand-written `pipe-pane` |
| 🎨 **Ready-made color themes** | built in — one `theme <name>` config line (Dracula, Nord, Gruvbox, Catppuccin, Solarized, Tokyo Night), no plugin | tmux needs the external tmux-themepack or a per-theme plugin |
| ⏮️ **Toggle the last window** | `Ctrl-B W` flips back to whichever window you were just on | `Ctrl-B l`, same idea (termdock's own `l` is pane-left, vim-style) |
| ⏮️ **Toggle the last pane** | `Ctrl-B ;` flips back to whichever pane you were just on in this window | `Ctrl-B ;`, same key, same idea |
| 📈 **CPU / memory segments** | built in, read straight from `/proc`, no subprocess | tmux needs the external `tmux-cpu`/`tmux-mem-cpu-load` plugins |
| 📏 **Line-wise copy selection** | `V` in copy-mode selects whole lines, `v` switches back without losing the selection | tmux's own copy-mode line-selection, same `V`/`v` idea |
| 🪟 **Popup running a specific tool** | `popup-command lazygit` in the config — no scripted binding needed | tmux needs a scripted `bind ... display-popup -E lazygit` |
| ⌨️ **Rebind a keybinding** | one `bind <key> <action>` config line, e.g. `bind M jump-picker` | tmux's own `bind-key`, same idea |
| 🎯 **Pane focus events** | `focus-events on` forwards synthetic focus-in/out on every pane/window switch | tmux's own `focus-events`, same idea (plus real OS-level focus tmux also forwards — see [🚧 What's missing](#-whats-missing-on-purpose-for-now)) |
| ➡️ **Move pane focus from a script** | `termdock select-pane -t TARGET -L/-R/-U/-D`, no client attached | tmux's own `select-pane -L/-R/-U/-D` |
| ⚠️ **Quitting the whole session** | `Ctrl-B q` asks `y`/`n` first — closing every window and pane at once deserves the same care `&` already gets | gone immediately, no confirmation |
| ⏩ **Repeating a focus move** | after `Ctrl-B ←`, a bare `←` keeps moving — and only the *arrows* repeat, so it can never swallow the `h` of something you start typing | tmux's `bind -r`, but its repeatable movement keys are `hjkl` too, which does eat typed letters |
| 🔎 **Regex search** | copy-mode `/` and global search both accept a regex (falls back to a literal substring if it doesn't compile) | copy-mode search is a plain substring only |

Every "tmux needs the external `tmux-whatever` plugin" above is describing **tmux**, not termdock: everything in the termdock column is built into the single `termdock` binary. There's no plugin manager, no plugin API, and nothing here — themes, status segments, logging, the popup, any of it — ever needs an external plugin, script, or program to work. The one narrow exception is the `git` status segment, which shells out to your system's own `git` binary the same way any git integration would (not a termdock plugin, just using the tool that's already there) — everything else is pure Go, self-contained.

## 🏗️ Architecture

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

## 📦 Installation

```sh
# Homebrew (macOS or Linux)
brew install padovanl/termdock/termdock

# prebuilt binary — download the tar.gz for your OS/arch from
# https://github.com/padovanl/termdock/releases and put the termdock
# binary somewhere on your $PATH

# from source, with Go 1.21+ already installed
go install github.com/padovanl/termdock@latest
```

Every [release](https://github.com/padovanl/termdock/releases) ships
prebuilt binaries for linux/darwin × amd64/arm64, built with
[goreleaser](https://goreleaser.com) (see `.goreleaser.yml`) —
`termdock --version` reports exactly which one you're running.
`.github/workflows/release.yml` is currently manual-only
(`workflow_dispatch`, no automatic tag trigger) while the project's
still under active development; see the comment at the top of that file
for how to switch it back to firing on every pushed `vX.Y.Z` tag once
you're ready to start cutting real releases.

### 🍺 Homebrew tap

`brew install padovanl/termdock/termdock` needs a `homebrew-termdock`
tap repository to exist under the same GitHub account — Homebrew's own
naming convention for taps — which goreleaser then pushes an updated
formula to on every release (see `.goreleaser.yml`'s `brews:` section
and `.github/workflows/release.yml`). Neither goreleaser nor anything
running inside *this* repo can create that repo for you; the one-time
setup is:

1. Create an empty GitHub repo named exactly `homebrew-termdock` under
   the `padovanl` account.
2. Generate a fine-grained GitHub personal access token with
   `contents: write` scoped to just that one repo.
3. Add it as a secret named `HOMEBREW_TAP_GITHUB_TOKEN` on *this* repo
   (Settings → Secrets and variables → Actions).

Until both exist, `.github/workflows/release.yml`'s Homebrew publish
step fails the release run — drop the `brews:` section from
`.goreleaser.yml` first if you'd rather cut a release without a tap.

## 🔨 Build

```sh
go build -o termdock .
```

Requires Go 1.21+ and a POSIX system (Linux/macOS/WSL).

## 🚀 Usage

```sh
termdock                        # attach to (or create) the default "main" session
termdock new [-s NAME]          # create a new session and attach to it
termdock attach [-t NAME] [-r]  # attach to an existing session; -r = read-only
termdock ls                     # list active sessions
termdock kill-session -t NAME   # terminate a session (and all its panes)
```

Sessions survive the terminal closing: you can disconnect over SSH, close
the window, or press `Ctrl-B d`, and find everything exactly as you left
it with a plain `termdock attach`. They survive the *daemon* dying too —
see [💾 Session persistence](#-session-persistence).

### 👀 Read-only observer

`termdock attach -t NAME -r` attaches as a view-only observer: every
frame streams to it exactly like a normal client, but every keystroke and
mouse event it sends is dropped server-side before it ever reaches the
session — it can watch, not touch. Good for screen-sharing or pairing
without handing over control, or for looking in on a long-running job
without any risk of an accidental keypress landing in someone else's
shell. Any number of normal and read-only clients can attach to the same
session at once (that's what makes detach/reattach work in the first
place — the server was already built to broadcast to multiple clients).
An observer's own terminal size never resizes the shared session either,
so a smaller/larger window on their end doesn't disrupt anyone actually
working in it.

## ⌨️ Keys

Every command starts with the **`Ctrl-B` prefix**, followed by a key. Any
other key, when `Ctrl-B` wasn't just pressed, is forwarded straight to the
active pane. Every one of these is the *default* — see
[⌨️ Custom keybindings](#-custom-keybindings) below to rebind any of
them to a different key.

| Key after `Ctrl-B` | Action |
|---|---|
| `v` or `%` | split the active pane **vertically** (side by side) |
| `s` or `"` | split the active pane **horizontally** (stacked) |
| `←/→/↑/↓` or `h/j/k/l` | move focus to the adjacent pane — after an arrow, a **bare** arrow keeps moving without the prefix (see [⏩ Repeating a focus move](#-repeating-a-focus-move)) |
| `o` or `Tab` | cycle to the next pane |
| `z` | 🎯 zoom: the active pane fills the whole screen — its border turns magenta and its title gets a `[Z]` tag, so it's obvious you're zoomed (`z` again to undo) |
| `r` | **resize-mode**: subsequent arrows/hjkl resize the pane, any other key exits |
| `[` | enter **copy-mode** (scroll the scrollback, see below) |
| `]` | paste the most recently copied (yanked) text into the active pane |
| `=` | 📋 **paste register picker**: fuzzy-pick one of your last 20 yanks to paste (see below) |
| `y` | toggle **sync-panes**: keystrokes get sent to every pane in this window at once |
| `c` | create a new **window** (tab) |
| `n` / `p` | switch to the next / previous window |
| `w` | 🔍 **jump picker**: type to fuzzy-filter every window/pane, ↑↓/Tab to select, Enter to jump (see below) |
| `W` | ⏮️ toggle back to the **previously active window** (see below) |
| `;` | ⏮️ toggle back to the **previously active pane** in this window (see below) |
| `g` | 🖼️ **overview**: a live-thumbnail grid of every pane in the session (see below) |
| `/` | 🔎 **search**: search every pane's scrollback at once (see below) |
| `S` | 🔄 **switch session**: fuzzy-pick another session, no detach needed (see below) |
| `P` | 🪟 toggle a floating **popup terminal** over the current layout (see below) |
| `u` | 🔗 **open picker**: fuzzy-pick a URL/path spotted on screen, copies it to the clipboard (see below) |
| `!` | ✂️ **break-pane**: move the active pane out into its own new window |
| `Q` | 🔢 **quick-jump**: badge every pane with a number, press it to jump there (see below) |
| `:` | 💻 **command prompt**: type a command (`new-window`, `split-window`, ...; see below) |
| `Space` | 🧱 cycle the active window through **preset layouts** (see below) |
| `R` | 🔁 **respawn-pane**: restart the shell in the active pane, in place |
| `L` | 📝 toggle **logging** the active pane's output to a file (see below) |
| `0`-`9` | jump straight to window N |
| `,` | rename the current window |
| `&` | ⚠️ close the current window and every pane in it (asks `y`/`n` first) |
| `x` | close the active pane |
| `d` | **detach**: disconnect from the session, which keeps running in the background |
| `q` | ⚠️ quit termdock — asks `y`/`n` first (closes the whole session and every window) |
| `?` | ❓ open a scrollable keybinding reference (any key closes it) |
| `Ctrl-B` | double-press: sends a literal `Ctrl-B` to the active pane |

If a shell exits (e.g. with `exit`), its pane closes on its own; when a
window's last pane closes, the window itself closes; once the last window
closes, the session ends.

### 🪟 Windows

Each window is a fully independent set of panes and its own split layout
— like a browser tab, or a tmux window. The status bar shows the window
list on the left as a strip of colored tabs, e.g. ` 0:bash  1:vim!
2:htop* `: the accent color marks the one you're looking at, orange marks
one that produced output while you weren't (`*`/`!` are still there in
the text too, in case colors aren't your thing). Click a tab to switch to
it, the same as a browser tab, and 🖱️ drag a tab sideways to reorder the
strip. A window's name is automatically the foreground command of its
active pane, the same as pane titles, until you rename it with `Ctrl-B
,`. 🖱️ Drag a pane's *title bar* onto a different window's tab to move
that pane there instead — the pane's process is untouched, only which
window's layout it belongs to changes, the same way dragging a browser
tab out into another window doesn't reload the page.

### 🔍 Jump picker

`Ctrl-B w` opens a type-ahead picker listing every pane in every window of
the session — tmux splits this across `choose-window` and `choose-pane`,
each a static list you page through with j/k; termdock's is one list,
fuzzy-filtered as you type (characters just need to appear in order, not
consecutively — `depl` matches `1:deploy`), so getting to a specific pane
in a session with a dozen windows is a few keystrokes instead of hunting
through a list by eye. `↑`/`↓`/`Tab` move the selection, `Enter` jumps
straight to it — switching windows and focusing that exact pane in one
step — `Esc` cancels. The selected entry also gets a live preview box
next to the list — a **minimap** of that pane, the whole thing shrunk
down rather than a crop of one corner, drawn with braille glyphs as a
2×4 pixel grid per cell. A terminal can't shrink its font, so this is
what "the same pane, smaller" actually looks like: you see the shape of
what's in it — where the text sits, how long the lines run, how much has
scrolled — updating live as you move the selection, so you're never
jumping blind. With an empty query
(the moment it opens, or after clearing it) the list is ordered
most-recently-used first instead, so `Ctrl-B w` then `Enter` is a fast
"jump back to whatever I was just looking at," Alt-Tab style.

### ⏮️ Toggling back

`Ctrl-B W` flips back to whichever *window* was active right before the
one you're looking at now — tmux's own `Ctrl-B l`, moved to `W` here
since lowercase `l` is already "move focus right" (the vim-style hjkl
pane navigation above). `Ctrl-B ;` does the same one level down, for the
*pane* you were just on within the current window — tmux's own binding,
same key. Both flip back and forth like Alt-Tab: press again to return
to where you just were. Every deliberate window switch (`n`/`p`, a
number, the jump picker, search, the overview, a new window, break-pane)
updates what `W` jumps back to; every deliberate pane focus change
(`hjkl`/arrows, a click, the jump picker, quick-jump) updates what `;`
jumps back to. Both are simple no-ops if there's nothing recorded yet,
or the window/pane they'd jump to has since closed.

### 🖼️ Pane overview

`Ctrl-B g` replaces the screen with a grid of every pane in every window
of the session, each tile a small live preview of what's actually running
in it — nothing in tmux shows more than one pane's content at a time
outside of actually looking at it. Arrows/hjkl move the selection around
the grid (up/down jump a full row, not just one tile), `Enter` or a click
jumps straight to that pane, `Esc` cancels. Good for "where did I leave
that build running" when you've lost track across a dozen panes.

### 🔎 Search everywhere

`Ctrl-B /` searches every pane's scrollback — history and the live
screen — across every window, all at once. Copy-mode's own `/` (below)
only ever looks inside the one pane you're already in; this is for "did
I see that error somewhere, but I don't remember which pane." Type to
search — case-insensitive, live results as you type, your query tried
as a regex first and falling back to a literal substring if it doesn't
compile as one — `↑`/`↓` move between matches, `Enter` jumps straight to
the exact
matching line — switching to that window/pane and dropping you into
copy-mode with the cursor right on it — `Esc` cancels.

### 🔄 Switching sessions

`Ctrl-B S` opens a fuzzy picker over every *other* active session, so
jumping from one project's session to another doesn't mean detaching
first and reattaching by name — tmux needs `choose-tree` or a manual
detach/reattach round-trip to do the same. Type to filter, `↑`/`↓`
select, `Enter` switches (your terminal reconnects to the other daemon in
place — no flicker, no dropping back to the shell), `Esc` cancels.

### 📋 Paste registers

`Ctrl-B ]` alone always pastes your single most recent copy-mode yank, no
picker involved — unchanged, still the fast path. `Ctrl-B =` (tmux's
`choose-buffer`, same key) opens a fuzzy picker over your last 20 yanks
instead, each shown as a one-line preview, for when the thing you want
isn't the very last thing you copied.

### 🪟 Popup terminal

`Ctrl-B P` toggles a floating scratch terminal centered over whatever
you're currently looking at — 80% wide, 70% tall, no split, no layout
change, no window switch. It's a single persistent pane per session:
toggling it away and back keeps whatever was running (`git log`, a REPL,
a quick `man` page), the same way tmux's own `display-popup -E` does, but
without needing to write the command out every time or wire up a binding
yourself. `Ctrl-B P` again (or `Ctrl-B d`) closes it, clicking outside it
also closes it, and everything else you type goes straight through to the
shell inside it, exactly like a normal pane.

Set `popup-command` in the config (see [⚙️ Configuration](#-configuration))
to run a specific tool there instead of an interactive shell — `lazygit`,
`btop`, `ranger`, whatever you always reach for the popup to open in the
first place — without needing to script a `bind` around
`display-popup -E` the way tmux does. Unlike the default persistent
scratch shell, a `popup-command` popup closes itself the moment that
command exits (quit lazygit, the popup's gone), the same one-shot feel
stock tmux popups already have; toggling it back open runs the command
fresh again.

### 🔗 Opening links and paths

`Ctrl-B u` scans the active pane's visible screen for anything that looks
like a URL or a filesystem path and opens a fuzzy picker over the
matches, most recent (bottom of screen) first — handy after a build spews
a stack trace full of file paths, or `curl` prints a link you want
without reaching for the mouse. `Enter` copies the selected one to your
clipboard via the same OSC52 mechanism copy-mode yanks use (the daemon
may well be running on a different machine over SSH, so it can't just
exec a browser locally); `Esc` cancels. This is what tmux needs the
external `tmux-open`/`tmux-fpp` plugins for.

### 🔔 Background activity

The moment a background window's `*` activity marker first lights up
(see [🪟 Windows](#-windows)), termdock rings your real terminal's bell
(`\a`) once — not once per line, so a chatty background pane doesn't turn
into a siren, and not at all for the window you're already looking at.
What that bell actually *does* — flash the tab, bounce the dock icon,
play a sound — is entirely up to your terminal emulator's own bell
setting, the same as any other program's `\a`. tmux needs both
`monitor-activity` and `visual-activity` set explicitly to get anywhere
close to this.

### ✂️ Breaking a pane out

`Ctrl-B !` (tmux's own `break-pane`, same key) takes the active pane out
of its current split layout and gives it a brand new window all to
itself — the opposite of a split, for when a pane you added to a layout
on a whim turns out to deserve its own window instead. A no-op if it's
already alone in its window.

### 🔢 Quick-jump

`Ctrl-B Q` (tmux's own `display-panes`, moved off `q` since that's
already termdock's quit) badges every pane in the active window with a
number — 1 through 9 — right where its title bar shows it too. Press the
digit to jump straight there, any other key just dismisses the badges
with no effect. Good for a window with enough panes that hunting one
down with `Tab` gets tedious, without needing the full jump picker or
overview for something this local.

### 💻 Command prompt

`Ctrl-B :` (tmux's own binding, same key) opens a typed command line for
the same handful of actions the external scripting CLI exposes
(`new-window`, `split-window`, `select-window`, `rename-window`,
`send-keys`, `kill-pane`, `break-pane`, `respawn-pane`) — without
leaving the session, and without needing a `-t SESSION[:WINDOW]` target,
since a command typed here always means "the window I'm looking at right
now." `new-window` takes `-n NAME`; `split-window` takes `-v`/`-s`
(side-by-side/stacked, same convention as `Ctrl-B v`/`Ctrl-B s`);
`send-keys` takes a trailing `Enter` to submit, same as the CLI version.
Unknown commands or bad arguments show an error in the status line
instead of doing anything. `Esc` cancels without running anything typed
so far.

### 🧱 Preset layouts

`Ctrl-B Space` (tmux's own `next-layout`, same key) rebuilds the active
window's split into the next preset shape — **tiled** (a roughly square
grid), **even-columns**, **even-rows** — cycling back to the first after
the last, always in the panes' current left-to-right/top-to-bottom
order, so it never depends on how the split was originally built by
hand. Every pane's process is left completely alone; only the layout
around it moves, the same guarantee dragging a pane's title onto another
window's tab already gives. A no-op with only one pane, and blocked
while zoomed (exit zoom first) since there'd be nothing to lay out.

### 🔁 Respawning a pane

`Ctrl-B R` (tmux's `respawn-pane`) kills whatever's running in the active
pane and starts a fresh shell in exactly the same spot — same size, same
position in the split, only the process and its underlying pane ID
change. Handy when a shell's wedged, an SSH connection dropped, or a REPL
needs a clean restart, without tearing down and rebuilding the split
around it. Unlike tmux, there's no separate `-k` flag to force it: this
already replaces a still-running process without asking, the same
no-confirmation convention `Ctrl-B x` (close pane) already uses.

### 📝 Logging a pane

`Ctrl-B L` toggles capturing the active pane's raw output to a file —
termdock's built-in equivalent of the tmux-logging plugin (or a
hand-written `pipe-pane -o 'cat >>file'`), with no plugin to install and
no path to type. Logs land under
`$XDG_STATE_HOME/termdock/logs/SESSION-wN-pID-TIMESTAMP.log` (falling
back to `~/.local/state/termdock/logs/`), and the status line shows the
exact path the moment logging starts. A pane that's currently logging
gets a `[REC]` tag on its title bar, so it's never a surprise which one
is being captured. Logging follows the pane's *process*, not its spot in
the layout: it survives `Ctrl-B !` (break-pane) moving the pane to a new
window, but a `Ctrl-B R` respawn — which replaces the process outright —
starts the fresh shell with logging off, the same as any other new pane.

### ⌨️ Custom keybindings

Every keybinding in the [⌨️ Keys](#-keys) table above is a *default*,
not a fixed assignment: `bind <key> <action>` in the config (see
[⚙️ Configuration](#-configuration)) reassigns one key to a different
action, e.g. `bind M jump-picker` moves the jump picker from `w` to
`M` (both then trigger it, since `bind` only touches the one key you
name — `w` keeps its old meaning unless you rebind that too). The full
list of action names is in `internal/core/bindings.go`; the help screen
(`Ctrl-B ?`) and the prefix-held cheat-sheet both always reflect
whatever's currently bound, default or rebound, so neither one goes
stale the moment you customize something. Scoped to the top-level
`Ctrl-B`-prefixed commands only — copy-mode's own keys, the popup's, a
picker's type-ahead, and so on aren't rebindable. Arrow keys and `Tab`
are always-available alternates for movement/cycling regardless of any
rebind, the same way they were before this existed.

The digits `0`-`9` are "jump to window N" by default, but an explicit
`bind` on one wins: `bind 5 vsplit` really does make `Ctrl-B 5` split,
at the cost of no longer being able to jump straight to window 5. Only
digits you actually rebind are affected; the rest keep jumping.

### ⏩ Repeating a focus move

Walking three panes over shouldn't mean pressing the prefix three times.
After a prefixed arrow moves the focus, a **bare** arrow keeps moving it
for `repeat-time` milliseconds (default 1000, `repeat-time 0` disables) —
so it's `Ctrl-B ←←←`, not `Ctrl-B ← Ctrl-B ← Ctrl-B ←`. Each repeat
extends the window, so a steady walk never expires mid-way, and any
other command (or any other key) ends it immediately, so arrows are
never left hijacked.

This is tmux's `bind -r`/`repeat-time`, with one deliberate difference:
**only the arrow keys repeat, never `hjkl`**. tmux makes its `hjkl`
movement bindings repeatable too, which means a letter you type shortly
after switching panes can be swallowed as a movement instead. Arrows
aren't ordinary text, so restricting the repeat to them removes that
whole failure mode while keeping everything the feature is actually for.

### 🎯 Focus events

`focus-events on` in the config makes termdock forward a synthetic
terminal focus-in/focus-out sequence (`\x1b[I`/`\x1b[O`) to a pane
whenever you switch to/away from it — the same signal a real terminal
sends on Alt-Tab, which apps like neovim's `:checktime`-on-FocusGained
autoread react to, so switching back to a pane running an editor that
was modified elsewhere on disk notices right away. Off by default, same
as tmux's own `focus-events`. This covers termdock's own internal
pane/window switching only, not your terminal emulator itself gaining
or losing OS-level focus — see
[🚧 What's missing](#-whats-missing-on-purpose-for-now).

### ❓ Help

`Ctrl-B ?` opens a scrollable reference listing every keybinding —
`↑`/`↓`/`j`/`k`/`PgUp`/`PgDn`, `Home`/`End` and the mouse wheel scroll it,
any other key closes it. `PgUp`/`PgDn` move by a real screenful, whatever
your terminal's height happens to be. It's the
same floating box the jump picker uses, just without the type-ahead
filter, so a long list stays readable instead of getting crammed into
(and clipped off of) a single status bar line.

### 📋 Copy-mode (scrollback and copying)

Enter with `Ctrl-B [`. From there:

- `h/j/k/l` or arrows: move the cursor; `PgUp`/`PgDn`/`Ctrl-U`/`Ctrl-D`:
  page/half-page; `g`/`G`: top/bottom of the scrollback.
- `v`: start a character-wise selection from the cursor — an exact span,
  the same as click-dragging with the mouse.
- `V`: start a line-wise selection instead — every column of every line
  the selection spans, ignoring where exactly the cursor sits on the
  first/last one, vim's own visual-line mode. Good for grabbing a clean
  block of log lines without chasing column alignment. Pressing `v`
  while a `V` selection is active (or `V` while a `v` one is) switches
  modes in place, keeping the selection you already have, the same way
  vim's own `v`/`V` do inside visual mode; pressing the *same* key again
  exits the selection instead.
- `y` or `Enter`: copy the selection and exit copy-mode. The copied text
  is pushed to the real terminal via **OSC52**, so it lands in the system
  clipboard on terminals that support it (essentially every modern one:
  iTerm2, Windows Terminal, kitty, WezTerm, recent gnome-terminal...).
- `/`: search the scrollback (type the text, `Enter` jumps to the closest
  match searching forward). `n` / `N` repeat the last search forward /
  backward. Case-insensitive; your query is tried as a regular
  expression first (`err(or)?-\d+` works) and only falls back to a
  literal substring match if it doesn't compile as one — same as the
  global search below.
- `q` or `Esc`: exit without copying.

### 🖱️ Mouse

- Click a pane: gives it focus.
- Click-drag on a pane's content: selects text and copies it on release —
  no need to enter copy-mode first, the same as any ordinary terminal.
  Releasing also *leaves* copy-mode, exactly like pressing `y`, so the
  pane goes straight back to taking your typing. The status bar confirms
  what was copied. (A plain click with no drag still just focuses the
  pane; a drag over blank space copies nothing and leaves your clipboard
  alone.)
- Double-click a pane's title bar: 🎯 zooms it (same as `Ctrl-B z`);
  double-click again to unzoom.
- Drag a pane's title bar onto a different window's tab: moves that pane
  into that window (see [🪟 Windows](#-windows)).
- Drag the border between two panes, side by side or stacked: resizes
  them.
- Click a window tab in the status bar: switches to it. Drag it sideways
  to reorder the tab strip, the same as dragging a browser tab.
- Wheel: if the pane has scrollback, automatically enters copy-mode and
  scrolls; scrolling back to the bottom exits it automatically.
- Drag in copy-mode: selects text and copies it on release.

### 🎨 Pane titles and borders

Every pane is framed by a thin border, with its title embedded in the top
edge (e.g. `2:vim`) — the number is the pane's position within its window
(left-to-right, top-to-bottom), not an internal id, so it stays small and
predictable as you split and close panes. The title shows the foreground
command, not just the shell name, so you can tell what's running where at
a glance. (Linux only; elsewhere it always shows the shell name.) The
active pane's border is drawn in the accent color (`pane-active-bg`
below) so it's obvious which pane has focus. Every border, junction, and
corner is auto-tiled from the actual pane layout — there's no such thing
as a two-pane seam that doesn't line up.

### 📊 Status bar

The left side is minimal at rest (`Ctrl-B ?`) so there's room for the
right side: hostname and clock, tmux-style. Press the prefix and the left
side expands to the full key list for as long as you're mid-command;
`Ctrl-B ?` opens the same list as a proper scrollable screen instead (see
[❓ Help](#-help)), for when you want to actually read it rather than
catch it in passing. Turn on `status-segments` in the config (see
[⚙️ Configuration](#-configuration)) to prepend a git branch and/or
battery reading to the right side too, computed with a short cache so
they don't add overhead to every frame.

## 📜 Scripting a session

Beyond the interactive keys, termdock has a small command-line interface
for driving a session from outside — a setup script that opens a project
in a few pre-arranged windows, a Makefile target that tails a log in a
split pane, anything you'd otherwise have to click through by hand. This
is what `tmux send-keys`/`new-window`/`split-window`/... are for tmux.

`TARGET` is `SESSION[:WINDOW[.PANE]]` — e.g. `main`, `main:1`, `main:1.2`.
`WINDOW` is either its index (the number in the status bar's tab strip) or
its name; `PANE` is the number shown in that pane's own title bar, which is
also the `INDEX` column of `list-panes`. Omitting `WINDOW` means "the active
window"; omitting `PANE` means "that window's active pane". `select-pane`
and `list-panes` act on a whole window, so they ignore a `.PANE` part.

```sh
termdock send-keys -t TARGET text... [Enter]     # type text into a pane; trailing "Enter" submits it
termdock new-window -t SESSION [-n NAME] [cmd...] # new window, optionally running cmd instead of the shell
termdock split-window -t TARGET [-v|-s] [cmd...]  # -v side by side (default), -s stacked
termdock select-window -t SESSION:WINDOW          # make a window the visible one
termdock select-pane -t TARGET -L|-R|-U|-D        # move that window's focus one pane in a direction
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
`select-pane` in particular is the piece external tooling needs to move
focus by direction without an interactive client attached at all — the
building block a vim-tmux-navigator-style integration (seamless `hjkl`
between vim splits and termdock panes) would call into, the same way it
calls tmux's own `select-pane -L/-R/-U/-D` today.

### 📐 Small terminals

The layout degrades gracefully instead of breaking, the same way tmux
keeps shrinking panes rather than refusing to redraw: panes shrink
proportionally down to zero size if there truly isn't room, the outer
border/margin around the whole pane area is dropped first to give a very
small terminal every last row and column of real content, and the status
bar is the first thing to go on a single-row terminal. Nothing crashes at
any size; existing splits just get harder to see the smaller you go,
exactly like a real multiplexer.

### 💾 Session persistence

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

## ⚙️ Configuration

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
popup-command lazygit    # what Ctrl-B P runs (default: the shell, see below)
focus-events on          # forward synthetic pane focus-in/out (default off)
repeat-time 1000         # ms a bare arrow keeps moving focus (default 1000, 0 off)
bind M jump-picker        # rebind one key to a different action (repeatable)
theme dracula            # bundled color preset (default: none, see below)
status-bg black          # status bar background (default black)
status-fg silver         # status bar foreground (default silver)
pane-active-bg teal       # active pane's border/title color (default teal)
status-segments git,battery,cpu,mem  # extra segments in the status bar (default: none)
```

Colors accept any W3C name tcell understands, or `#rrggbb` hex.
`prefix`/`history-limit`/`shell`/`popup-command`/`focus-events`/`bind`/
`repeat-time`
are read by the **server**, so they take effect when a session is
*created* (`termdock new`), not on every attach; `mouse`, `theme`, and
the colors are read by the **client**, so they apply per attach and can
differ between two clients looking at the same session. `status-segments`
is a comma-separated list, read by the server: `git` shows the active
pane's current directory's branch (Linux only, empty outside a repo),
`battery` shows charge level and charging state, `cpu`/`mem` show overall
system usage read straight from `/proc/stat`/`/proc/meminfo` — all Linux
only, all cached for a couple of seconds (`cpu` also needs two samples an
interval apart to compute a delta from, so it shows nothing for the
first few seconds after being enabled) so nothing here adds real
overhead to every redraw. See
[⌨️ Custom keybindings](#-custom-keybindings) and
[🎯 Focus events](#-focus-events) above for `bind` and `focus-events`.

### 🎨 Themes

`theme <name>` sets `status-bg`/`status-fg`/`pane-active-bg` all at once
from a bundled, accurately-sourced palette built directly into
termdock — no plugin, no plugin manager, nothing to install: the same
convenience tmux users need an external plugin
(tmux-themepack/Catppuccin-for-tmux/dracula-tmux/nord-tmux) for. Built
in: `dracula`, `nord`, `gruvbox`, `catppuccin`, `solarized`,
`tokyo-night` (the demo screenshot above is running `theme dracula`). A
theme is always just a *baseline*: an explicit `status-bg`/`status-fg`/
`pane-active-bg` line in the same config still overrides just that one
color, regardless of whether it comes before or after the `theme` line —
so `theme nord` plus a single `pane-active-bg` tweak works exactly like
you'd expect. An unknown theme name is silently ignored, the same
leniency every other setting in `termdock.conf` already has, falling
back to the plain defaults.

### 🐞 Debugging input

Set `TERMDOCK_INPUT_LOG=/path/to/file` when starting the *server* (i.e.
on the first `termdock` invocation that creates the session) and every
key, mouse and resize event the daemon receives gets appended there,
along with the prefix/mode state it left behind:

```
02:47:54.598 key code=66 rune='\x00' mod=0    -> prefix=true mode=normal
02:47:54.598 key code=257 rune='\x00' mod=0   -> prefix=false mode=normal
```

Input problems in a multiplexer are otherwise very hard to pin down —
what your terminal emulator actually sends for a given chord, whether a
key reached the daemon at all, and whether the prefix was armed when it
did are all invisible from the outside, with the terminal, tcell, the
client and the server each a plausible culprit. Unset (the default) it
costs nothing.

## 📁 Code layout

- `main.go` — CLI: subcommands (`new`/`attach`/`ls`/`kill-session`) and
  starting the background daemon.
- `cli.go` — the scripting commands (`send-keys`/`new-window`/...).
- `internal/config` — the optional config file: prefix key, mouse, colors,
  bundled themes, scrollback size, shell, keybinding overrides, focus events.
- `internal/pane` — one pane: a shell process in a pty + the VT100
  emulator that interprets its output.
- `internal/vt10x` — the terminal emulator ([vt10x](https://github.com/hinshun/vt10x),
  vendored and extended with a scrollback buffer, since upstream doesn't
  have one).
- `internal/layout` — the binary split tree (vertical/horizontal) that
  computes each pane's on-screen rectangle, and interactive resizing.
- `internal/core` — the session's brain, with no terminal attached:
  windows, panes, layout, copy-mode (character- and line-wise selection,
  regex search), mouse, resize-mode, the jump picker,
  last-window/last-pane toggling, global search (regex), the pane
  overview, paste registers, session switching, the popup terminal, the
  URL/path opener, the activity bell, break-pane, status bar segments
  (including CPU/memory), quick-jump, the command prompt, preset
  layouts, respawn-pane, pane logging, rebindable keybindings
  (`bindings.go`), synthetic focus events (`focusevents.go`),
  confirm-before-quit, help screen. Runs in the server.
- `internal/proto` — the messages exchanged between client and server
  over the socket.
- `internal/persist` — the on-disk session-snapshot format and file I/O
  for crash/reboot recovery (see [💾 Session persistence](#-session-persistence)).
- `internal/server` — the daemon: owns a session, accepts clients over a
  unix socket, broadcasts a frame whenever something changes.
- `internal/client` — the UI: connects to the server, draws with
  [tcell](https://github.com/gdamore/tcell), forwards keys and mouse.

## 🧪 Testing

```sh
go build ./...   # everything compiles
go vet ./...     # static checks
go test ./...    # the actual test suite
```

Every package with logic worth pinning down has one: layout geometry and
the split-tree edge cases, the client's box-drawing/overlay/grid
rendering, pane process handling (including logging's raw pty tee — see
`internal/pane/pane_test.go`), the snapshot format, the config file
parser (including every bundled theme applying correctly and an
explicit color always beating a theme, in either line order — see
`internal/config/config_test.go`), the session brain in `internal/core` (mouse gesture
disambiguation — click vs. drag vs. double-click, all sharing the same
press/release events — every picker's fuzzy filter, window/pane
reordering and moving, the pane overview's grid math, global search,
the popup terminal's own lifecycle and keys (including a configured
`popup-command` and its own process exiting on its own), the URL/path
opener's regex matching, the activity bell's edge-triggering,
break-pane, status segments (git/battery/CPU/memory, including CPU's
two-samples-needed delta math), quick-jump's digit cap, the command
prompt's every verb, preset layouts' equal-share math, respawn-pane,
pane logging, last-window/last-pane toggling (including the dangling
pointer a closed or moved-elsewhere target would otherwise leave — see
`internal/core/lasttoggle_test.go`), copy-mode's character- vs.
line-wise selection and both search modes' regex-with-literal-fallback
(`internal/core/copymode_test.go`, `search_test.go`, `textsearch_test.go`),
the rebinding system (`bindings_test.go` — a rebound key routes to the
right action, an unknown action name is ignored, the help screen/cheat-sheet
stay accurate, arrows/Tab keep working regardless), synthetic focus
events (`focusevents_test.go`), select-pane's direction math including
targeting a *background* window (`cli_test.go`), and quit now asking for
confirmation the same way kill-window already did, from every path that
can reach it including the popup's own separate key handler
(`quit_test.go`), and end-to-end crash-recovery), and `internal/server`, with real
socket-level integration tests: two actual daemons for a session-switch
to jump between, a read-only client's input and resize both confirmed
dropped by driving a second, ordinary client and checking what it sees,
a background window's activity confirmed to ring a bell on an attached
client, and several rounds of new keybindings — including a config-driven
`bind` override and `select-pane` — driven end to end through one real
client connection. `internal/core/interop_test.go` specifically checks
features against *each other* — logging surviving a break-pane, a
respawn correctly *not* carrying logging over to the fresh process, the
command prompt's verbs matching their direct keybindings exactly —
rather than each in isolation. All of it spins up real shells in real
ptys and real unix-socket daemons rather than mocking the terminal or
network layer, and isolates every bit of state a run
touches — session-snapshot I/O (`$XDG_STATE_HOME`) and session sockets
(`$XDG_RUNTIME_DIR`) alike — to throwaway temp directories, so a test
run never touches or gets confused by your actual sessions.

CI (`.github/workflows/ci.yml`) runs build, vet, and the full test suite
on every push and pull request against `master`. A separate workflow
(`.github/workflows/release.yml`) runs the test suite once more and then
cuts a full release — see [📦 Installation](#-installation) — but is
currently manual-only while the project's under active development,
rather than firing automatically on every pushed version tag.

## 📄 License

[MIT](LICENSE) — see the LICENSE file.

## 🚧 What's missing (on purpose, for now)

Still not there, to keep the scope manageable: reflowing scrollback
lines to a new width on resize (they keep whatever width they were
written at), and real OS-level terminal focus forwarding —
[focus-events](#-focus-events) only covers termdock's own internal
pane/window switching, not the client detecting your terminal emulator
itself gaining/losing focus (Alt-Tab), which would need protocol
changes between the client and server this round didn't take on.
