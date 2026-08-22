"""Content for termdock's manual pages. Rendered by build.py.

Kept apart from the template so writing a page is writing prose, not
fighting HTML boilerplate — and so a change to the page shell is one
edit rather than six.

Each page is a dict: file, nav (the top-bar label), title, description
(for search results and link previews), lede, and body. The body's <h2>s
become the table of contents automatically, so there is no second list
to keep in step.
"""

GETTING_STARTED = {
    "file": "getting-started.html",
    "nav": "start",
    "title": "Getting started",
    "description": "Install termdock, create your first session, split it into panes, and understand what detaching actually does.",
    "lede": "Ten minutes from installing to knowing why a session survives closing the terminal.",
    "body": """
<figure class="manual-figure">
<img src="demo.gif" width="1920" height="1080" loading="lazy"
     alt="A termdock session: three named panes, a failed command's exit status shown on its pane title, the session retheme'd live from a command prompt, the settings screen, every pane previewed at once, the jump picker's live minimap, every command the session has run with how it ended, the same commands on a shared timeline, zooming one pane, closing a pane and taking it back, then detaching with the work still running">
<figcaption>The whole of this page, in about forty seconds. Each of these
is covered below or in <a href="workflow.html">Working in a session</a>.</figcaption>
</figure>

<h2>Install</h2>

<p>Prebuilt packages and binaries are on the
<a href="https://github.com/padovanl/termdock/releases">releases page</a>
for linux and macOS, amd64 and arm64.</p>

<pre><code># Debian / Ubuntu
sudo dpkg -i termdock_*_amd64.deb

# Fedora / RHEL
sudo rpm -i termdock-*.x86_64.rpm

# Homebrew (macOS or Linux)
brew install padovanl/termdock/termdock

# From source, with Go 1.21+
go install github.com/padovanl/termdock@latest</code></pre>

<p>Check it landed:</p>

<pre><code>termdock --version</code></pre>

<h2>Your first session</h2>

<p>Run <code>termdock</code> with no arguments. It attaches to a session
called <code>main</code>, creating it if it isn't there yet. You get one
pane running your shell, and a status bar along the bottom.</p>

<p>Everything termdock does starts with the <strong>prefix key</strong>,
<span class="k">Ctrl-B</span>, followed by another key. Any key you press
without the prefix goes straight to the shell, exactly as if termdock
weren't there.</p>

<div class="note">
<strong>If you forget everything else, remember two.</strong>
<span class="k">Ctrl-B</span> <span class="k">?</span> opens a scrollable
list of every key, always generated from what is <em>actually</em> bound
right now — so it stays right even if you rebind things.
<span class="k">Ctrl-B</span> <span class="k">d</span> detaches.
</div>

<h2>Splitting into panes</h2>

<pre><code>Ctrl-B v     split side by side
Ctrl-B s     split top and bottom
Ctrl-B x     close the pane you're in
Ctrl-B z     zoom the pane to fill the screen (again to undo)</code></pre>

<p>Move between them with <span class="k">Ctrl-B</span> and an arrow key,
or <code>h</code> <code>j</code> <code>k</code> <code>l</code> if you
think in vim. Moving is the one thing you do repeatedly, so after a
prefixed arrow a <em>bare</em> arrow keeps moving for a second — crossing
three panes is <span class="k">Ctrl-B</span> <span class="k">←</span>
<span class="k">←</span> <span class="k">←</span>, not the prefix three
times.</p>

<p>Closed the wrong one? <span class="k">Ctrl-B</span>
<span class="k">Z</span> brings it back, in the window it came from and
the directory it was sitting in.</p>

<h2>Windows are tabs</h2>

<p>A window is a whole separate set of panes, like a browser tab. The
strip along the bottom left is the window list.</p>

<pre><code>Ctrl-B c     new window
Ctrl-B n / p next / previous window
Ctrl-B 0-9   jump straight to window N
Ctrl-B ,     rename this window
Ctrl-B &amp;     close the window and everything in it (asks first)</code></pre>

<p>Once you have more than a few, stop counting and start searching:
<span class="k">Ctrl-B</span> <span class="k">w</span> opens a
type-ahead list of every pane in every window, with a live minimap of
the one you're about to jump to.</p>

<h2>Detaching: the whole point</h2>

<p>Press <span class="k">Ctrl-B</span> <span class="k">d</span>. You are
back at your ordinary shell, and termdock says:</p>

<pre><code>termdock: ▦ detached from "main" — it is still running; reattach with: termdock attach -t "main"</code></pre>

<p>Nothing stopped. The panes, the shells, the program halfway through
compiling — all of it is still running inside a daemon that is not
attached to your terminal any more. Close the terminal window, log out,
lose your SSH connection: it makes no difference to the session.</p>

<pre><code>termdock ls                    # what's running
termdock attach -t main        # pick up exactly where you left off
termdock kill-session -t main  # actually end it</code></pre>

<p>This is what a multiplexer is <em>for</em>, and it is worth trying
deliberately once — detach, close the terminal entirely, open a new one,
attach — because until you have seen it, every other feature reads as
convenience rather than as the point.</p>

<h2>Where to go next</h2>

<ul>
<li><a href="keys.html">Every key</a> — the full reference.</li>
<li><a href="shell-integration.html">Shell integration</a> — one line in
your shell rc that unlocks command history, jumping between commands,
and copying a command's whole output. Start here if you only read one
more page.</li>
<li><a href="workflow.html">Working in it all day</a> — the features
that earn their keep once a session has been open for a week.</li>
<li><a href="configuration.html">Configuration</a> — themes, keys,
settings you can change while it runs.</li>
</ul>
""",
}

KEYS = {
    "file": "keys.html",
    "nav": "keys",
    "title": "Every key",
    "description": "The complete termdock keybinding reference: panes, windows, copy-mode, and everything behind the Ctrl-B prefix.",
    "lede": "Everything behind the prefix. All of it rebindable, and all of it also on the Ctrl-B ? screen inside termdock — which is generated from what's actually bound, so it never goes stale.",
    "body": """
<h2>Panes</h2>

<table>
<tr><th>Key</th><th>Does</th></tr>
<tr><td><code>v</code> or <code>%</code></td><td>Split side by side.</td></tr>
<tr><td><code>s</code> or <code>"</code></td><td>Split top and bottom.</td></tr>
<tr><td><code>←→↑↓</code> / <code>hjkl</code></td><td>Move focus. After a prefixed arrow, a bare arrow keeps moving for a second.</td></tr>
<tr><td><code>o</code> or <code>Tab</code></td><td>Cycle to the next pane.</td></tr>
<tr><td><code>;</code></td><td>Back to the pane you were just in.</td></tr>
<tr><td><code>z</code></td><td>Zoom this pane full-screen. Its border turns magenta and the title gains <code>[Z]</code>.</td></tr>
<tr><td><code>r</code></td><td>Resize mode: arrows resize, any other key leaves.</td></tr>
<tr><td><code>x</code></td><td>Close this pane.</td></tr>
<tr><td><code>Z</code></td><td>Reopen the last closed pane, in its window and directory.</td></tr>
<tr><td><code>!</code></td><td>Break this pane out into a window of its own.</td></tr>
<tr><td><code>R</code></td><td>Respawn: restart the shell in place.</td></tr>
<tr><td><code>.</code></td><td>Name this pane. Empty clears it.</td></tr>
<tr><td><code>Q</code></td><td>Badge every pane with a digit; press one to jump there.</td></tr>
<tr><td><code>Space</code></td><td>Cycle the window through tiled / even-columns / even-rows.</td></tr>
</table>

<h2>Windows</h2>

<table>
<tr><th>Key</th><th>Does</th></tr>
<tr><td><code>c</code></td><td>New window.</td></tr>
<tr><td><code>n</code> / <code>p</code></td><td>Next / previous window.</td></tr>
<tr><td><code>0</code>–<code>9</code></td><td>Jump to window N.</td></tr>
<tr><td><code>W</code></td><td>Back to the window you were just in.</td></tr>
<tr><td><code>,</code></td><td>Rename this window.</td></tr>
<tr><td><code>&amp;</code></td><td>Close the window and every pane in it. Asks first.</td></tr>
<tr><td><code>w</code></td><td>Jump picker: type to filter every window and pane, with a live minimap.</td></tr>
<tr><td><code>g</code></td><td>Overview: every pane in the session as a grid of live thumbnails.</td></tr>
</table>

<h2>Finding things</h2>

<table>
<tr><th>Key</th><th>Does</th></tr>
<tr><td><code>/</code></td><td>Search every pane's scrollback at once, plain text or regex.</td></tr>
<tr><td><code>[</code></td><td>Copy-mode: scroll and select the scrollback.</td></tr>
<tr><td><code>u</code></td><td>Pick a URL or path visible on screen; copies it to your clipboard.</td></tr>
<tr><td><code>H</code></td><td>Every command run in this session, fuzzy-searchable. <a href="shell-integration.html">Needs shell integration.</a></td></tr>
<tr><td><code>T</code></td><td>Session timeline: when each command ran, for how long. <a href="shell-integration.html">Needs shell integration.</a></td></tr>
<tr><td><code>O</code></td><td>Copy a command's entire output. <a href="shell-integration.html">Needs shell integration.</a></td></tr>
</table>

<h2>Copy-mode</h2>

<p>Enter with <span class="k">Ctrl-B</span> <span class="k">[</span>.
Inside, the prefix is not needed:</p>

<table>
<tr><th>Key</th><th>Does</th></tr>
<tr><td><code>hjkl</code> / arrows</td><td>Move the cursor.</td></tr>
<tr><td><code>PgUp</code> / <code>PgDn</code> / <code>Ctrl-U</code> / <code>Ctrl-D</code></td><td>Page and half-page.</td></tr>
<tr><td><code>g</code> / <code>G</code></td><td>Top / bottom of the scrollback.</td></tr>
<tr><td><code>v</code></td><td>Start a character-wise selection.</td></tr>
<tr><td><code>V</code></td><td>Whole-line selection. <code>v</code> switches back without losing it.</td></tr>
<tr><td><code>y</code></td><td>Yank the selection, to the clipboard and the paste registers.</td></tr>
<tr><td><code>/</code></td><td>Search this pane. Regex, falling back to a literal match.</td></tr>
<tr><td><code>{</code> / <code>}</code></td><td>Jump to the previous / next command. <a href="shell-integration.html">Needs shell integration.</a></td></tr>
<tr><td><code>q</code> or <code>Esc</code></td><td>Leave.</td></tr>
</table>

<h2>Session and everything else</h2>

<table>
<tr><th>Key</th><th>Does</th></tr>
<tr><td><code>d</code></td><td>Detach. The session keeps running.</td></tr>
<tr><td><code>q</code></td><td>Quit the whole session. Asks first.</td></tr>
<tr><td><code>$</code></td><td>Rename this session — socket and snapshot move with it.</td></tr>
<tr><td><code>S</code></td><td>Switch to another session without detaching.</td></tr>
<tr><td><code>P</code></td><td>Toggle the floating scratch terminal.</td></tr>
<tr><td><code>y</code></td><td>Synchronised input: type into every pane at once, or the ones you picked.</td></tr>
<tr><td><code>]</code> / <code>=</code></td><td>Paste the last yank / pick from the last 20.</td></tr>
<tr><td><code>L</code></td><td>Log this pane's output to a file.</td></tr>
<tr><td><code>A</code></td><td>Log <em>every</em> pane in this window to a directory you name.</td></tr>
<tr><td><code>m</code></td><td>Tell me when this pane's command finishes.</td></tr>
<tr><td><code>C</code></td><td>Settings screen: every setting, changeable live.</td></tr>
<tr><td><code>:</code></td><td>Command prompt.</td></tr>
<tr><td><code>?</code></td><td>This list, inside termdock.</td></tr>
<tr><td><code>Ctrl-B</code></td><td>Pressed twice: sends a literal Ctrl-B to the shell.</td></tr>
</table>

<h2>Rebinding</h2>

<p>Every key above is a default, not a fixture. One line in the config:</p>

<pre><code>bind M jump-picker</code></pre>

<p>Both <code>w</code> and <code>M</code> now open it —
<code>bind</code> touches only the key you name. Action names are listed
by the help screen; the full set lives in
<code>internal/core/bindings.go</code>.</p>

<div class="note">
<strong>Digits and arrows.</strong> <code>0</code>–<code>9</code> jump to
a window unless you deliberately rebind that digit, in which case your
binding wins. Arrow keys always work for movement whatever else you
bind, so a rebind can never leave you unable to move.
</div>
""",
}

SHELL = {
    "file": "shell-integration.html",
    "nav": "shell",
    "title": "Shell integration",
    "description": "Teach your shell to mark prompts (OSC 133) and termdock learns where every command starts, ends and how it went — enabling command history, jumping between commands, and copying one command's whole output.",
    "lede": "One line in your shell rc. It is the single highest-value thing you can configure, and it unlocks four features nothing else can offer.",
    "body": """
<h2>The problem it solves</h2>

<p>Every terminal has the same blind spot. It receives one long stream of
characters and has no idea which of them are your prompt, which are the
command you typed, and which are that command's output. It is all just
text arriving.</p>

<p>That is why no multiplexer offers "jump back to the previous command"
or "copy that command's output" — not because nobody thought of it, but
because the information genuinely is not there to act on.</p>

<p><strong>OSC 133</strong> is the fix the terminal world settled on: the
shell announces the boundaries as it goes. Four invisible, zero-width
markers per command — prompt starts, prompt ends, command started
running, command finished with its exit status.</p>

<p>termdock records them in <em>its own</em> VT emulator, which is why
this works over SSH, in any terminal, whether or not the terminal you are
sitting at has ever heard of OSC 133. tmux cannot do this at any price:
it has no emulator of its own to record them in.</p>

<h2>Turning it on</h2>

<p>Add one line to your shell's startup file:</p>

<pre><code># ~/.bashrc, ~/.zshrc, or ~/.config/fish/config.fish
eval "$(termdock shell-init)"</code></pre>

<p><code>termdock shell-init</code> detects your shell from
<code>$SHELL</code>; pass <code>bash</code>, <code>zsh</code> or
<code>fish</code> to be explicit. It <strong>prints</strong> the snippet
rather than installing it — that file is yours and you should read what
goes into it. To just look:</p>

<pre><code>termdock shell-init bash | less</code></pre>

<p>Open a new pane afterwards; shells already running won't pick it up.
Nothing about your prompt changes visually.</p>

<div class="note">
<strong>If your prompt comes from a theme</strong> — oh-my-zsh,
powerlevel10k, starship — put the <code>eval</code> line <em>after</em>
that theme's setup, or the theme will overwrite the marker termdock
appends to <code>PS1</code>. <code>termdock doctor</code> will tell you
whether it found the line at all.
</div>

<h2>A worked example</h2>

<p>You run a test suite. It fails somewhere in three hundred lines of
output. You then run four more commands while poking at it, and now the
failure is far up the scrollback:</p>

<pre><code>$ go test ./...          ← 300 lines of output, somewhere up there
$ git status
$ vim internal/core/foo.go
$ git diff
$ go build ./...
$                        ← you are here</code></pre>

<p><strong>Without</strong> shell integration, retrieving that failure
means entering copy-mode, scrolling up by eye past four commands, finding
where the test run started, guessing where it ended, drag-selecting
several screens of text, and hoping you didn't clip the first line.</p>

<p><strong>With it:</strong></p>

<table>
<tr><th>You press</th><th>What happens</th></tr>
<tr><td><code>Ctrl-B [</code> then <code>{</code></td><td>Jumps to the prompt of <code>go build</code>.</td></tr>
<tr><td><code>{ { { {</code></td><td>Four more jumps, one per command, landing on <code>go test ./...</code>.</td></tr>
<tr><td><code>Ctrl-B O</code></td><td>The <strong>entire</strong> output of that run — all 300 lines — is on your clipboard.</td></tr>
</table>

<h2>What you get</h2>

<h3>Move by command, not by line</h3>

<p><code>{</code> and <code>}</code> in copy-mode jump to the previous
and next command. Each jump lands on a prompt with that command's output
filling the screen below it — which is the thing you were scrolling to
find. Repeated presses walk back through your history one command at a
time.</p>

<h3>Copy a command's whole output</h3>

<p><span class="k">Ctrl-B</span> <span class="k">O</span>. Not "roughly
this screenful", not "what's currently visible" — exactly the lines
between where that command started printing and where it stopped,
whether that is 2 lines or 3000, with the prompt and the command itself
excluded.</p>

<p><em>Which</em> command follows where you are looking: in copy-mode it
is the one your cursor sits in, so walking back with <code>{</code> and
then copying does what it plainly looks like it should. At a live prompt
it is the most recent. Pressed while a build is still running, you get
everything it has printed so far.</p>

<h3>A verdict on every pane</h3>

<p>A pane whose last command failed says so in its title:</p>

<pre><code> 2:go [✗1 47s]        ← exited 1, took 47 seconds
 3:npm                ← last command succeeded, quickly: nothing added</code></pre>

<p>The exit status appears only on failure, the duration only when the
command ran longer than a few seconds. A title is no place for noise,
and <code>✗</code> is the thing worth catching out of the corner of your
eye when you glance at a pane you left running.</p>

<h3>History and timeline</h3>

<p>Both of these become possible, and they get
<a href="workflow.html">their own section</a>:
<span class="k">Ctrl-B</span> <span class="k">H</span> searches every
command the session has run, and <span class="k">Ctrl-B</span>
<span class="k">T</span> draws them on a timeline.</p>

<h2>Without it</h2>

<p>Nothing breaks. There are simply no marks, and each feature says so
and points at the fix rather than silently doing nothing:</p>

<pre><code>no command marks in this pane — run `termdock shell-init` for the shell snippet</code></pre>
""",
}

WORKFLOW = {
    "file": "workflow.html",
    "nav": "workflow",
    "title": "Working in it all day",
    "description": "Command history across panes, a session timeline, saved layouts, logging a whole window, selective broadcast — the features that earn their keep once a session has been open for a week.",
    "lede": "The parts that stop mattering in a demo and start mattering on the third day of the same session.",
    "body": """
<h2>Search every command you have run</h2>

<p><span class="k">Ctrl-B</span> <span class="k">H</span> fuzzy-searches
every command run in <em>any</em> pane of the session, newest first, with
how each one exited and how long it took:</p>

<pre><code>go test ./...                            ✗1  2m14s  0:api › 1
kubectl rollout status deploy/web                   1:ops › 2
docker compose up -d                          8s    1:ops › 1</code></pre>

<p>Type to filter. <span class="k">Enter</span> <strong>types it into
the current pane</strong> without running it — the list is full of
things that already happened, some of which failed, and firing one
straight off a fuzzy match is how the wrong directory gets deleted.
Repeats collapse to a single entry.</p>

<p>Your shell's own history cannot be this, for three separate reasons:
it is per-shell, so what you ran in the pane next door is invisible; it
records what you typed but not what happened, so the command that worked
looks identical to the three attempts before it; and it is written when
that shell exits, so a pane still open has contributed nothing.</p>

<p>Needs <a href="shell-integration.html">shell integration</a>.</p>

<h2>See when everything ran</h2>

<p><span class="k">Ctrl-B</span> <span class="k">T</span> draws every
command on one shared time scale, oldest first:</p>

<pre><code>23:46:40  go build ./...   ██████████··················  31ms  0:api › 1
23:46:40  go test ./...    ·········███████████████████  60ms  ✗1  0:api › 1
23:46:40  tail -f app.log  ···························█  running  1:ops › 1</code></pre>

<p>It answers the question you ask <em>after</em> something has gone
wrong, when the evidence is spread across four panes' scrollback: was the
build still running when I started the migration? Because every bar is
drawn on the same scale, overlapping work reads as overlap. Commands
still going are included and marked.</p>

<p>Needs <a href="shell-integration.html">shell integration</a>.</p>

<h2>Tell me when this finishes</h2>

<p><span class="k">Ctrl-B</span> <span class="k">m</span> marks a pane.
The moment whatever it is running exits and falls back to a bare prompt,
you get a terminal bell and a message naming the pane. An armed pane
wears a <code>[⏳]</code> tag on its title; pressing <code>m</code> again
takes it back.</p>

<p>Two things make this better than appending
<code>; notify-send done</code>. It needs <strong>no shell
configuration</strong> — termdock asks the pty which process group holds
the foreground, so "busy" is that name not being your shell's. And it can
be armed <strong>after</strong> the command is already running, which a
wrapper fundamentally cannot: you almost never know in advance that this
is the run that will take twenty minutes.</p>

<h2>Name your panes</h2>

<p>Six panes all called <code>bash</code> tell you nothing.
<span class="k">Ctrl-B</span> <span class="k">.</span> names one. The
name wins over the process name, appears in the jump picker so the pane
becomes findable by it, and is saved with the session so a crash does not
undo it. Confirming an empty prompt clears it.</p>

<h2>Log a whole window at once</h2>

<p><span class="k">Ctrl-B</span> <span class="k">A</span> asks for a
directory and logs <strong>every pane in the window</strong>, one file
each. Press it again to stop them all.</p>

<pre><code>~/bug-1234/
  deploy-feat_login-api_server.log
  deploy-feat_login-worker.log
  deploy-feat_login-pane3.log      ← unnamed panes fall back to position</code></pre>

<p>Files are named after the things you recognise: the session, the
window, and the pane's own name where you gave it one. A directory of
<code>api.log</code>, <code>worker.log</code> and <code>db.log</code> is
worth something an hour later; one of
<code>p3-20260821-154233.log</code> is not.</p>

<p>The prompt is prefilled with the default log directory, so the common
case is <span class="k">Enter</span>; <code>~</code> is expanded and the
directory is created if missing. A pane you had already started logging
by hand with <code>Ctrl-B L</code> is left alone rather than restarted,
so this never truncates a file you opened deliberately.</p>

<h2>Type into several panes at once</h2>

<p><span class="k">Ctrl-B</span> <span class="k">y</span> sends your
keystrokes to every pane in the window. Useful for driving a handful of
machines together — the same tail, the same deploy.</p>

<p>But the pane running your editor, or the one holding the output you
are comparing against, is exactly the one that must <em>not</em> receive
them. So open the overview (<span class="k">Ctrl-B</span>
<span class="k">g</span>) and press <span class="k">space</span> on each
pane you want included. The status bar then reads
<code>[SYNC 3/7]</code> — before pressing Enter into several machines at
once, the number is the thing you want confirmed.</p>

<p>Picking a pane turns synchronisation on by itself, and deselecting the
last one turns it off rather than falling back to "everything".</p>

<h2>Reopen a closed pane</h2>

<p><span class="k">Ctrl-B</span> <span class="k">Z</span> brings back the
pane you just closed: a fresh shell, in the window it came from, started
in the directory it was sitting in. The last 16 closures are kept, so
repeated presses walk further back.</p>

<p>It covers every way a pane goes away, including a shell that exited on
its own — which is the case it mostly exists for: an <code>exit</code>
typed into the wrong pane. What comes back is the <em>place</em>, not the
process: retyping the command is easy, remembering which of four windows
it was in and <code>cd</code>-ing three levels down again is what
actually costs you.</p>

<h2>Saved layouts</h2>

<pre><code>termdock layout save -t work dev   # capture the current arrangement
termdock layout apply dev          # rebuild it, here or on another machine
termdock layout ls
termdock layout rm dev</code></pre>

<p>A <a href="configuration.html">session snapshot</a> is automatic and
about surviving a crash. A layout is deliberate and about starting the
same working set again tomorrow: windows, splits, ratios, window and pane
names, and each pane's working directory.</p>

<p>Applying <strong>adds</strong> to the session rather than replacing
it. A layout is something you reach for to <em>start</em> work, and one
that silently closed panes you had running — with no undo for a whole
session's worth — would be a thing you approach nervously.</p>

<p>This is the job people currently leave a multiplexer for, using
tmuxinator or teamocil: an external tool, a YAML file, a language
runtime.</p>
""",
}

CONFIG = {
    "file": "configuration.html",
    "nav": "config",
    "title": "Configuration",
    "description": "Every termdock setting, the eleven bundled themes, changing settings while a session runs, and termdock doctor for the ones that fail silently.",
    "lede": "One optional file of plain key/value lines — and a way to change most of it without restarting anything.",
    "body": """
<h2>Where the config lives</h2>

<p><code>$XDG_CONFIG_HOME/termdock/termdock.conf</code>, falling back to
<code>~/.config/termdock/termdock.conf</code>, or wherever
<code>$TERMDOCK_CONFIG</code> points. It does not exist until you create
it, and a missing file simply means defaults.</p>

<p>Plain <code>key value</code> lines, <code>#</code> for comments:</p>

<pre><code># termdock.conf
prefix C-a               # prefix key, any Ctrl+letter (default C-b)
mouse on                 # mouse support (default on)
history-limit 10000      # scrollback lines kept per pane
shell /bin/zsh           # shell for new panes (default $SHELL)
popup-command lazygit    # what Ctrl-B P runs (default: the shell)
focus-events on          # forward synthetic pane focus-in/out (default off)
repeat-time 1000         # ms a bare arrow keeps moving focus (0 disables)
bind M jump-picker       # rebind one key; repeatable, one per line
theme dracula            # bundled color preset
status-bg black
status-fg silver
pane-active-bg teal
pane-bg default          # background behind unstyled pane content
pane-fg default
status-segments git,battery,cpu,mem
status-icons unicode     # icons before them: off, unicode or nerd</code></pre>

<div class="note">
<strong>A typo never stops a session starting.</strong> A line termdock
cannot parse is ignored and the default kept. That is the right trade,
and its cost is that a mistake is invisible — which is what
<code>termdock doctor</code> below is for.
</div>

<h2>Icons in the status bar</h2>

<p><code>status-icons</code> puts a glyph in front of each optional
segment.</p>

<div class="note">
<strong>Only <code>nerd</code> needs a font installed.</strong>
termdock cannot ship an icon font, and no terminal program can: it
writes characters to a pty, and your <em>terminal emulator</em> draws
them with whatever font it is set to. Which glyphs exist is its
decision, never termdock's. If you would rather not install anything,
use <code>unicode</code> — or leave it <code>off</code>.
</div>

<p>It has three values rather than on/off, because no program can ask a
terminal whether its font actually contains a glyph — so the choice is
yours to make by looking.</p>

<table>
<tr><th>Value</th><th>Shows</th></tr>
<tr><td><code>off</code></td><td>The default. Words alone:
<code>cpu 8% | mem 41%</code>. Never wrong on any font.</td></tr>
<tr><td><code>unicode</code></td><td><code>░ cpu 8% | ▓ mem 71%</code> —
the shade fills up as the number climbs, so the glyph is a second look
at the figure rather than decoration. Nothing to install: these are
Block Elements, which ordinary monospace fonts carry, rather than the
Private Use Area. They are text-presentation too, so they stay one
column wide and cannot push the right-aligned bar out of
alignment.</td></tr>
<tr><td><code>nerd</code></td><td>Real microchip and memory icons, and
the only value that <strong>requires installing a
<a href="https://www.nerdfonts.com/">Nerd Font</a></strong> and
selecting it in your terminal. These glyphs live in the Private Use
Area — codepoints Unicode leaves deliberately unassigned — so only a
font patched to add them has anything to draw. Without one the bar reads
<code>◆ mem 10%</code>: a replacement box where the microchip should
be.</td></tr>
</table>

<p>Open the settings screen with <span class="k">Ctrl-B</span>
<span class="k">C</span>, put the cursor on <code>status-icons</code> and
step it with ←→. The bar redraws as you go, so you can see which set
your font can draw instead of guessing. If a set shows boxes, it is the
wrong one for your font.</p>

<h2>Installing a Nerd Font</h2>

<div class="note">
<strong>Install it where the terminal is, not where termdock is.</strong>
The font is used by the terminal emulator you are looking at. Over SSH
that is your laptop, not the server; on Windows with WSL that is
Windows, not the WSL distribution. Installing fonts on the far side does
nothing at all, and is the usual reason this appears not to work.
</div>

<p>Any font from
<a href="https://www.nerdfonts.com/font-downloads">nerdfonts.com</a> will
do — JetBrains Mono is used here. On <strong>Linux</strong>:</p>

<pre><code>mkdir -p ~/.local/share/fonts
curl -fLo /tmp/JetBrainsMono.zip \
  https://github.com/ryanoasis/nerd-fonts/releases/latest/download/JetBrainsMono.zip
unzip -o /tmp/JetBrainsMono.zip -d ~/.local/share/fonts/JetBrainsMono
fc-cache -f</code></pre>

<p>On <strong>macOS</strong>:</p>

<pre><code>brew install --cask font-jetbrains-mono-nerd-font</code></pre>

<p>On <strong>Windows</strong>, including when you run termdock in WSL —
do this on the Windows side, not inside the distribution:</p>

<ol>
<li>Download <code>JetBrainsMono.zip</code> from the
<a href="https://github.com/ryanoasis/nerd-fonts/releases/latest">nerd-fonts
releases</a>.</li>
<li>Extract it, select the <code>.ttf</code> files, right-click →
<strong>Install</strong>.</li>
<li>Windows Terminal → <strong>Settings</strong> → your profile →
<strong>Appearance</strong> → <strong>Font face</strong>.</li>
</ol>

<p>Then pick the font in your terminal's settings. It is listed as
<strong>JetBrainsMono Nerd Font</strong>, or as
<strong>JetBrainsMono NF</strong> in terminals that show the short family
name — Windows Terminal is one. Restart the terminal, then set
<code>status-icons nerd</code>. None of this is needed for
<code>unicode</code>, which draws characters fonts already have.</p>

<h2>Which settings apply when</h2>

<p><code>prefix</code>, <code>shell</code>, <code>history-limit</code>,
<code>popup-command</code>, <code>focus-events</code>,
<code>repeat-time</code> and <code>bind</code> are read by the
<strong>server</strong>, so they take effect when a session is
<em>created</em>.</p>

<p><code>mouse</code>, <code>theme</code> and the colours are read by the
<strong>client</strong>, so detaching and reattaching is enough.</p>

<h2>Themes</h2>

<p>Eleven, built in — no plugin, no plugin manager:</p>

<pre><code>termdock themes

catppuccin  dracula  everforest  gruvbox  monokai  nord
one-dark    rose-pine  solarized  tokyo-night  ubuntu</code></pre>

<p>A theme sets the status bar, the active pane's accent, <em>and the
pane backgrounds</em> — so a themed session looks themed all the way out
to the margins, rather than being a coloured status bar floating on
whatever your terminal profile uses. termdock also asks the terminal
emulator itself to adopt the colours, so even the few pixels of padding
around the character grid match, and puts them back when you detach.</p>

<p>A theme is only a baseline: an explicit <code>pane-active-bg</code>
line still overrides just that one colour, whichever order the two lines
come in. <code>pane-bg default</code> opts the pane backgrounds back out
while keeping the rest.</p>

<h2>Changing settings while it runs</h2>

<p><span class="k">Ctrl-B</span> <span class="k">C</span> opens every
setting with its current value and what it does.
<span class="k">←</span> <span class="k">→</span> steps through the
values a setting can take — all eleven palettes on <code>theme</code>,
applying each as you land on it, so you choose by looking rather than by
reading names. <span class="k">Enter</span> types a value for anything
free-form.</p>

<p><span class="k">S</span> writes the current value to your config file,
rewriting just that line. Nothing is written unless you ask: silently
rewriting a file full of your own comments and ordering is not a thing to
do as a side effect of trying something out.</p>

<p>The same vocabulary works from the command prompt:</p>

<pre><code>Ctrl-B :  set theme nord
Ctrl-B :  set -p theme nord     # and persist it</code></pre>

<h2>Checking your own setup</h2>

<pre><code>termdock doctor</code></pre>

<p>Checks the things that fail <em>silently</em>:</p>

<pre><code>[ warn ] theme                  "drakula" is not a built-in theme, so the line is being ignored
                                → check the spelling against `termdock themes`
[ warn ] shell integration      no `termdock shell-init` line found in your shell startup files
                                → add: eval "$(termdock shell-init)"</code></pre>

<p>Every check reports what it <em>found</em> rather than a bare verdict,
so the output is worth pasting into a bug report from a machine you
cannot see, and every warning names the thing to do about it.</p>

<h2>Crash recovery</h2>

<p>termdock snapshots each session continuously: the layout, every pane's
working directory and name, <em>and the last 200 lines of each pane's
screen</em>. If the daemon dies or the machine reboots, starting a
session with the same name brings it back — including the stack trace you
were reading, rather than four blank prompts.</p>

<p>What comes back is text, not a live program: nothing can resurrect
what was running. Quitting deliberately with
<span class="k">Ctrl-B</span> <span class="k">q</span> deletes the
snapshot, so a session you ended stays ended.</p>

<h2>Debugging input</h2>

<p>If a key does something unexpected, set
<code>TERMDOCK_INPUT_LOG=/path/to/file</code> when starting the server
and every key, mouse and resize event is appended there with the state it
left behind:</p>

<pre><code>02:47:54.598 key code=66 rune='\\x00' mod=0    -> prefix=true mode=normal
02:47:54.598 key code=257 rune='\\x00' mod=0   -> prefix=false mode=normal</code></pre>

<p>What your terminal emulator actually sends for a chord, and whether a
key reached the daemon at all, are otherwise invisible.</p>
""",
}

REMOTE = {
    "file": "remote.html",
    "nav": "remote",
    "title": "Remote sessions & sharing",
    "description": "Run a termdock session on a server, attach from anywhere, and let a colleague watch read-only without giving them your account.",
    "lede": "A session lives on the machine that owns the panes. Reaching it is SSH's job, and SSH does it well.",
    "body": """
<h2>A session on another machine</h2>

<p>A session's socket lives on the machine running the panes, so "remote"
means SSH — and it works with nothing extra installed or configured:</p>

<pre><code>ssh -t server termdock new -s work        # create it there, and attach
ssh -t server termdock attach -t work     # come back from any other machine
ssh -t server termdock attach -t work -r  # attach as a read-only observer</code></pre>

<p>The <code>-t</code> is doing real work: it asks SSH for a pty, which
the client needs in order to draw.</p>

<p>Detach with <span class="k">Ctrl-B</span> <span class="k">d</span> and
the panes keep running on the server exactly as they would locally. Close
the laptop, open another one, attach again. This is the client/server
split earning its keep: your terminal was never where the work lived.</p>

<h2>Letting someone watch</h2>

<p><code>-r</code> is a real read-only attach. Every frame streams to
that client, and every key and mouse event it sends is
<strong>dropped by the server</strong> before it reaches the session.
That is enforced on the daemon's side, not by asking the client to
behave, so a modified or scripted client cannot type either. An
observer's terminal size does not resize the shared session, so a smaller
window on their end disturbs nobody.</p>

<p>Any number of normal and read-only clients can attach at once.</p>

<h2>Without giving away your account</h2>

<p>The socket is <code>0600</code> inside a <code>0700</code> directory,
so attaching means being <em>you</em> on that machine. Handing a
colleague your login so they can watch is not sharing; it is handing over
the keys.</p>

<p>SSH solves this properly, with a <strong>forced command</strong>. Put
their public key in your <code>~/.ssh/authorized_keys</code>, restricted
to exactly one thing:</p>

<pre><code>restrict,pty,command="/usr/local/bin/termdock attach -t work -r" ssh-ed25519 AAAAC3Nz... alice</code></pre>

<p>Now <code>ssh server</code> from Alice's machine drops her straight
into watching that session, read-only, and nothing else.
<code>restrict</code> denies port forwarding, agent forwarding, X11 and
user rc files; <code>pty</code> puts back the one capability the client
actually needs. Delete the line to end the sharing.</p>

<div class="note">
<strong>Use the binary's absolute path.</strong> A forced command runs
with a minimal environment, and <code>termdock</code> may not be on
<code>PATH</code>.
</div>

<h2>Why this is SSH's job</h2>

<p>Authentication and encryption are the parts that must not be got
wrong, and OpenSSH has had decades of scrutiny on exactly them. A
bespoke listener in termdock with a hand-rolled token would be a
downgrade wearing the word "feature".</p>

<p>It compares well, too. tmux users reach for <code>wemux</code>, which
only works between accounts on one machine, or <code>tmate</code>, which
relays your terminal through someone else's servers.</p>

<h2>Driving a session from a script</h2>

<p>Sessions can be operated without a client attached at all, which is
what makes them usable from CI, cron, or a setup script:</p>

<pre><code>termdock new-window -t work -n logs
termdock split-window -t work -v
termdock send-keys -t work:1 'tail -f /var/log/app.log' Enter
termdock select-window -t work:1
termdock select-pane -t work:1 -R
termdock list-windows -t work
termdock list-panes -t work:1</code></pre>

<p>A target is <code>SESSION[:WINDOW[.PANE]]</code>, where
<code>PANE</code> is the number shown in the pane's title bar.</p>
""",
}

PAGES = [GETTING_STARTED, KEYS, SHELL, WORKFLOW, CONFIG, REMOTE]
