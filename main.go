// Command termdock is a terminal multiplexer: split the screen into
// panes, and keep sessions running in the background so you can detach
// and reattach later, the way tmux does. Run with no arguments to attach
// to (or create) the default session; run "termdock help" for the full
// command list.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/padovanl/termdock/internal/client"
	"github.com/padovanl/termdock/internal/config"
	"github.com/padovanl/termdock/internal/server"
)

const defaultSession = "main"

// version is overwritten at build time via
// -ldflags "-X main.version=vX.Y.Z" (see .goreleaser.yml); a plain
// "go build"/"go install" leaves it at "dev".
var version = "dev"

func main() {
	args := os.Args[1:]

	if len(args) > 0 && args[0] == "__daemon" {
		runDaemon(args[1:])
		return
	}

	if len(args) == 0 {
		cmdAttachOrCreate(defaultSession)
		return
	}

	switch args[0] {
	case "new":
		name := parseSessionFlag(args[1:], "")
		if name == "" {
			name = autoName()
		}
		cmdNew(name)
	case "attach":
		cmdAttach(parseSessionFlag(args[1:], defaultSession), hasFlag(args[1:], "-r", "--view"))
	case "ls", "list-sessions":
		cmdList()
	case "kill-session":
		name := parseSessionFlag(args[1:], "")
		if name == "" {
			fatal("usage: termdock kill-session -t NAME")
		}
		cmdKill(name)
	case "send-keys":
		cmdSendKeys(args[1:])
	case "new-window":
		cmdNewWindow(args[1:])
	case "split-window":
		cmdSplitWindow(args[1:])
	case "select-window":
		cmdSelectWindow(args[1:])
	case "list-windows":
		cmdListWindows(args[1:])
	case "list-panes":
		cmdListPanes(args[1:])
	case "-h", "--help", "help":
		printUsage()
	case "-v", "--version", "version":
		fmt.Println("termdock version " + version)
	default:
		fmt.Fprintf(os.Stderr, "termdock: unknown command %q\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func cmdAttachOrCreate(name string) {
	sock, err := server.SocketPath(name)
	check(err)
	if _, ok := server.Probe(sock); !ok {
		check(spawnDaemon(name, sock))
	}
	check(client.Run(sock, config.Load(), false))
}

func cmdNew(name string) {
	sock, err := server.SocketPath(name)
	check(err)
	if _, ok := server.Probe(sock); ok {
		fatal(fmt.Sprintf("session %q is already active (use: termdock attach -t %s)", name, name))
	}
	check(spawnDaemon(name, sock))
	check(client.Run(sock, config.Load(), false))
}

func cmdAttach(name string, readOnly bool) {
	sock, err := server.SocketPath(name)
	check(err)
	if _, ok := server.Probe(sock); !ok {
		fatal(fmt.Sprintf("no active session %q (use: termdock new -s %s)", name, name))
	}
	check(client.Run(sock, config.Load(), readOnly))
}

func cmdList() {
	infos, err := server.List()
	check(err)
	if len(infos) == 0 {
		fmt.Println("no active sessions")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSION\tPANES\tCREATED")
	for _, in := range infos {
		age := time.Since(time.Unix(in.CreatedAt, 0)).Truncate(time.Second)
		fmt.Fprintf(tw, "%s\t%d\t%s ago\n", in.Name, in.PaneCount, age)
	}
	tw.Flush()
}

func cmdKill(name string) {
	sock, err := server.SocketPath(name)
	check(err)
	if err := server.Kill(sock); err != nil {
		fatal(fmt.Sprintf("session %q not reachable: %v", name, err))
	}
	fmt.Printf("session %q terminated\n", name)
}

// spawnDaemon starts the server as a fully-detached background process
// (its own session, stdio redirected away) and waits for its socket to
// come up before returning.
func spawnDaemon(name, sock string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	dir, err := server.Dir()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(dir+"/"+name+".log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "__daemon", "--session", name, "--socket", sock)
	cmd.Stdin = devnull
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The daemon fully detaches (new session, stdio elsewhere); don't wait
	// on it, just poll until its socket answers.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := server.Probe(sock); ok {
			return nil
		}
		time.Sleep(30 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for session %q to start (check %s)", name, dir+"/"+name+".log")
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("__daemon", flag.ExitOnError)
	name := fs.String("session", "", "")
	sock := fs.String("socket", "", "")
	fs.Parse(args)
	if *name == "" || *sock == "" {
		os.Exit(2)
	}
	if err := server.Run(*name, *sock, config.Load()); err != nil {
		fmt.Fprintln(os.Stderr, "termdock daemon:", err)
		os.Exit(1)
	}
}

func autoName() string {
	existing := map[string]bool{}
	if infos, err := server.List(); err == nil {
		for _, in := range infos {
			existing[in.Name] = true
		}
	}
	for i := 0; ; i++ {
		n := strconv.Itoa(i)
		if !existing[n] {
			return n
		}
	}
}

func parseSessionFlag(args []string, def string) string {
	for i, a := range args {
		if (a == "-s" || a == "-t" || a == "--session") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return def
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n {
				return true
			}
		}
	}
	return false
}

func check(err error) {
	if err != nil {
		fatal(err.Error())
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "termdock:", msg)
	os.Exit(1)
}

func printUsage() {
	fmt.Print(`termdock - terminal multiplexer with persistent sessions

Usage:
  termdock                     attach to (or create) the default "main" session
  termdock new [-s NAME]       create a new session and attach to it
  termdock attach [-t NAME] [-r]  attach to an existing session (default "main");
                                  -r attaches read-only, as an observer
  termdock ls                  list active sessions
  termdock kill-session -t NAME  terminate a session
  termdock --version            print the version and exit

Scripting (drive a session without attaching to it; TARGET is
SESSION[:WINDOW[.PANE]], e.g. "main", "main:1", "main:1.4"):
  termdock send-keys -t TARGET text... [Enter]
  termdock new-window -t SESSION [-n NAME] [command...]
  termdock split-window -t TARGET [-v|-s] [command...]
  termdock select-window -t SESSION:WINDOW
  termdock list-windows -t SESSION
  termdock list-panes -t SESSION[:WINDOW]

Inside, the prefix key is Ctrl-B. See README.md for the full key list.
`)
}
