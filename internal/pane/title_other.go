//go:build !linux

package pane

import "os"

// foregroundName isn't implemented outside Linux; panes just keep their
// static shell-name title.
func foregroundName(f *os.File) string {
	return ""
}
