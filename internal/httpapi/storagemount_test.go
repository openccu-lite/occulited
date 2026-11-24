package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/system"
)

// occulited task 160: the userfs's mount options read from the mount table, set through the
// remount, refused where the userfs is no ext4 mount.
func TestStorageMountRoutes(t *testing.T) {
	dir := t.TempDir()
	table := filepath.Join(dir, "mounts")
	write := func(fstype, opts string) {
		if err := os.WriteFile(table, []byte(fmt.Sprintf("/dev/root / ext4 ro 0 0\n/dev/mmcblk0p3 /usr/local %s %s 0 0\n", fstype, opts)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ext4", "rw,noatime,nodiratime,commit=30")
	fail := false
	um := &system.UserfsMount{File: filepath.Join(dir, "userfs-mount.json"), Mounts: table,
		Set: func(_ context.Context, lazytime bool, commit int) error {
			if fail {
				return errors.New("mount: /usr/local is busy")
			}
			o := "rw,noatime,nodiratime"
			if lazytime {
				o = "rw,lazytime,noatime,nodiratime"
			}
			write("ext4", fmt.Sprintf("%s,commit=%d", o, commit))
			return nil
		}}
	mux := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t), UserfsMount: um}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	const path = "/api/system/v1/storage/mount-options"
	opt := func(out map[string]any, key string) string {
		m, _ := out[key].(map[string]any)
		return fmt.Sprintf("%v/%v", m["lazytime"], m["commit_s"])
	}

	st, out, _ := do(t, srv, "GET", path, "", nil)
	if st != 200 || out["supported"] != true || out["mounted"] != true || out["fstype"] != "ext4" || out["mount"] != "/usr/local" ||
		out["options"] != "rw,noatime,nodiratime,commit=30" || opt(out, "active") != "false/30" || opt(out, "setting") != "false/30" ||
		opt(out, "default") != "false/30" || out["custom"] != false || fmt.Sprint(out["commit_choices"]) != "[5 30 60 120]" {
		t.Fatalf("the image's: %d %v", st, out)
	}
	for _, body := range []string{`{}`, `{"lazytime":true}`, `{"commit_s":60}`, `{"lazytime":true,"commit_s":45}`, `{"lazytime":true,"commit_s":0}`} {
		if st, out, _ := do(t, srv, "PUT", path, body, nil); st != 422 || out["error"] != "invalid-body" {
			t.Errorf("%s: %d %v", body, st, out)
		}
	}
	st, out, _ = do(t, srv, "PUT", path, `{"lazytime":true,"commit_s":120}`, nil)
	if st != 200 || opt(out, "active") != "true/120" || opt(out, "setting") != "true/120" || out["custom"] != true || out["options"] != "rw,lazytime,noatime,nodiratime,commit=120" {
		t.Fatalf("set: %d %v", st, out)
	}
	if st, out, _ = do(t, srv, "GET", path, "", nil); st != 200 || opt(out, "setting") != "true/120" {
		t.Errorf("read back: %d %v", st, out)
	}
	// a remount the kernel refuses: 500, the choice stays
	fail = true
	if st, out, _ := do(t, srv, "PUT", path, `{"lazytime":false,"commit_s":60}`, nil); st != 500 || out["error"] != "internal" {
		t.Errorf("a failed remount: %d %v", st, out)
	}
	fail = false
	if _, out, _ = do(t, srv, "GET", path, "", nil); opt(out, "setting") != "true/120" || opt(out, "active") != "true/120" {
		t.Errorf("after the failed remount: %v", out)
	}
	if st, out, _ = do(t, srv, "PUT", path, `{"lazytime":false,"commit_s":30}`, nil); st != 200 || out["custom"] != false || opt(out, "active") != "false/30" {
		t.Errorf("back to the image's: %d %v", st, out)
	}

	// no ext4: shown, not settable
	write("f2fs", "rw,noatime")
	if st, out, _ = do(t, srv, "GET", path, "", nil); st != 200 || out["supported"] != false || out["fstype"] != "f2fs" {
		t.Errorf("f2fs: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "PUT", path, `{"lazytime":true,"commit_s":60}`, nil); st != 409 || out["error"] != "unsupported" {
		t.Errorf("f2fs, set: %d %v", st, out)
	}

	// a system without the setting
	mux2 := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t)}).Register(mux2)
	srv2 := httptest.NewServer(mux2)
	t.Cleanup(srv2.Close)
	if st, out, _ := do(t, srv2, "GET", path, "", nil); st != 200 || out["supported"] != false || opt(out, "default") != "false/30" {
		t.Errorf("no setting: %d %v", st, out)
	}
	if st, _, _ := do(t, srv2, "PUT", path, `{"lazytime":true,"commit_s":60}`, nil); st != 501 {
		t.Errorf("no setting, set: %d", st)
	}
}
