package priv

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// occulited B-146: the estimate walks /usr/local as createBackup.sh's tar does - the top-level
// exclusions, --exclude-tag=.nobackup - and names the largest directories.
func TestBackupEstimate(t *testing.T) {
	root := t.TempDir()
	w := func(p string, n int) {
		t.Helper()
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("etc/config/rfd.conf", 100)
	w("etc/config/.hidden", 7) // a dot file below the top is in the archive
	w("addons/redmatic/var/flows.json", 1000)
	w("addons/redmatic/lib/.nobackup", 0)   // the tag itself is a file of size 0
	w("addons/redmatic/lib/big.bin", 50000) // left out with its directory's content
	w("leftover/old/a.sbk", 2<<20)
	w("leftover/old/b.sbk", 2<<20)
	w("leftover/c", 10)
	w("tmp/occulite-backup/x.sbk", 70000) // --exclude=usr/local/tmp
	w(".firmwareUpdate", 70000)           // --exclude=usr/local/.*
	w(".cache/x", 70000)
	w("lost+found/x", 70000)
	w("eQ-3-Backup/x", 70000)
	w("toplevel.txt", 3)
	w("big-leftover.tar", 3<<20) // a file directly below the root is named itself
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "etc/link")); err != nil {
		t.Fatal(err)
	}

	old := BackupEstimateRoot
	BackupEstimateRoot = root
	t.Cleanup(func() { BackupEstimateRoot = old })
	c, sock := logListHelper(t, t.TempDir(), nil)
	est, err := c.BackupEstimate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if est.Bytes != 100+7+1000+2<<20+2<<20+10+3+3<<20 || est.Files != 8 || est.Partial {
		t.Errorf("bytes %d files %d partial %v", est.Bytes, est.Files, est.Partial)
	}
	if est.Free <= 0 {
		t.Errorf("free %d", est.Free)
	}
	// leftover is named by its subdirectory (most of it), nothing under 1 MiB is named
	want := []BackupDirSize{{Path: filepath.Join(root, "leftover/old"), Bytes: 4 << 20}, {Path: filepath.Join(root, "big-leftover.tar"), Bytes: 3 << 20}}
	if !reflect.DeepEqual(est.Largest, want) {
		t.Errorf("largest %+v", est.Largest)
	}
	// the operation takes nothing
	if res, _ := rawLogList(t, sock, request{Op: opBackupEstimate, Path: "/etc"}); res.OK || !strings.Contains(res.Error, "takes nothing") {
		t.Errorf("with a path: %+v", res)
	}
}

func TestBackupEstimateRunsOutOfTime(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	est, err := estimateBackup(ctx, root)
	if err != nil || !est.Partial {
		t.Errorf("%+v %v", est, err)
	}
	if _, err := estimateBackup(context.Background(), filepath.Join(root, "missing")); err == nil {
		t.Error("a missing root answered")
	}
}

func TestLargestDirsBound(t *testing.T) {
	const m = 1 << 20
	sizes := map[string]int64{"a": 10 * m, "b": 20 * m, "c": 30 * m, "d": 40 * m, "e": 50 * m, "f": 60 * m, "f/x": 29 * m, "f/y": 31 * m, "g": 1}
	got := largestDirs("/r", sizes, 5)
	var names []string
	for _, d := range got {
		names = append(names, d.Path)
	}
	if strings.Join(names, " ") != "/r/e /r/d /r/f/y /r/c /r/b" {
		t.Errorf("%v", names)
	}
}

// the backup script runs in its low-priority unit where systemd runs, unchanged elsewhere; no
// other program is touched
func TestBackupScriptCommand(t *testing.T) {
	old := backupSystemdDir
	t.Cleanup(func() { backupSystemdDir = old })
	backupSystemdDir = t.TempDir()
	name, args := backupScriptCommand("/bin/createBackup.sh", []string{"/usr/local/tmp/x.sbk"})
	line := name + " " + strings.Join(args, " ")
	for _, want := range []string{"systemd-run --wait --pipe --collect --quiet --unit=occulite-backup-script", "-p IOSchedulingClass=idle", "-p Nice=19", "-p CPUWeight=idle", "-p MemoryHigh=256M", "-- /bin/createBackup.sh /usr/local/tmp/x.sbk"} {
		if !strings.Contains(line, want) {
			t.Errorf("%q lacks %q", line, want)
		}
	}
	if name, args := backupScriptCommand("/bin/restoreBackup.sh", []string{"-c", "x"}); name != "/bin/restoreBackup.sh" || len(args) != 2 {
		t.Errorf("another program: %s %v", name, args)
	}
	backupSystemdDir = filepath.Join(backupSystemdDir, "none")
	if name, args := backupScriptCommand("/bin/createBackup.sh", []string{"x"}); name != "/bin/createBackup.sh" || len(args) != 1 {
		t.Errorf("without systemd: %s %v", name, args)
	}
}
