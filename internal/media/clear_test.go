package media

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearExcept(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)
	for _, id := range []string{"keep", "old"} {
		if err := os.Mkdir(filepath.Join(dir, id), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	n, err := mgr.ClearExcept("keep")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removed %d", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatalf("old cache still present: %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	n, err = mgr.ClearExcept("")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("removed %d, want both remaining folders", n)
	}
}