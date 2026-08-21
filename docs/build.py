#!/usr/bin/env python3
"""Builds termdock's manual pages from the content in pages.py.

The landing page (index.html) is hand-written and stays that way: it is
a different job, and templating it would make it harder to fiddle with,
not easier. Everything else shares one shell — head, top bar, table of
contents, prev/next — because a manual whose pages drift apart in
navigation is a manual people stop trusting to be complete.

Run: python3 docs/build.py
"""

import html
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))

PAGE = """<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} — termdock</title>
<meta name="description" content="{description}">
<meta property="og:title" content="{title} — termdock">
<meta property="og:description" content="{description}">
<meta property="og:type" content="article">
<meta name="theme-color" content="#07100f">
<link rel="icon" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'><rect x='2.5' y='2.5' width='27' height='27' rx='3' fill='none' stroke='%2335e0c8' stroke-width='2.4'/><path d='M16 3.5 V28.5 M16 13 H28.5' stroke='%2335e0c8' stroke-width='2.4' stroke-linecap='round'/></svg>">
<link rel="stylesheet" href="style.css">
</head>
<body>
<div class="crt-veil" aria-hidden="true"></div>

<header class="topbar">
  <div class="topbar-inner">
    <a class="brand" href="index.html" style="text-decoration:none;border:0">termdock<span class="cursor">_</span></a>
    <nav class="navlinks">
{navlinks}
      <a href="https://github.com/padovanl/termdock">github</a>
    </nav>
  </div>
</header>

<main class="manual">
  <aside class="manual-toc">
    <h2>On this page</h2>
{toc}
  </aside>
  <div class="manual-body">
    <h1 style="margin:0 0 0.5rem;font-size:2rem">{title}</h1>
    <p class="page-lede">{lede}</p>
{body}
    <nav class="manual-nav">
      <span>{prev}</span>
      <span>{next}</span>
    </nav>
  </div>
</main>

<footer>
  <div class="wrap">
    <div class="foot-chips">
      <a class="foot-chip" href="index.html">Home</a>
      <a class="foot-chip" href="https://github.com/padovanl/termdock">GitHub</a>
      <a class="foot-chip" href="https://github.com/padovanl/termdock/releases">Releases</a>
      <a class="foot-chip" href="https://github.com/padovanl/termdock/issues">Issues</a>
    </div>
    <p class="foot-note">MIT licensed · written from scratch in Go · linux &amp; macOS</p>
  </div>
</footer>
</body>
</html>
"""


def slug(text):
    """The anchor for a heading, matching what a reader would guess."""
    text = re.sub(r"<[^>]+>", "", text).lower()
    text = re.sub(r"[^\w\s-]", "", text)
    return re.sub(r"\s+", "-", text.strip())


def build(pages):
    navlinks = "\n".join(
        '      <a href="{}">{}</a>'.format(p["file"], p["nav"]) for p in pages
    )

    for i, page in enumerate(pages):
        body = page["body"].strip()

        # Give every h2 an id, and build the table of contents from the
        # same pass — so the two can never disagree about what is on the
        # page, which is the usual way a hand-kept ToC rots.
        headings = []

        def anchor(match):
            text = match.group(1)
            headings.append(text)
            return '<h2 id="{}">{}</h2>'.format(slug(text), text)

        body = re.sub(r"<h2>(.*?)</h2>", anchor, body, flags=re.S)

        toc = "\n".join(
            '    <a href="#{}">{}</a>'.format(slug(h), re.sub(r"<[^>]+>", "", h))
            for h in headings
        )

        prev_link = ""
        if i > 0:
            prev_link = '← <a href="{}">{}</a>'.format(
                pages[i - 1]["file"], pages[i - 1]["title"]
            )
        next_link = ""
        if i < len(pages) - 1:
            next_link = '<a href="{}">{}</a> →'.format(
                pages[i + 1]["file"], pages[i + 1]["title"]
            )

        out = PAGE.format(
            title=html.escape(page["title"]),
            description=html.escape(page["description"]),
            lede=page["lede"],
            navlinks=navlinks,
            toc=toc,
            body=body,
            prev=prev_link,
            next=next_link,
        )
        path = os.path.join(HERE, page["file"])
        with open(path, "w", encoding="utf-8", newline="\n") as f:
            f.write(out)
        print("wrote", page["file"], "({} sections)".format(len(headings)))


if __name__ == "__main__":
    sys.path.insert(0, HERE)
    from pages import PAGES  # noqa: E402

    build(PAGES)
