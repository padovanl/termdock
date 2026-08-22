#!/usr/bin/env python3
"""Builds the printable keyboard cheatsheet.

Deliberately not part of the manual's template. A cheatsheet is a
different object: one page, no navigation, no prose, and it has to
survive being printed — so it carries its own tight layout and a print
stylesheet rather than inheriting a reading layout meant for scrolling.

The bindings are read out of internal/core/bindings.go rather than typed
here, so the sheet cannot drift from the program. Anything the source
does not know about (copy-mode keys, which are not in the binding table)
is listed separately and marked as such.

Run: python3 docs/cheatsheet.py
"""

import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)

# How the prefix-key actions are grouped on the sheet, by action name.
# Anything in bindings.go but not named here lands in "Everything else",
# so adding an action can never make it silently vanish from the sheet.
GROUPS = [
    ("Panes", [
        "vsplit", "hsplit", "focus-left", "focus-down", "focus-up", "focus-right",
        "cycle-focus", "last-pane", "zoom", "resize-mode", "close-pane",
        "reopen-pane", "break-pane", "respawn-pane", "rename-pane",
        "quick-jump", "cycle-layout",
    ]),
    ("Windows", [
        "new-window", "next-window", "prev-window", "last-window",
        "rename-window", "kill-window", "jump-picker", "overview",
    ]),
    ("Finding things", [
        "search", "copy-mode", "open-link", "command-history", "timeline",
        "copy-last-output",
    ]),
    ("Session", [
        "detach", "quit", "rename-session", "switch-session", "popup",
        "sync-panes", "settings", "command-prompt", "help",
    ]),
    ("Copying & logging", [
        "paste", "paste-picker", "toggle-logging", "log-window", "watch-done",
    ]),
]

# Copy-mode is a separate keymap: those keys are handled inside the mode
# and are not in the rebindable table, so they are written out here and
# flagged as fixed.
COPY_MODE = [
    ("h j k l / arrows", "Move the cursor"),
    ("PgUp PgDn Ctrl-U Ctrl-D", "Page and half-page"),
    ("g / G", "Top / bottom of the scrollback"),
    ("v", "Start a character-wise selection"),
    ("V", "Whole-line selection (v switches back)"),
    ("y", "Yank to clipboard and paste registers"),
    ("/", "Search this pane (regex, else literal)"),
    ("n / N", "Next / previous match"),
    ("{ }", "Previous / next command *"),
    ("q or Esc", "Leave copy-mode"),
]

CLI = [
    ("termdock", "Attach to (or create) the session named main"),
    ("termdock new -s NAME", "Create a session and attach"),
    ("termdock attach -t NAME [-r]", "Attach; -r is read-only"),
    ("termdock ls", "List running sessions"),
    ("termdock kill-session -t NAME", "End a session"),
    ("termdock themes", "List the bundled colour themes"),
    ("termdock doctor", "Check for settings failing silently"),
    ("termdock shell-init [SHELL]", "Print the shell snippet for command marks *"),
    ("termdock layout save|apply|ls|rm", "Named window arrangements"),
    ("termdock send-keys -t TARGET ...", "Type into a pane from a script"),
    ("termdock split-window -t TARGET", "Split from a script"),
    ("termdock select-pane -t TARGET -L|-R|-U|-D", "Move focus from a script"),
    ("termdock list-windows / list-panes -t T", "Inspect a session"),
]


def read_bindings():
    """(action name -> [keys]) and (action -> description) from the source."""
    src = open(os.path.join(ROOT, "internal", "core", "bindings.go"), newline="").read()

    consts = dict(re.findall(r"\t(act\w+)\s+action = \"([^\"]+)\"", src))
    keys = {}
    for key, const in re.findall(r"\t'(.)': (act\w+),", src):
        name = consts.get(const)
        if not name:
            continue
        keys.setdefault(name, []).append("Space" if key == " " else key)

    # The map body, taken from its declaration rather than by splitting on
    # the name: the name also appears in the doc comment above it, and
    # str.split returns every segment — so slicing between the first two
    # occurrences yielded 78 characters of comment and no entries at all,
    # leaving every row on the sheet labelled with its action name.
    m = re.search(
        r"var actionDescriptions = map\[action\]string\{(.*?)\n\}", src, re.S
    )
    if not m:
        raise SystemExit("cannot find actionDescriptions in bindings.go")
    descs = {
        consts[c]: d
        for c, d in re.findall(r"(act\w+):\s+\"([^\"]+)\",", m.group(1))
        if c in consts
    }
    missing = sorted(set(consts.values()) - set(descs))
    if missing:
        raise SystemExit("no description for: " + ", ".join(missing))
    return keys, descs


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def rows(pairs):
    return "\n".join(
        '<tr><td class="k">{}</td><td>{}</td></tr>'.format(esc(k), esc(v))
        for k, v in pairs
    )


def build():
    keys, descs = read_bindings()
    placed = {name for _, names in GROUPS for name in names}
    leftover = sorted(set(keys) - placed)

    groups = list(GROUPS)
    if leftover:
        groups.append(("Everything else", leftover))

    blocks = []
    for title, names in groups:
        pairs = []
        for name in names:
            if name not in keys:
                continue
            combo = " ".join(sorted(keys[name], key=lambda k: (len(k), k)))
            pairs.append((combo, descs.get(name, name)))
        if pairs:
            blocks.append(
                '<section class="cs-block"><h2>{}</h2><table>{}</table></section>'.format(
                    esc(title), rows(pairs)
                )
            )

    blocks.append(
        '<section class="cs-block"><h2>Copy-mode <span class="cs-note">(no prefix, fixed keys)</span></h2>'
        "<table>{}</table></section>".format(rows(COPY_MODE))
    )
    blocks.append(
        '<section class="cs-block cs-wide"><h2>From the shell</h2><table>{}</table></section>'.format(
            rows(CLI)
        )
    )

    covered = sum(len(v) for v in keys.values())
    out = TEMPLATE.replace("{{BLOCKS}}", "\n".join(blocks)).replace(
        "{{COUNT}}", str(covered)
    )
    path = os.path.join(HERE, "cheatsheet.html")
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(out)
    print("wrote cheatsheet.html ({} keys, {} blocks)".format(covered, len(blocks)))


TEMPLATE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>termdock cheatsheet</title>
<meta name="description" content="Every termdock keybinding on one printable page: panes, windows, copy-mode and the command line.">
<link rel="icon" href="logo.svg" type="image/svg+xml">
<link rel="stylesheet" href="style.css">
<style>
  /* A cheatsheet is its own object: one page, dense, and it has to
     survive being printed. Hence its own layout rather than the
     manual's reading column. */
  .cs-page { max-width: 68rem; margin: 0 auto; padding: 2rem 1.25rem 3rem; }
  .cs-head { display: flex; align-items: baseline; gap: 1rem; flex-wrap: wrap;
             border-bottom: 1px solid var(--hairline); padding-bottom: 1rem; margin-bottom: 1.5rem; }
  .cs-head h1 { font-family: var(--serif); font-size: 2rem; margin: 0; }
  .cs-head .cs-sub { color: var(--rust); font-family: var(--mono); font-size: .82rem; }
  .cs-print { margin-left: auto; font-family: var(--mono); font-size: .8rem;
              border: 1px solid var(--hairline); border-radius: 6px; padding: .35rem .7rem;
              color: var(--phosphor); cursor: pointer; background: none; }
  .cs-grid { columns: 3 20rem; column-gap: 1.75rem; }
  .cs-block { break-inside: avoid; margin: 0 0 1.4rem; }
  .cs-block.cs-wide { column-span: all; }
  .cs-block h2 { font-family: var(--serif); font-size: 1.05rem; color: var(--phosphor);
                 margin: 0 0 .4rem; letter-spacing: 0; }
  .cs-block h2::before { content: ""; }
  .cs-note { font-family: var(--mono); font-size: .7rem; color: var(--rust); }
  .cs-block table { width: 100%; border-collapse: collapse; font-size: .84rem; }
  .cs-block td { padding: .18rem .4rem .18rem 0; vertical-align: top;
                 border-bottom: 1px solid var(--hairline-soft); }
  .cs-block td.k { font-family: var(--mono); color: var(--phosphor-dim);
                   white-space: nowrap; width: 1%; padding-right: .8rem; }
  .cs-foot { margin-top: 1.5rem; border-top: 1px solid var(--hairline); padding-top: 1rem;
             font-size: .82rem; color: var(--rust); }

  @media print {
    /* Printed on paper the CRT palette is unreadable and wastes ink, so
       the sheet inverts to black on white and drops the chrome. */
    .crt-veil, .topbar, footer, .cs-print { display: none !important; }
    body { background: #fff !important; color: #111 !important; font-size: 10pt; }
    .cs-page { max-width: none; padding: 0; }
    .cs-grid { columns: 3 15rem; column-gap: 1.2rem; }
    .cs-head h1, .cs-block h2 { color: #000 !important; }
    .cs-block td.k { color: #333 !important; }
    .cs-block td { border-bottom: 1px solid #ddd !important; }
    .cs-head, .cs-foot { border-color: #999 !important; }
    a { color: #000 !important; text-decoration: none; }
    @page { margin: 12mm; }
  }
</style>
</head>
<body>
<div class="crt-veil" aria-hidden="true"></div>

<header class="topbar">
  <div class="topbar-inner">
    <a class="brand" href="index.html" style="text-decoration:none;border:0"><img src="logo.svg" alt="" width="18" height="18" style="vertical-align:-3px;margin-right:.4rem">termdock<span class="cursor">_</span></a>
    <nav class="navlinks">
      <a href="getting-started.html">start</a>
      <a href="keys.html">keys</a>
      <a href="shell-integration.html">shell</a>
      <a href="workflow.html">workflow</a>
      <a href="configuration.html">config</a>
      <a href="remote.html">remote</a>
      <a href="https://github.com/padovanl/termdock">github</a>
    </nav>
  </div>
</header>

<main class="cs-page">
  <div class="cs-head">
    <h1>termdock cheatsheet</h1>
    <span class="cs-sub">every key starts with <strong>Ctrl-B</strong> unless noted</span>
    <button class="cs-print" onclick="window.print()">Print / save as PDF</button>
  </div>

  <div class="cs-grid">
{{BLOCKS}}
  </div>

  <p class="cs-foot">
    {{COUNT}} bindings, read straight from the source so this sheet cannot
    drift from the program. Every one of them is rebindable — see
    <a href="keys.html">Every key</a>. Lines marked <strong>*</strong> need
    <a href="shell-integration.html">shell integration</a>.
    Inside termdock, <span class="k">Ctrl-B</span> <span class="k">?</span>
    shows the same list generated from what you actually have bound.
  </p>
</main>

<footer>
  <div class="wrap">
    <div class="foot-chips">
      <a class="foot-chip" href="index.html">Home</a>
      <a class="foot-chip" href="getting-started.html">Manual</a>
      <a class="foot-chip" href="https://github.com/padovanl/termdock">GitHub</a>
    </div>
    <p class="foot-note">MIT licensed · written from scratch in Go · linux &amp; macOS</p>
  </div>
</footer>
</body>
</html>
"""

if __name__ == "__main__":
    sys.exit(build())
