package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// occulited task 167: the first start's leftovers pass keeps the ReGa database (lite-addon-logic
// imports from it) and still removes its .bak; KeptRegadom tells it, a link is none, and only the
// explicit removal takes it.
func TestKeptRegadom(t *testing.T) {
	root := Root(t.TempDir())
	state := t.TempDir()
	cfg := filepath.Join(string(root), "etc", "config")
	_ = os.MkdirAll(cfg, 0o755)
	if k := root.KeptRegadom(); k.Present {
		t.Fatalf("%+v", k)
	}
	if _, err := root.RemoveKeptRegadom(); !errors.Is(err, ErrNoRegadom) {
		t.Fatal(err)
	}
	if _, err := root.ReadKeptRegadom(); !errors.Is(err, ErrNoRegadom) {
		t.Fatal(err)
	}
	// a link at the path is not the database
	_ = os.Symlink("/etc/shadow", filepath.Join(cfg, "homematic.regadom"))
	if k := root.KeptRegadom(); k.Present {
		t.Fatalf("a link: %+v", k)
	}
	_ = os.Remove(filepath.Join(cfg, "homematic.regadom"))

	_ = os.WriteFile(filepath.Join(cfg, "homematic.regadom"), []byte("regadom"), 0o600)
	_ = os.WriteFile(filepath.Join(cfg, "homematic.regadom.bak"), []byte("bak"), 0o600)
	when := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(cfg, "homematic.regadom"), when, when)
	run, ran, err := root.RemoveLeftoversOnce(state, true, when, nil, nil)
	if err != nil || !ran {
		t.Fatal(ran, err)
	}
	if len(run.Removed) != 1 || run.Removed[0] != "regadom-bak" {
		t.Fatalf("%+v", run)
	}
	k := root.KeptRegadom()
	if !k.Present || k.Bytes != 7 || k.Modified != "2026-11-01T12:00:00Z" {
		t.Fatalf("kept: %+v", k)
	}
	if b, err := root.ReadKeptRegadom(); err != nil || string(b) != "regadom" {
		t.Fatalf("%q %v", b, err)
	}
	freed, err := root.RemoveKeptRegadom()
	if err != nil || freed != 7 {
		t.Fatal(freed, err)
	}
	if root.KeptRegadom().Present {
		t.Fatal("still there")
	}
}
