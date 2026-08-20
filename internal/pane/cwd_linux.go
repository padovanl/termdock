//go:build linux

package pane

import (
	"os"
	"strconv"
)

// processCwd resolves a process's current working directory via the
// /proc/<pid>/cwd symlink.
func processCwd(pid int) string {
	link, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	if err != nil {
		return ""
	}
	return link
}
