package server

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns the directory termdock keeps its session sockets in,
// creating it (mode 0700, so other users on the machine can't see or
// connect to it) if needed.
func Dir() (string, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = filepath.Join(os.TempDir(), fmt.Sprintf("termdock-%d", os.Getuid()))
	} else {
		base = filepath.Join(base, "termdock")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", err
	}
	return base, nil
}

// SocketPath returns the unix socket path for a named session.
func SocketPath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sock"), nil
}
