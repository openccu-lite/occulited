package system

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// failingTighten is the helper refusing the change (an old helper, a read-only userfs)
type failingTighten struct{ priv.Local }

func (failingTighten) TightenAddonParent(string, uint32) error { return errors.New("refused: op") }

// occulited B-145: the parents of the addons' directories lose o+w (and g+w of a group other than
// root's) at every start; what cannot be fixed that way is answered with its reason, and the live
// check sees what the walk would refuse.
func TestHardenAddonParents(t *testing.T) {
	me := uint32(os.Getuid())
	tight := func(m uint32) string { return octal(tightened(m)) }
	type want struct {
		fixed  []string // "path old new"
		unsafe []string // "path reason"
		live   []string // "path reason" before the hardening
		after  []string // the live check afterwards
	}
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		ops   priv.Ops
		owner uint32
		want  want
	}{
		{name: "all safe", setup: func(*testing.T, string) {}},
		{
			name:  "a world-writable /usr/local",
			setup: func(t *testing.T, root string) { chmodT(t, root, "/usr/local", 0o777) },
			want:  want{fixed: []string{"/usr/local 0777 " + tight(0o777)}, live: []string{"/usr/local mode"}},
		},
		{
			name: "three of them, one with the sticky bit",
			setup: func(t *testing.T, root string) {
				chmodT(t, root, "/usr/local/etc", 0o757)
				chmodT(t, root, "/usr/local/addons", 0o1777)
				chmodT(t, root, "/usr/local/etc/config/addons/www", 0o777)
			},
			want: want{
				fixed: []string{"/usr/local/etc 0757 0755", "/usr/local/addons 1777 " + tight(0o1777), "/usr/local/etc/config/addons/www 0777 " + tight(0o777)},
				live:  []string{"/usr/local/etc mode", "/usr/local/addons mode", "/usr/local/etc/config/addons/www mode"},
			},
		},
		{
			name:  "another owner: answered, not changed",
			setup: func(t *testing.T, root string) { chmodT(t, root, "/usr/local", 0o777) },
			owner: me + 1,
			want: want{
				unsafe: []string{"/usr/local owner", "/usr/local/etc owner", "/usr/local/addons owner", "/usr/local/etc/config owner", "/usr/local/etc/config/addons owner", "/usr/local/etc/config/addons/www owner"},
				live:   []string{"/usr/local owner", "/usr/local/etc owner", "/usr/local/addons owner", "/usr/local/etc/config owner", "/usr/local/etc/config/addons owner", "/usr/local/etc/config/addons/www owner"},
				after:  []string{"/usr/local owner", "/usr/local/etc owner", "/usr/local/addons owner", "/usr/local/etc/config owner", "/usr/local/etc/config/addons owner", "/usr/local/etc/config/addons/www owner"},
			},
		},
		{
			name: "a link to a world-writable directory: answered, neither followed nor changed",
			setup: func(t *testing.T, root string) {
				linkT(t, root, "/usr/local/etc/config/addons/www", "/elsewhere", 0o777)
			},
			want: want{unsafe: []string{"/usr/local/etc/config/addons/www link"}, live: []string{"/usr/local/etc/config/addons/www link"}, after: []string{"/usr/local/etc/config/addons/www link"}},
		},
		{
			name: "a link to a safe directory: fine",
			setup: func(t *testing.T, root string) {
				linkT(t, root, "/usr/local/etc/config/addons/www", "/elsewhere", 0o755)
			},
		},
		{
			name:  "the helper refuses: answered as chmod",
			setup: func(t *testing.T, root string) { chmodT(t, root, "/usr/local/addons", 0o777) },
			ops:   failingTighten{},
			want:  want{unsafe: []string{"/usr/local/addons chmod"}, live: []string{"/usr/local/addons mode"}, after: []string{"/usr/local/addons mode"}},
		},
		{
			name: "the ReGa runs: nothing done",
			setup: func(t *testing.T, root string) {
				chmodT(t, root, "/usr/local", 0o777)
				if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "bin/ReGaHss"), nil, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: want{live: []string{"/usr/local mode"}, after: []string{"/usr/local mode"}},
		},
		{
			name: "missing parents are skipped",
			setup: func(t *testing.T, root string) {
				if err := os.RemoveAll(filepath.Join(root, "usr/local/etc")); err != nil {
					t.Fatal(err)
				}
				chmodT(t, root, "/usr/local/addons", 0o777)
			},
			want: want{fixed: []string{"/usr/local/addons 0777 " + tight(0o777)}, live: []string{"/usr/local/addons mode"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			for _, d := range []string{"usr/local/etc/config/addons/www", "usr/local/addons"} {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range priv.AddonParentDirs {
				_ = os.Chmod(filepath.Join(root, d), 0o755)
			}
			c.setup(t, root)
			oldPriv, oldUID := Priv, parentRootUID
			t.Cleanup(func() { Priv, parentRootUID = oldPriv, oldUID })
			Priv = priv.Local{}
			if c.ops != nil {
				Priv = c.ops
			}
			parentRootUID = me
			if c.owner != 0 {
				parentRootUID = c.owner
			}
			r := Root(root)
			if got := reasons(r.UnsafeAddonParents()); !equalList(got, c.want.live) {
				t.Errorf("live before: %v, want %v", got, c.want.live)
			}
			fixed, unsafe := r.HardenAddonParents()
			var gotFixed []string
			for _, f := range fixed {
				gotFixed = append(gotFixed, f.Path+" "+octal(f.Old)+" "+octal(f.New))
				var st syscall.Stat_t
				if err := syscall.Stat(filepath.Join(root, f.Path), &st); err != nil || st.Mode&0o7777 != f.New {
					t.Errorf("%s: mode %04o, want %04o", f.Path, st.Mode&0o7777, f.New)
				}
			}
			if !equalList(gotFixed, c.want.fixed) {
				t.Errorf("fixed: %v, want %v", gotFixed, c.want.fixed)
			}
			if got := reasons(unsafe); !equalList(got, c.want.unsafe) {
				t.Errorf("unsafe: %v, want %v", got, c.want.unsafe)
			}
			for _, u := range unsafe {
				if u.Reason == "chmod" && u.Err == nil {
					t.Errorf("%s: no error kept", u.Path)
				}
				if len(u.Mode) != 4 {
					t.Errorf("%s: mode %q", u.Path, u.Mode)
				}
			}
			if got := reasons(r.UnsafeAddonParents()); !equalList(got, c.want.after) {
				t.Errorf("live after: %v, want %v", got, c.want.after)
			}
		})
	}
}

// the rule, with the owners and groups a test cannot create
func TestParentUnsafe(t *testing.T) {
	old := parentRootUID
	t.Cleanup(func() { parentRootUID = old })
	parentRootUID = 0
	for _, c := range []struct {
		mode, uid, gid uint32
		want           string
		tight          uint32
	}{
		{0o755, 0, 0, "", 0o755},
		{0o775, 0, 0, "", 0o755}, // root's group may write: the walk lets it pass, so it is left alone
		{0o775, 0, 20, "mode", 0o755},
		{0o777, 0, 0, "mode", 0o755}, // fixed: go-w, the bit of root's group as well (sshd's StrictModes)
		{0o777, 0, 20, "mode", 0o755},
		{0o1777, 0, 0, "mode", 0o1755}, // stricter than the walk: a sticky bit does not exempt
		{0o2757, 0, 0, "mode", 0o2755},
		{0o755, 1000, 0, "owner", 0o755},
		{0o777, 1000, 1000, "owner", 0o755},
	} {
		if got := parentUnsafe(c.mode, c.uid, c.gid); got != c.want {
			t.Errorf("%04o %d:%d: %q, want %q", c.mode, c.uid, c.gid, got, c.want)
		}
		if got := tightened(c.mode); got != c.tight {
			t.Errorf("%04o gid %d tightened: %04o, want %04o", c.mode, c.gid, got, c.tight)
		}
	}
}

func chmodT(t *testing.T, root, p string, m uint32) {
	t.Helper()
	if err := syscall.Chmod(filepath.Join(root, p), m); err != nil {
		t.Fatal(err)
	}
}

// linkT replaces p by a link to target (under root), made with mode m
func linkT(t *testing.T, root, p, target string, m uint32) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, target), 0o755); err != nil {
		t.Fatal(err)
	}
	chmodT(t, root, target, m)
	if err := os.RemoveAll(filepath.Join(root, p)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, target), filepath.Join(root, p)); err != nil {
		t.Fatal(err)
	}
}

func octal(m uint32) string { return fmt.Sprintf("%04o", m) }

func reasons(list []AddonParent) []string {
	var out []string
	for _, u := range list {
		out = append(out, u.Path+" "+u.Reason)
	}
	return out
}

func equalList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
