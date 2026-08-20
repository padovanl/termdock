package persist

import (
	"os"
	"testing"
)

// TestMain points Dir at a throwaway temp directory for the whole test
// binary run, so these tests never read or write the real
// ~/.local/state/termdock.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "termdock-test-state-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", dir)
	code := m.Run() // os.Exit below would skip a deferred cleanup, so run it explicitly first
	os.RemoveAll(dir)
	os.Exit(code)
}

func TestSaveLoadDeleteRoundtrip(t *testing.T) {
	name := "test-persist-roundtrip"

	if _, ok := Load(name); ok {
		t.Fatal("Load should report ok=false before anything was ever Saved")
	}

	snap := Snapshot{
		SessionName: name,
		Windows: []Window{
			{
				Name:    "dev",
				Renamed: true,
				Root: Node{
					Split: 1, // Vertical
					Ratio: 0.6,
					First: &Node{Cwd: "/tmp/left"},
					Second: &Node{
						Split:  2, // Horizontal
						Ratio:  0.3,
						First:  &Node{Cwd: "/tmp/right-top"},
						Second: &Node{Cwd: "/tmp/right-bottom"},
					},
				},
			},
			{Name: "logs", Root: Node{Cwd: "/var/log"}},
		},
	}

	if err := Save(name, snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := Load(name)
	if !ok {
		t.Fatal("Load should report ok=true right after Save")
	}
	if got.SessionName != snap.SessionName {
		t.Errorf("SessionName = %q, want %q", got.SessionName, snap.SessionName)
	}
	if len(got.Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(got.Windows))
	}
	w0 := got.Windows[0]
	if w0.Name != "dev" || !w0.Renamed {
		t.Errorf("window 0 = %+v, want Name=dev Renamed=true", w0)
	}
	if w0.Root.Split != 1 || w0.Root.Ratio != 0.6 {
		t.Errorf("window 0 root = %+v, want Split=1 Ratio=0.6", w0.Root)
	}
	if w0.Root.First == nil || w0.Root.First.Cwd != "/tmp/left" {
		t.Errorf("window 0 root.First = %+v, want Cwd=/tmp/left", w0.Root.First)
	}
	if w0.Root.Second == nil || w0.Root.Second.Split != 2 || w0.Root.Second.Second == nil ||
		w0.Root.Second.Second.Cwd != "/tmp/right-bottom" {
		t.Errorf("window 0 root.Second nested structure not preserved: %+v", w0.Root.Second)
	}
	if got.Windows[1].Root.Cwd != "/var/log" {
		t.Errorf("window 1 root.Cwd = %q, want /var/log", got.Windows[1].Root.Cwd)
	}

	Delete(name)
	if _, ok := Load(name); ok {
		t.Fatal("Load should report ok=false after Delete")
	}
}

func TestLoadMissingIsNotOK(t *testing.T) {
	if _, ok := Load("test-definitely-never-saved-xyz"); ok {
		t.Fatal("Load of a session that was never saved should report ok=false")
	}
}
