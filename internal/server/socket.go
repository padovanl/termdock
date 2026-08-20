package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if err := ValidateSessionName(name); err != nil {
		return "", err
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".sock"), nil
}

// ValidateSessionName rejects names that wouldn't stay a plain file
// inside Dir(). A session name is pasted straight into a socket path (and
// the daemon's log path alongside it), and filepath.Join happily resolves
// whatever it's given: "proj/api" pointed at a subdirectory nothing ever
// creates, so the daemon failed to start with an error about an internal
// log path, and "../escaped" genuinely climbed *out* of the sockets
// directory — leaving a live daemon with its socket somewhere List() does
// not look, so "termdock ls" couldn't see the session and there was no
// ordinary way to find it again.
func ValidateSessionName(name string) error {
	switch {
	case name == "":
		return errors.New("session name cannot be empty")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a usable session name", name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("session name %q cannot contain a path separator", name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("session name %q cannot contain a null byte", name)
	}
	return nil
}
