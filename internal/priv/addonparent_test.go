package priv

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// occulited B-145: the helper takes go-w off exactly the six parents of the addons' directories - root's (here: the test's own user, as the helper's), never
// through a link, never a bit added, never another bit taken.
func TestAddonParentTighten(t *testing.T) {
	root := t.TempDir()
	at := func(rel string) string { return filepath.Join(root, rel) }
	// deepest first, so a parent's 0777 is set after its children exist
	for _, d := range []string{"usr/local/etc/config/addons/www", "usr/local/addons", "usr/local/bin", "etc/config"} {
		if err := os.MkdirAll(at(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	modeOf := func(rel string) uint32 {
		t.Helper()
		var st unix.Stat_t
		if err := unix.Lstat(at(rel), &st); err != nil {
			t.Fatal(err)
		}
		return st.Mode & 0o7777
	}
	chmod := func(rel string, m os.FileMode) {
		t.Helper()
		if err := os.Chmod(at(rel), m); err != nil {
			t.Fatal(err)
		}
	}
	srv := &Server{Policy: DefaultPolicy(root, "/usr/local/etc/occulite")}
	ctx := context.Background()
	do := func(rel string, mode uint32) response {
		return srv.do(ctx, request{Op: opAddonParentTighten, Path: at(rel), Mode: mode})
	}

	// every parent on the list: 0777 → 0755
	tight := uint32(0o755)
	for _, p := range AddonParentDirs {
		rel := strings.TrimPrefix(p, "/")
		chmod(rel, 0o777)
		if r := do(rel, tight); r.Error != "" || !r.OK {
			t.Errorf("%s: %+v", p, r)
		}
		if m := modeOf(rel); m != tight {
			t.Errorf("%s: mode %04o", p, m)
		}
	}
	// the same mode again is no change and no error
	if r := do("usr/local", tight); r.Error != "" {
		t.Errorf("unchanged: %+v", r)
	}
	// o+w alone, g+w kept: allowed (only bits taken away)
	chmod("usr/local/addons", 0o777)
	if r := do("usr/local/addons", 0o775); r.Error != "" {
		t.Errorf("o-w alone: %+v", r)
	}
	if m := modeOf("usr/local/addons"); m != 0o775 {
		t.Errorf("o-w alone: mode %04o", m)
	}
	// a sticky bit stays where it was asked to stay
	if err := unix.Chmod(at("usr/local/addons"), 0o1777); err != nil {
		t.Fatal(err)
	}
	if r := do("usr/local/addons", 0o1000|tight); r.Error != "" {
		t.Errorf("sticky kept: %+v", r)
	}
	if m := modeOf("usr/local/addons"); m != 0o1000|tight {
		t.Errorf("sticky kept: mode %04o", m)
	}

	// a mode that adds a bit is refused, the mode stays
	chmod("usr/local/etc", 0o755)
	for _, m := range []uint32{0o777, 0o757, 0o4755, 0o1755} {
		if r := do("usr/local/etc", m); r.Error == "" || !strings.Contains(r.Error, "would add") {
			t.Errorf("add %04o: %+v", m, r)
		}
	}
	// a mode that takes another bit than o+w/g+w is refused
	chmod("usr/local/etc", 0o777)
	for _, m := range []uint32{0o700, 0o750, 0o555, 0o0, 0o771} {
		if r := do("usr/local/etc", m); r.Error == "" || !strings.Contains(r.Error, "would take") {
			t.Errorf("take %04o: %+v", m, r)
		}
	}
	// not a permission mode
	if r := do("usr/local/etc", 0o40755); r.Error == "" {
		t.Errorf("file type bits: %+v", r)
	}
	if m := modeOf("usr/local/etc"); m != 0o777 {
		t.Errorf("refused, yet mode %04o", m)
	}

	// paths outside the list: refused at the boundary
	chmod("usr/local/bin", 0o777)
	for _, rel := range []string{"usr/local/bin", "usr/local/etc/config/rc.d", "usr/local/addons/x", "usr", "etc/config", "usr/local/etc/config/addons/www/x"} {
		if r := do(rel, 0o755); !strings.HasPrefix(r.Error, "refused") {
			t.Errorf("%s: %+v", rel, r)
		}
	}
	if r := srv.do(ctx, request{Op: opAddonParentTighten, Path: "/usr/local", Mode: 0o755}); !strings.HasPrefix(r.Error, "refused") {
		t.Errorf("outside the root: %+v", r)
	}
	if m := modeOf("usr/local/bin"); m != 0o777 {
		t.Errorf("outside the list changed: %04o", m)
	}
	// more than a path and a mode
	if r := srv.do(ctx, request{Op: opAddonParentTighten, Path: at("usr/local"), Mode: 0o755, Target: "x"}); !strings.HasPrefix(r.Error, "refused") {
		t.Errorf("extra field: %+v", r)
	}

	// a link as the parent itself: not followed, the target keeps its mode
	elsewhere := at("elsewhere")
	if err := os.Mkdir(elsewhere, 0o777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(elsewhere, 0o777)
	if err := os.RemoveAll(at("usr/local/etc/config/addons/www")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, at("usr/local/etc/config/addons/www")); err != nil {
		t.Fatal(err)
	}
	if r := do("usr/local/etc/config/addons/www", 0o755); r.Error == "" {
		t.Errorf("link followed: %+v", r)
	}
	// a link on the way: /usr/local/etc/config → elsewhere
	if err := os.RemoveAll(at("usr/local/etc/config")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(elsewhere, "addons"), 0o777); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(elsewhere, "addons"), 0o777)
	if err := os.Symlink(elsewhere, at("usr/local/etc/config")); err != nil {
		t.Fatal(err)
	}
	if r := do("usr/local/etc/config/addons", 0o755); r.Error == "" {
		t.Errorf("link on the way followed: %+v", r)
	}
	for _, p := range []string{elsewhere, filepath.Join(elsewhere, "addons")} {
		var st unix.Stat_t
		_ = unix.Stat(p, &st)
		if st.Mode&0o7777 != 0o777 {
			t.Errorf("%s changed through a link: %04o", p, st.Mode&0o7777)
		}
	}
}

// the rule itself, with the owners a test cannot create: root's directory, another owner, a file.
func TestTightenAllowed(t *testing.T) {
	dir := uint32(unix.S_IFDIR)
	for _, c := range []struct {
		name        string
		mode, uid   uint32
		want        uint32
		ok          bool
		errContains string
	}{
		{"o-w", dir | 0o777, 0, 0o775, true, ""},
		{"go-w", dir | 0o777, 0, 0o755, true, ""},
		{"g-w", dir | 0o775, 0, 0o755, true, ""},
		{"sticky kept", dir | 0o1777, 0, 0o1755, true, ""},
		{"sticky taken", dir | 0o1777, 0, 0o755, false, "would take"},
		{"setgid kept", dir | 0o2777, 0, 0o2755, true, ""},
		{"unchanged", dir | 0o755, 0, 0o755, true, ""},
		{"not root's", dir | 0o777, 1000, 0o755, false, "not by root"},
		{"adds o+w", dir | 0o755, 0, 0o757, false, "would add"},
		{"adds setuid", dir | 0o755, 0, 0o4755, false, "would add"},
		{"takes r", dir | 0o777, 0, 0o771, false, "would take"},
		{"takes x", dir | 0o777, 0, 0o754, false, "would take"},
		{"a file", unix.S_IFREG | 0o777, 0, 0o755, false, "not a directory"},
		{"a link", unix.S_IFLNK | 0o777, 0, 0o755, false, "not a directory"},
		{"type bits", dir | 0o777, 0, dir | 0o755, false, "not a permission mode"},
	} {
		err := tightenAllowed(c.mode, c.uid, c.want, 0)
		if (err == nil) != c.ok || (err != nil && !strings.Contains(err.Error(), c.errContains)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

// the Client reaches the operation over the socket
func TestAddonParentTightenClient(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "usr/local/addons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(root, "usr/local"), 0o777)
	c, _ := logListHelper(t, root, nil)
	if err := c.TightenAddonParent(filepath.Join(root, "usr/local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(root, "usr/local")); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode %v", fi.Mode())
	}
	if err := c.TightenAddonParent(filepath.Join(root, "usr/local/addons/x"), 0o755); err == nil {
		t.Error("outside the list")
	}
}
