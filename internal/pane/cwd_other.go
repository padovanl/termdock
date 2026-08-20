//go:build !linux

package pane

// processCwd isn't implemented outside Linux; session snapshots just
// restart panes without a specific working directory.
func processCwd(pid int) string {
	return ""
}
