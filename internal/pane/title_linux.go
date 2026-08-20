//go:build linux

package pane

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// foregroundName reads the name of the process currently in the pty's
// foreground process group (what the shell has an interactive command
// running, if any), for auto-updating pane titles the way tmux does. Linux
// only: reads TIOCGPGRP off the pty, then /proc/<pgid>/comm.
func foregroundName(f *os.File) string {
	var pgid int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgid)))
	if errno != 0 || pgid <= 0 {
		return ""
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(int(pgid)) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
