package priv

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ioDropIn(t *testing.T) string {
	t.Helper()
	old := IOAccountingDropIn
	IOAccountingDropIn = filepath.Join(t.TempDir(), "system.conf.d", "50-occulite-io-accounting.conf")
	t.Cleanup(func() { IOAccountingDropIn = old })
	return IOAccountingDropIn
}

func TestSetIOAccountingLocal(t *testing.T) {
	path := ioDropIn(t)
	ctx := context.Background()
	if err := (Local{}).SetIOAccounting(ctx, false); err != nil {
		t.Fatalf("off with nothing there: %v", err)
	}
	if err := (Local{}).SetIOAccounting(ctx, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(b), "[Manager]\nDefaultIOAccounting=yes\n") {
		t.Fatalf("the drop-in: %q %v", b, err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode())
	}
	// on again leaves the file alone: systemd need not be reloaded for it
	before := fi.ModTime()
	if err := (Local{}).SetIOAccounting(ctx, true); err != nil {
		t.Fatal(err)
	}
	if fi, _ = os.Stat(path); !fi.ModTime().Equal(before) {
		t.Errorf("an unchanged drop-in was written again")
	}
	// a link in its place is replaced, never written through
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	if err := (Local{}).SetIOAccounting(ctx, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(victim); string(b) != "untouched" {
		t.Errorf("the link's target was written: %q", b)
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the drop-in is not a regular file: %v %v", fi, err)
	}
	if err := (Local{}).SetIOAccounting(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after off: %v", err)
	}
}

// Through the helper: the switch alone, a request with anything else is refused.
func TestIOAccountingOperation(t *testing.T) {
	path := ioDropIn(t)
	sock := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logged []string
	srv := &Server{Policy: DefaultPolicy("/", "/usr/local/etc/occulite"), Log: func(f string, a ...any) { logged = append(logged, f) }}
	go func() { _ = srv.Serve(ctx, l) }()
	c := Client{Socket: sock}
	if err := c.SetIOAccounting(ctx, true); err != nil {
		t.Fatalf("on through the helper: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("after on: %v", err)
	}
	if err := c.SetIOAccounting(ctx, false); err != nil {
		t.Fatalf("off through the helper: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("after off: %v", err)
	}
	for _, req := range []request{
		{Op: opIOAccounting, Enabled: true, Path: "/run/systemd/system.conf.d/other.conf"},
		{Op: opIOAccounting, Enabled: true, Data: []byte("[Manager]\nDefaultEnvironment=X=1\n")},
	} {
		if _, err := c.call(ctx, req); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("a request with more than the switch: %v", err)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused request wrote the drop-in")
	}
	if !strings.Contains(strings.Join(logged, "\n"), "io-accounting") {
		t.Errorf("the refusal was not logged: %v", logged)
	}
}
