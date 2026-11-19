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

// occulited B-148: an uninstall takes the addon's account out of the two files again - exactly the
// lines of that name and that uid, in place - and removes its runtime directories.
func TestRemoveAddonUserOperation(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "etc"), 0o755)
	passwd, group := filepath.Join(root, "etc/passwd"), filepath.Join(root, "etc/group")
	const basePasswd = "root:x:0:0::/:/bin/sh\nocculite:x:8100:8100::/var/lib/occulite:/bin/false\n"
	const baseGroup = "root:x:0:\ncerts:x:8101:\n"
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	inode := func(p string) uint64 {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		return st.Sys().(*syscall.Stat_t).Ino
	}
	write(passwd, basePasswd)
	write(group, baseGroup)
	sock := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &Server{Policy: DefaultPolicy(root, "/usr/local/etc/occulite"), Log: func(string, ...any) {}}
	go func() { _ = srv.Serve(ctx, l) }()
	c := Client{Socket: sock}

	for _, a := range []struct {
		name string
		uid  int
	}{{"addon-mosq", 30004}, {"addon-sync", 30005}, {"addon-zz", 30006}} {
		if err := c.AddAddonUser(passwd, group, a.name, a.uid); err != nil {
			t.Fatal(err)
		}
	}
	pIno, gIno := inode(passwd), inode(group)
	// the middle one goes; the others and the files' own lines stay, the inodes are the same
	if err := c.RemoveAddonUser(passwd, group, "addon-sync", 30005); err != nil {
		t.Fatal(err)
	}
	wantPasswd := basePasswd + "addon-mosq:x:30004:30004::/usr/local/addons/mosq:/bin/false\naddon-zz:x:30006:30006::/usr/local/addons/zz:/bin/false\n"
	wantGroup := baseGroup + "addon-mosq:x:30004:\naddon-zz:x:30006:\n"
	if got := read(passwd); got != wantPasswd {
		t.Errorf("passwd:\n%s", got)
	}
	if got := read(group); got != wantGroup {
		t.Errorf("group:\n%s", got)
	}
	if inode(passwd) != pIno || inode(group) != gIno {
		t.Error("the files were replaced rather than edited in place")
	}
	// gone already, or the name with another uid: nothing changes, no error
	for _, uid := range []int{30005, 30009} {
		if err := c.RemoveAddonUser(passwd, group, "addon-sync", uid); err != nil {
			t.Errorf("%d: %v", uid, err)
		}
	}
	if err := c.RemoveAddonUser(passwd, group, "addon-mosq", 30006); err != nil {
		t.Error(err)
	}
	if got := read(passwd); got != wantPasswd {
		t.Errorf("passwd after the misses:\n%s", got)
	}
	// the last line, and a group line with members: by name and number, whatever follows
	write(group, baseGroup+"addon-mosq:x:30004:www-data\naddon-zz:x:30006:")
	if err := c.RemoveAddonUser(passwd, group, "addon-zz", 30006); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveAddonUser(passwd, group, "addon-mosq", 30004); err != nil {
		t.Fatal(err)
	}
	if got := read(passwd); got != basePasswd {
		t.Errorf("passwd at the end:\n%s", got)
	}
	if got := read(group); got != baseGroup {
		t.Errorf("group at the end:\n%q", got)
	}
	// the boundary is the creation's
	for name, try := range map[string]func() error{
		"passwd elsewhere": func() error {
			return c.RemoveAddonUser(filepath.Join(root, "etc/config/passwd"), group, "addon-a", 30010)
		},
		"files swapped":  func() error { return c.RemoveAddonUser(group, passwd, "addon-a", 30010) },
		"not an addon":   func() error { return c.RemoveAddonUser(passwd, group, "occulite", 30010) },
		"uid below base": func() error { return c.RemoveAddonUser(passwd, group, "addon-a", 8100) },
		"uid zero":       func() error { return c.RemoveAddonUser(passwd, group, "addon-a", 0) },
	} {
		if err := try(); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := (Local{}).RemoveAddonUser(passwd, group, "root", 0); err == nil {
		t.Error("Local took a non-addon account")
	}
	if got := read(passwd); got != basePasswd {
		t.Errorf("passwd after the refusals:\n%s", got)
	}
	if err := (Local{}).RemoveAddonUser(filepath.Join(root, "missing"), group, "addon-a", 30010); err == nil {
		t.Error("a missing file is an error")
	}
}

func TestRemoveAddonRuntimeDirs(t *testing.T) {
	base1, base2 := t.TempDir(), t.TempDir()
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the addon's directory with a socket-like file and a link to outside in it; another addon's
	for _, p := range []string{filepath.Join(base1, "sync"), filepath.Join(base1, "other")} {
		if err := os.MkdirAll(p, 0o770); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(p, "provider.sock"), nil, 0o600)
	}
	if err := os.Symlink(outside, filepath.Join(base1, "sync", "out")); err != nil {
		t.Fatal(err)
	}
	// a link in the directory's place: removed, not followed
	if err := os.Symlink(outside, filepath.Join(base2, "sync")); err != nil {
		t.Fatal(err)
	}
	if err := removeAddonRuntimeDirs([]string{base1, base2, filepath.Join(t.TempDir(), "none")}, "sync"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(base1, "sync"), filepath.Join(base2, "sync")} {
		if _, err := os.Lstat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s left: %v", p, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("a link was followed")
	}
	if _, err := os.Stat(filepath.Join(base1, "other", "provider.sock")); err != nil {
		t.Error("another addon's directory was touched")
	}
	for _, id := range []string{"", "..", "a/b", "../sync"} {
		if err := removeAddonRuntimeDirs([]string{base1}, id); err == nil {
			t.Errorf("id %q taken", id)
		}
	}
	// the helper takes an addon id and nothing else
	sock := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var seen []string
	srv := &Server{Policy: DefaultPolicy(t.TempDir(), "/usr/local/etc/occulite"), Ops: runtimeDirOps{seen: &seen}, Log: func(string, ...any) {}}
	go func() { _ = srv.Serve(ctx, l) }()
	c := Client{Socket: sock}
	if err := c.RemoveAddonRuntimeDirs("lite-addon-sync"); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveAddonRuntimeDirs("../etc"); err == nil || !errors.Is(err, ErrRefused) {
		t.Errorf("a path as id: %v", err)
	}
	if strings.Join(seen, ",") != "lite-addon-sync" {
		t.Errorf("ops saw %v", seen)
	}
	// the bases are the two the system makes directories in
	if strings.Join(AddonRuntimeDirBases, " ") != "/run/lighttpd-addons /run/occulite/backup-targets" {
		t.Errorf("bases: %v", AddonRuntimeDirBases)
	}
}

type runtimeDirOps struct {
	Local
	seen *[]string
}

func (o runtimeDirOps) RemoveAddonRuntimeDirs(id string) error {
	*o.seen = append(*o.seen, id)
	return nil
}
