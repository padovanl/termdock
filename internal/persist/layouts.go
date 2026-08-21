package persist

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Saved layouts are the piece that turns a multiplexer into a
// reproducible workspace. A session snapshot (see Snapshot) is automatic
// and about *this* session surviving a crash; a layout is deliberate and
// about starting the same arrangement again tomorrow, on another
// machine, or handing it to a colleague.
//
// It stores the shape — windows, splits, ratios, names — and the working
// directory and command for each pane, so applying it rebuilds the whole
// working set rather than an empty grid you then have to populate by
// hand. That is the job people currently leave termdock for, using
// tmuxinator or teamocil: an external tool, a YAML file, a dependency.

// LayoutPane is one pane in a saved layout.
type LayoutPane struct {
	Name    string `json:",omitempty"` // a name the user gave it
	Cwd     string `json:",omitempty"`
	Command string `json:",omitempty"` // run instead of a plain shell
}

// LayoutNode mirrors the split tree, with leaves carrying a pane.
type LayoutNode struct {
	Split  int         `json:",omitempty"` // 0 leaf, 1 vertical, 2 horizontal
	Ratio  float64     `json:",omitempty"`
	Pane   *LayoutPane `json:",omitempty"`
	First  *LayoutNode `json:",omitempty"`
	Second *LayoutNode `json:",omitempty"`
}

// LayoutWindow is one window of a saved layout.
type LayoutWindow struct {
	Name string `json:",omitempty"`
	Root LayoutNode
}

// Layout is a named, reusable arrangement.
type Layout struct {
	Name    string
	Windows []LayoutWindow
}

// layoutsDir is where they live: alongside session snapshots, since both
// are state that outlives a run without being configuration.
func layoutsDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, "layouts")
	if err := os.MkdirAll(sub, 0700); err != nil {
		return "", err
	}
	return sub, nil
}

// ValidateLayoutName keeps a name a plain file inside layoutsDir, for
// the same reason session names are checked: it is pasted into a path,
// and filepath.Join will happily resolve "../escaped" out of the
// directory entirely.
func ValidateLayoutName(name string) error {
	switch {
	case name == "":
		return errors.New("layout name cannot be empty")
	case name == "." || name == "..":
		return fmt.Errorf("%q is not a usable layout name", name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("layout name %q cannot contain a path separator", name)
	case strings.ContainsRune(name, 0):
		return fmt.Errorf("layout name %q cannot contain a null byte", name)
	}
	return nil
}

func layoutPath(name string) (string, error) {
	if err := ValidateLayoutName(name); err != nil {
		return "", err
	}
	dir, err := layoutsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

// SaveLayout writes a layout, replacing any of the same name.
func SaveLayout(l Layout) error {
	p, err := layoutPath(l.Name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	// Write-then-rename, same as session snapshots: a process dying
	// mid-write must not leave a half-written layout for the next load.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// LoadLayout reads a saved layout by name.
func LoadLayout(name string) (Layout, error) {
	p, err := layoutPath(name)
	if err != nil {
		return Layout{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Layout{}, fmt.Errorf("no saved layout called %q", name)
		}
		return Layout{}, err
	}
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return Layout{}, fmt.Errorf("layout %q is corrupt: %w", name, err)
	}
	return l, nil
}

// ListLayouts names every saved layout, sorted.
func ListLayouts() ([]string, error) {
	dir, err := layoutsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)
	return names, nil
}

// DeleteLayout removes one.
func DeleteLayout(name string) error {
	p, err := layoutPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no saved layout called %q", name)
		}
		return err
	}
	return nil
}
