package priv

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// occulited B-153: the tag goes where the write test may run, never onto a share or elsewhere, and
// a request of any other shape is refused before it reaches the operation.

type recordTag struct {
	Local
	calls []string
}

func (r *recordTag) BackupTag(_ context.Context, dir string) error {
	r.calls = append(r.calls, dir)
	return nil
}

func TestBackupTagBoundary(t *testing.T) {
	root := t.TempDir()
	sock := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ops := &recordTag{}
	srv := &Server{Policy: DefaultPolicy(root, "/usr/local/etc/occulite"), Ops: ops}
	go func() { _ = srv.Serve(ctx, l) }()
	c := Client{Socket: sock}
	for _, d := range []string{"media/usb1", "usr/local/sdcard/backup", "etc"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "usr/local/sdcard"), filepath.Join(root, "media/usb0")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "etc"), filepath.Join(root, "media/evil")); err != nil {
		t.Fatal(err)
	}
	good := []string{"/usr/local/backup", "/usr/local/backup/b153", "/media/usb1/backup", "/media/usb0/backup"}
	for _, ok := range good {
		if err := c.BackupTag(ctx, filepath.Join(root, ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"/media/net/nas1/lab", "/media/net", "/etc", "/usr/local/etc", "/usr/local", "/usr/local/backupx", "/media/evil/x", "/media/../etc", "/media/usb1/", "media/usb1"} {
		p := bad
		if strings.HasPrefix(bad, "/") {
			p = root + bad
		}
		if err := c.BackupTag(ctx, p); !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if _, err := c.call(ctx, request{Op: opBackupTag, Path: filepath.Join(root, "usr/local/backup"), Name: "x"}); !errors.Is(err, ErrRefused) {
		t.Errorf("a request with more than a path: %v", err)
	}
	if len(ops.calls) != len(good) {
		t.Fatalf("calls %q", ops.calls)
	}
}

// The operation itself: the folder made, the tag an empty regular file, idempotent, the content
// of an existing tag left alone; a link or a FIFO in the tag's place is not written through.
func TestBackupTagLocal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usr", "local", "backup", "b153")
	for i := 0; i < 2; i++ {
		if err := (Local{}).BackupTag(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
	}
	fi, err := os.Lstat(filepath.Join(dir, NoBackupTag))
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Fatalf("%v %v", fi, err)
	}
	if err := os.WriteFile(filepath.Join(dir, NoBackupTag), []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := TagNoBackup(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, NoBackupTag)); string(b) != "kept" {
		t.Fatalf("an existing tag changed: %q", b)
	}
	if err := (Local{}).BackupTag(context.Background(), "relative"); err == nil {
		t.Fatal("a relative path was taken")
	}

	for _, tc := range []struct {
		name  string
		plant func(path string) error
	}{
		{"link", func(path string) error {
			return os.Symlink(filepath.Join(filepath.Dir(path), "elsewhere"), path)
		}},
		{"fifo", func(path string) error { return syscall.Mkfifo(path, 0o644) }},
		{"directory", func(path string) error { return os.Mkdir(path, 0o755) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := t.TempDir()
			if err := tc.plant(filepath.Join(d, NoBackupTag)); err != nil {
				t.Fatal(err)
			}
			if err := TagNoBackup(d); err == nil {
				t.Fatal("written")
			}
			if _, err := os.Lstat(filepath.Join(d, "elsewhere")); err == nil {
				t.Fatal("the link was followed")
			}
		})
	}
}
