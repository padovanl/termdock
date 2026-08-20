// Package proto defines the wire messages exchanged between a termdock
// client (the tcell UI attached to a real terminal) and the termdock
// server (the persistent daemon that owns the panes and survives
// detach/reattach). Both directions are gob-encoded straight onto the
// session's unix socket connection.
package proto

// Rect is an axis-aligned screen region, mirroring internal/layout.Rect.
type Rect struct {
	X, Y, W, H int
}

// Cell is one rendered terminal cell.
type Cell struct {
	Ch   rune
	FG   uint32
	BG   uint32
	Attr uint16
}

// PaneFrame is the rendered content of a single pane. Rect is pure
// terminal content — the client draws a one-cell border around it (with
// Title embedded in the top edge) entirely outside these bounds, inferred
// from the Rects of all panes in the Frame rather than sent explicitly.
type PaneFrame struct {
	ID            int
	Rect          Rect
	Title         string
	Active        bool
	Zoomed        bool // this pane currently fills the whole window (prefix z)
	CursorX       int
	CursorY       int
	CursorVisible bool
	Cells         [][]Cell // Rect's content rows x cols
}

// WindowTab is one entry in the status bar's window tab strip, already
// laid out by the server: Label is the exact text to draw (padding
// included) and X/W is the column range it occupies on the status bar
// row, so the client can just paint it and the server can hit-test a
// click against the same numbers with no duplicated layout logic on
// either side.
type WindowTab struct {
	Index    int
	Label    string
	Active   bool
	Activity bool
	X, W     int
}

// Frame is a full snapshot of everything the client needs to paint.
// The server sends one whenever session state changes.
type Frame struct {
	Cols, Rows   int
	Panes        []PaneFrame
	StatusPrefix string      // " termdock:name ", drawn before the tab strip
	Windows      []WindowTab // the window tab strip, drawn after StatusPrefix
	StatusText   string      // trailing segment, drawn after the tab strip
	StatusRight  string      // right-aligned segment (hostname/clock)
	StatusStyle  string      // "normal" | "prefix" | "mode"
	ShowStatus   bool        // false on a 1-row-tall terminal: no room for it
	SessionName  string
}

// WindowInfo and PaneInfo answer the list-windows/list-panes CLI
// commands.
type WindowInfo struct {
	Index  int
	Name   string
	Active bool
	Panes  int
}

type PaneInfo struct {
	ID     int
	Title  string
	Active bool
}

// ServerInfo answers a Query control message.
type ServerInfo struct {
	Name      string
	PaneCount int
	CreatedAt int64 // unix seconds
}

// ClientMsg is anything the client sends to the server. Kind selects which
// fields are meaningful.
//
// "hello" starts an interactive attach (the Frame-streaming loop).
// "query" and "kill" are one-shot control messages, answered with a
// single reply and no attach. So are the scripting commands below
// (send-keys, new-window, split-window, select-window, list-windows,
// list-panes) — termdock's equivalent of tmux's command interface, for
// driving a session from a shell script rather than a live client.
// Detaching is driven server-side (Ctrl-B d is just a "key" message);
// the server tells the client to hang up via ServerMsg{Kind:"bye"}.
type ClientMsg struct {
	Kind string

	Cols, Rows int // hello, resize

	// key: raw tcell.EventKey fields, forwarded verbatim so all
	// interpretation logic lives server-side in internal/core.
	KeyRune rune
	KeyCode int32
	KeyMod  int32

	// mouse: raw tcell.EventMouse fields.
	MouseX, MouseY int
	MouseButtons   int32
	MouseMod       int32

	// Scripting commands. WindowIdx >= 0 wins if given; else WindowName is
	// looked up by the window's display name; else it's the active
	// window. PaneID <= 0 means "that window's active pane".
	WindowIdx  int
	WindowName string
	PaneID     int
	CLIText    string // send-keys: literal text to write to the pane
	CLIEnter   bool   // send-keys: append a carriage return after CLIText
	CLICommand string // new-window/split-window: run this instead of the shell
	CLIName    string // new-window: initial window name
	CLIAxis    string // split-window: "v" (side by side) or "s" (stacked)
}

// ServerMsg is anything the server sends to the client.
type ServerMsg struct {
	Kind      string // "frame" | "info" | "clipboard" | "bye" | "cli"
	Frame     *Frame
	Info      *ServerInfo
	Clipboard string // Kind == "clipboard": text to push via OSC52
	Bye       string // Kind == "bye": human-readable reason, shown before the client exits

	// Kind == "cli": reply to a scripting command.
	CLIError   string
	CLIWindows []WindowInfo
	CLIPanes   []PaneInfo
	CLIIndex   int // new-window: the created window's index
	CLIPaneID  int // split-window: the created pane's ID
}
