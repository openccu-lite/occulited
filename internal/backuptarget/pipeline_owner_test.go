package backuptarget

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// occulited B-152: the root half makes every level of backup-targets/<id> occulite's with mode
// 0700 - a first run on a fresh state directory, and a repair of what an earlier run left root's -
// so the daemon reads last.json afterwards. Chown is recorded: the tests do not run as root.
func TestPipelineSecretDirsOwned(t *testing.T) {
	cases := []struct {
		name string
		// prepare lays out the state directory before the run; the paths it returns must be
		// given to occulite (besides the target's folder and backup-targets/).
		prepare  func(t *testing.T, state string) []string
		instance string
		// result: the run writes the directory target's last.json
		result bool
	}{
		{name: "first run, fresh state directory", instance: "all", result: true,
			prepare: func(*testing.T, string) []string { return nil }},
		{name: "root-owned parent of an earlier run, mode 0700, target folder there", instance: "all", result: true,
			prepare: func(t *testing.T, state string) []string {
				mkdir(t, filepath.Join(state, SecretsDir, DirectoryID), 0o700)
				return nil
			}},
		{name: "repair with the nightly switch off: nothing else happens", instance: "nightly",
			prepare: func(t *testing.T, state string) []string {
				mkdir(t, filepath.Join(state, SecretsDir), 0o755)
				mkdir(t, filepath.Join(state, SecretsDir, DirectoryID), 0o750)
				other := filepath.Join(state, SecretsDir, "sftp1")
				mkdir(t, other, 0o700)
				if err := os.WriteFile(filepath.Join(state, SecretsDir, "stray"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{other}
			}},
	}
	const uid, gid = 4711, 4712
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			old := checkDir
			checkDir = func(Target) (string, error) { return "", nil }
			t.Cleanup(func() { checkDir = old })
			state := t.TempDir()
			st := &Store{Dir: state, Root: t.TempDir()}
			if _, err := st.Put(Target{Kind: KindDirectory, Name: "USB", Enabled: true, MaxBackups: 2, Directory: &Directory{Path: filepath.Join(t.TempDir(), "backup")}}); err != nil {
				t.Fatal(err)
			}
			if c.instance == "nightly" {
				if err := st.SetNightly(false); err != nil {
					t.Fatal(err)
				}
			}
			extra := c.prepare(t, state)
			owned := map[string][2]int{}
			p := &Pipeline{Store: st, Staging: filepath.Join(t.TempDir(), "staging"), Hostname: "lab-box", Version: "1.0.0",
				UID: uid, GID: gid,
				Chown: func(path string, u, g int) error { owned[path] = [2]int{u, g}; return nil },
				Now:   func() time.Time { return time.Date(2026, 11, 19, 0, 7, 0, 0, time.UTC) },
				Create: func(_ context.Context, path string) error {
					return os.WriteFile(path, []byte("backup"), 0o600)
				}}
			if err := p.CreateRun(context.Background(), c.instance); err != nil {
				t.Fatal(err)
			}
			dirs := append([]string{filepath.Join(state, SecretsDir), st.SecretDir(DirectoryID)}, extra...)
			for _, d := range dirs {
				fi, err := os.Lstat(d)
				if err != nil {
					t.Fatal(err)
				}
				if fi.Mode().Perm() != 0o700 || !fi.IsDir() {
					t.Errorf("%s: mode %v, want a 0700 directory", d, fi.Mode())
				}
				if owned[d] != [2]int{uid, gid} {
					t.Errorf("%s not given to occulite: %v", d, owned)
				}
			}
			if _, ok := owned[state]; ok {
				t.Error("the state directory itself was chowned")
			}
			if _, ok := owned[filepath.Join(state, SecretsDir, "stray")]; ok {
				t.Error("a file in backup-targets/ was chowned by the repair")
			}
			last := filepath.Join(st.SecretDir(DirectoryID), LastFile)
			if !c.result {
				if _, err := os.Stat(last); err == nil {
					t.Fatal("last.json written although nothing ran")
				}
				return
			}
			if owned[last] != [2]int{uid, gid} {
				t.Errorf("last.json not given to occulite: %v", owned)
			}
			// the daemon's read (Manager.view) finds the result
			if r, ok := ReadResult(st.SecretDir(DirectoryID)); !ok || !r.OK {
				t.Fatalf("last.json: %+v %v", r, ok)
			}
		})
	}
}

// A backup-targets/ that is a link is not followed: no result is written through it.
func TestPipelineSecretDirLink(t *testing.T) {
	state, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(state, SecretsDir)); err != nil {
		t.Fatal(err)
	}
	p := &Pipeline{Store: &Store{Dir: state}, Chown: func(string, int, int) error { return nil }}
	if _, err := p.secretDir(DirectoryID); err == nil {
		t.Fatal("a linked backup-targets/ was accepted")
	}
	p.writeResult(Target{ID: DirectoryID}, Result{OK: true})
	if left, _ := os.ReadDir(elsewhere); len(left) != 0 {
		t.Fatalf("written through the link: %v", left)
	}
	// a missing state directory is not made by root
	p.Store.Dir = filepath.Join(state, "missing")
	if _, err := p.secretDir(DirectoryID); err == nil {
		t.Fatal("a missing state directory was made")
	}
}

func mkdir(t *testing.T, dir string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
}
