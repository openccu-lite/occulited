package backuptarget

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// occulited B-153: a directory target on the userfs lies inside the tree createBackup.sh archives,
// so every backup took in the earlier ones. The folder carries .nobackup - written when the target
// is saved (through the helper) and by the root half of every run - and the script's
// --exclude-tag=.nobackup leaves its contents out.

func tagged(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, priv.NoBackupTag))
	return err == nil && fi.Mode().IsRegular()
}

// The save: a directory target's folder through the helper, nothing for the other kinds or a path
// outside the write test's, nothing for a stick that is not plugged in.
func TestManagerTag(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		target  Target
		root    string // "/" = the system itself
		noStick bool
		want    string // the helper's call, "" = none
	}{
		{"userfs", Target{Kind: KindDirectory, Directory: &Directory{Path: "/usr/local/backup/b153"}}, "", false, "tag /usr/local/backup/b153"},
		{"userfs root", Target{Kind: KindDirectory, Directory: &Directory{Path: "/usr/local/backup"}}, "", false, "tag /usr/local/backup"},
		{"stick", Target{Kind: KindDirectory, Directory: &Directory{Path: "/media/usb1/backup"}}, "", false, "tag /media/usb1/backup"},
		{"stick missing", Target{Kind: KindDirectory, Directory: &Directory{Path: "/media/usb1/backup"}}, "/", true, ""},
		{"outside", Target{Kind: KindDirectory, Directory: &Directory{Path: "/usr/local/etc"}}, "", false, ""},
		{"no path", Target{Kind: KindDirectory}, "", false, ""},
		{"share", Target{Kind: KindShare, Share: &ShareRef{ID: "nas", Folder: "b"}}, "", false, ""},
		{"sftp", Target{Kind: KindSFTP, SFTP: &SFTP{Host: "nas", User: "b"}}, "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, h, _ := newManager(t)
			if tc.root != "" {
				m.Root = tc.root
			}
			old := noMediumAt
			t.Cleanup(func() { noMediumAt = old })
			noMediumAt = func(Target) (bool, error) { return tc.noStick, errors.New("no stick") }
			if err := m.Tag(ctx, tc.target); err != nil {
				t.Fatal(err)
			}
			want := []string{}
			if tc.want != "" {
				want = append(want, "tag "+m.join(strings.TrimPrefix(tc.want, "tag ")))
			}
			if strings.Join(h.calls, "|") != strings.Join(want, "|") {
				t.Fatalf("calls %q, want %q", h.calls, want)
			}
		})
	}
}

// The run: every directory target's existing folder at the start (a target saved before the tag
// existed, a switched-off one), a folder the probe has just made before the backup is created,
// never a folder that is not there.
func TestPipelineTagsDirectoryTargets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		exists   bool
		instance string
		want     bool
	}{
		{"enabled, existing folder", true, true, "all", true},
		{"enabled, new folder (made by the probe)", true, false, "all", true},
		{"switched off, existing folder", false, true, "all", true},
		{"switched off, no folder", false, false, "all", false},
		{"nightly switched off, existing folder", true, true, "nightly-off", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newPipeRig(t, false)
			ctx := context.Background()
			d, _, _ := r.p.Store.Get(DirectoryID)
			d.Enabled = tc.enabled
			if _, err := r.p.Store.Put(d); err != nil {
				t.Fatal(err)
			}
			if tc.exists {
				if err := os.MkdirAll(r.usb, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			var atCreate bool
			r.p.Create = func(_ context.Context, path string) error {
				atCreate = tagged(r.usb)
				return os.WriteFile(path, r.content, 0o600)
			}
			instance := tc.instance
			if instance == "nightly-off" {
				if err := r.p.Store.SetNightly(false); err != nil {
					t.Fatal(err)
				}
				instance = "nightly"
			}
			_ = r.p.CreateRun(ctx, instance)
			if got := tagged(r.usb); got != tc.want {
				t.Fatalf("tagged %v, want %v", got, tc.want)
			}
			if tc.enabled && instance == "all" && !atCreate {
				t.Fatal("the folder was not tagged when the backup was created")
			}
		})
	}
}

// gnuTar answers the path of a GNU tar (createBackup.sh's --exclude-tag is GNU's), or skips.
func gnuTar(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("tar")
	if err != nil {
		t.Skip("no tar")
	}
	out, err := exec.Command(p, "--version").Output()
	if err != nil || !bytes.Contains(out, []byte("GNU tar")) {
		t.Skip("not GNU tar")
	}
	return p
}

// tarList lists an archive's entries.
func tarList(t *testing.T, tar, archive string) []string {
	t.Helper()
	out, err := exec.Command(tar, "-tzf", archive).Output()
	if err != nil {
		t.Fatalf("tar -t %s: %v", archive, err)
	}
	return strings.Fields(string(out))
}

// Two runs into a directory target on the userfs, each backup made by tar the way createBackup.sh
// makes its usr_local.tar.gz (line 67 of the fork's script, minus the paths that are not here):
// the second archive holds no .sbk of the first, and the same tar without --exclude-tag would have
// (the control, so the test proves the tag and not an empty folder).
func TestPipelineSecondBackupHoldsNoEarlierOne(t *testing.T) {
	tar := gnuTar(t)
	old := checkDir
	checkDir = func(Target) (string, error) { return "", nil }
	t.Cleanup(func() { checkDir = old })
	root := t.TempDir()
	conf := filepath.Join(root, "usr/local/etc/config")
	if err := os.MkdirAll(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "rfd.conf"), []byte("Port = /dev/mmd_bidcos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "usr/local/backup/b153")
	st := &Store{Dir: t.TempDir(), Root: t.TempDir()}
	if _, err := st.Put(Target{Kind: KindDirectory, Name: "SD", Enabled: true, MaxBackups: 5, Directory: &Directory{Path: dir}}); err != nil {
		t.Fatal(err)
	}
	args := func(withTag bool, out string) []string {
		a := []string{"-C", root, "--owner=root", "--group=root", "--exclude=usr/local/tmp", "--exclude=usr/local/.*", "--exclude=usr/local/lost+found"}
		if withTag {
			a = append(a, "--exclude-tag=.nobackup")
		}
		return append(a, "--one-file-system", "-czf", out, "usr/local")
	}
	now := time.Date(2026, 11, 20, 0, 7, 0, 0, time.UTC)
	p := &Pipeline{Store: st, Staging: filepath.Join(t.TempDir(), "staging"), Hostname: "lab-box", Version: "2.0.0",
		Now: func() time.Time { return now },
		Create: func(ctx context.Context, path string) error {
			out, err := exec.CommandContext(ctx, tar, args(true, path)...).CombinedOutput()
			if err != nil {
				return errors.New(string(out))
			}
			return nil
		}}
	ctx := context.Background()
	if err := p.CreateRun(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(dir, "lab-box-2.0.0-2026-11-20-0007.sbk")
	if _, err := os.Stat(first); err != nil || !tagged(dir) {
		t.Fatalf("the first run: %v, tagged %v", err, tagged(dir))
	}
	now = now.Add(24 * time.Hour)
	if err := p.CreateRun(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "lab-box-2.0.0-2026-11-21-0007.sbk")
	entries := tarList(t, tar, second)
	var hasConf, hasFolder bool
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e, ".sbk"), strings.HasSuffix(e, ".sbk.age"), strings.HasSuffix(e, ".partial"):
			t.Fatalf("the second backup holds %s: %q", e, entries)
		case e == "usr/local/etc/config/rfd.conf":
			hasConf = true
		case e == "usr/local/backup/b153/":
			hasFolder = true
		}
	}
	if !hasConf || !hasFolder {
		t.Fatalf("the archive lacks the configuration or the folder itself: %q", entries)
	}
	// the control: without the tag's exclusion the same tree takes the first backup in
	control := filepath.Join(t.TempDir(), "control.tar.gz")
	if out, err := exec.Command(tar, args(false, control)...).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.Contains(strings.Join(tarList(t, tar, control), "\n"), "usr/local/backup/b153/lab-box-2.0.0-2026-11-20-0007.sbk") {
		t.Fatal("the control holds no earlier backup: the test proves nothing")
	}
}
