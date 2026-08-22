// Scripting commands: termdock's equivalent of tmux's command interface
// (send-keys, new-window, split-window, select-window, list-windows,
// list-panes), for driving a session from outside — a shell script
// setting up a dev environment, a Makefile, whatever — rather than a
// live client. Each dials the target session's socket, sends one control
// message, prints the reply, and exits; the running session (and any
// attached client) is untouched otherwise.
package main

import (
	"encoding/gob"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/padovanl/termdock/internal/persist"
	"github.com/padovanl/termdock/internal/proto"
	"github.com/padovanl/termdock/internal/server"
)

func cmdSendKeys(args []string) {
	target, rest := extractTarget(args)
	if target == "" || len(rest) == 0 {
		fatal("usage: termdock send-keys -t TARGET text... [Enter]")
	}
	enter := false
	if rest[len(rest)-1] == "Enter" {
		enter = true
		rest = rest[:len(rest)-1]
	}
	session, winIdx, winName, paneIdx := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{
		Kind: "send-keys", WindowIdx: winIdx, WindowName: winName, PaneIndex: paneIdx,
		CLIText: strings.Join(rest, " "), CLIEnter: enter,
	})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
}

func cmdNewWindow(args []string) {
	target, rest := extractTarget(args)
	if target == "" {
		fatal("usage: termdock new-window -t SESSION [-n NAME] [command...]")
	}
	name := ""
	if len(rest) >= 2 && rest[0] == "-n" {
		name, rest = rest[1], rest[2:]
	}
	session, _, _, _ := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{Kind: "new-window", CLIName: name, CLICommand: strings.Join(rest, " ")})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
	fmt.Printf("created window %d\n", reply.CLIIndex)
}

func cmdSplitWindow(args []string) {
	target, rest := extractTarget(args)
	if target == "" {
		fatal("usage: termdock split-window -t TARGET [-v|-s] [command...]")
	}
	axis := "v" // v = side by side, s = stacked — matches termdock's own Ctrl-B v/s, not tmux's inverted -h/-v
	if len(rest) > 0 && (rest[0] == "-v" || rest[0] == "-s") {
		axis, rest = strings.TrimPrefix(rest[0], "-"), rest[1:]
	}
	session, winIdx, winName, paneIdx := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{
		Kind: "split-window", WindowIdx: winIdx, WindowName: winName, PaneIndex: paneIdx,
		CLIAxis: axis, CLICommand: strings.Join(rest, " "),
	})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
	fmt.Printf("created pane %d\n", reply.CLIPaneID)
}

func cmdSelectWindow(args []string) {
	target, _ := extractTarget(args)
	session, winIdx, winName, _ := parseTarget(target)
	if target == "" || (winIdx < 0 && winName == "") {
		fatal("usage: termdock select-window -t SESSION:WINDOW")
	}
	reply := dialCLI(session, proto.ClientMsg{Kind: "select-window", WindowIdx: winIdx, WindowName: winName})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
}

func cmdSelectPane(args []string) {
	target, rest := extractTarget(args)
	if target == "" || len(rest) != 1 {
		fatal("usage: termdock select-pane -t TARGET -L|-R|-U|-D")
	}
	dir := ""
	switch rest[0] {
	case "-L":
		dir = "L"
	case "-R":
		dir = "R"
	case "-U":
		dir = "U"
	case "-D":
		dir = "D"
	default:
		fatal("usage: termdock select-pane -t TARGET -L|-R|-U|-D")
	}
	session, winIdx, winName, _ := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{Kind: "select-pane", WindowIdx: winIdx, WindowName: winName, CLIDirection: dir})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
}

func cmdListWindows(args []string) {
	target, _ := extractTarget(args)
	if target == "" {
		fatal("usage: termdock list-windows -t SESSION")
	}
	session, _, _, _ := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{Kind: "list-windows"})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "INDEX\tNAME\tPANES\tACTIVE")
	for _, w := range reply.CLIWindows {
		fmt.Fprintf(tw, "%d\t%s\t%d\t%s\n", w.Index, w.Name, w.Panes, activeMark(w.Active))
	}
	tw.Flush()
}

func cmdListPanes(args []string) {
	target, _ := extractTarget(args)
	if target == "" {
		fatal("usage: termdock list-panes -t SESSION[:WINDOW]")
	}
	session, winIdx, winName, _ := parseTarget(target)
	reply := dialCLI(session, proto.ClientMsg{Kind: "list-panes", WindowIdx: winIdx, WindowName: winName})
	if reply.CLIError != "" {
		fatal(reply.CLIError)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// INDEX first, and it's what a TARGET's ".PANE" part takes: it's the
	// number the pane shows in its own title bar on screen. ID is the
	// internal identifier, reported because respawn-pane changes it and
	// it can be handy to see, but it isn't an address.
	fmt.Fprintln(tw, "INDEX\tID\tTITLE\tACTIVE")
	for _, p := range reply.CLIPanes {
		fmt.Fprintf(tw, "%d\t%d\t%s\t%s\n", p.Index, p.ID, p.Title, activeMark(p.Active))
	}
	tw.Flush()
}

func activeMark(active bool) string {
	if active {
		return "*"
	}
	return ""
}

// extractTarget pulls "-t VALUE" (or --target) out of args, returning the
// value and the remaining args with it removed.
func extractTarget(args []string) (target string, rest []string) {
	for i, a := range args {
		if (a == "-t" || a == "--target") && i+1 < len(args) {
			rest = append(append([]string{}, args[:i]...), args[i+2:]...)
			return args[i+1], rest
		}
	}
	return "", args
}

// parseTarget splits a "session[:window[.pane]]" spec, where window may
// be either a numeric index or the name it was given (via new-window -n,
// or the default auto-name). windowIdx is -1 (meaning "look at
// windowName instead, or if that's empty too, the active window") if the
// window part isn't a plain integer. paneIdx is the pane's 1-based
// position within that window — the number it shows in its own title bar,
// and the INDEX column of list-panes — or 0 (meaning "that window's
// active pane") if unspecified.
func parseTarget(spec string) (session string, windowIdx int, windowName string, paneIdx int) {
	windowIdx = -1
	parts := strings.SplitN(spec, ":", 2)
	session = parts[0]
	if len(parts) != 2 || parts[1] == "" {
		return session, windowIdx, windowName, paneIdx
	}
	wp := strings.SplitN(parts[1], ".", 2)
	if n, err := strconv.Atoi(wp[0]); err == nil {
		windowIdx = n
	} else {
		windowName = wp[0]
	}
	if len(wp) == 2 {
		if n, err := strconv.Atoi(wp[1]); err == nil {
			paneIdx = n
		}
	}
	return session, windowIdx, windowName, paneIdx
}

// dialCLI sends one control message to session's socket and returns its
// single reply. Exits the process on any transport error, same as check.
func dialCLI(session string, msg proto.ClientMsg) proto.ServerMsg {
	sock, err := server.SocketPath(session)
	check(err)
	conn, err := net.Dial("unix", sock)
	if err != nil {
		fatal(fmt.Sprintf("session %q not reachable: %v", session, err))
	}
	defer conn.Close()
	if err := gob.NewEncoder(conn).Encode(msg); err != nil {
		fatal(err.Error())
	}
	var reply proto.ServerMsg
	if err := gob.NewDecoder(conn).Decode(&reply); err != nil {
		fatal(err.Error())
	}
	return reply
}

// cmdLayout implements the "layout" verb: save, apply, list and delete
// named arrangements. Saving and applying need a live session (the
// former to read it, the latter to build into), so both go over the
// socket; listing and deleting are pure filesystem work and don't
// require one to be running.
func cmdLayout(args []string) {
	if len(args) == 0 {
		fatal("usage: termdock layout save|apply|ls|rm [-t SESSION] [NAME]")
	}
	sub := args[0]
	target, rest := extractTarget(args[1:])
	name := ""
	if len(rest) > 0 {
		name = rest[0]
	}

	switch sub {
	case "ls", "list":
		names, err := persist.ListLayouts()
		check(err)
		if len(names) == 0 {
			fmt.Println("no saved layouts")
			return
		}
		for _, n := range names {
			fmt.Println(n)
		}

	case "rm", "delete":
		if name == "" {
			fatal("usage: termdock layout rm NAME")
		}
		check(persist.DeleteLayout(name))
		fmt.Printf("deleted layout %q\n", name)

	case "save":
		if name == "" {
			fatal("usage: termdock layout save [-t SESSION] NAME")
		}
		session, _, _, _ := parseTarget(target)
		reply := dialCLI(session, proto.ClientMsg{Kind: "save-layout", CLIName: name})
		if reply.CLIError != "" {
			fatal(reply.CLIError)
		}
		fmt.Printf("saved layout %q\n", name)

	case "apply":
		if name == "" {
			fatal("usage: termdock layout apply [-t SESSION] NAME")
		}
		session, _, _, _ := parseTarget(target)
		reply := dialCLI(session, proto.ClientMsg{Kind: "apply-layout", CLIName: name})
		if reply.CLIError != "" {
			fatal(reply.CLIError)
		}
		fmt.Printf("applied layout %q\n", name)

	default:
		fatal(fmt.Sprintf("unknown layout command %q; try save, apply, ls or rm", sub))
	}
}
