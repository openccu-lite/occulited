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

const userfsTable = `/dev/root / ext4 ro,noatime,nodiratime 0 0
/dev/mmcblk0p1 /boot vfat ro,relatime 0 0
/dev/mmcblk0p3 /usr/local ext4 rw,noatime,nodiratime,commit=30 0 0
/dev/sda1 /media/usb1 ext4 rw,relatime 0 0
`

// userfsFake points the operation at a mount table file and records the remounts.
func userfsFake(t *testing.T, table string) *[]string {
	t.Helper()
	oldDir, oldMounts, oldRemount := userfsDir, userfsMounts, userfsRemount
	t.Cleanup(func() { userfsDir, userfsMounts, userfsRemount = oldDir, oldMounts, oldRemount })
	userfsMounts = filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(userfsMounts, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []string
	userfsRemount = func(_ context.Context, dir, opts string) error {
		calls = append(calls, dir+" "+opts)
		return nil
	}
	return &calls
}

func TestUserfsMountLine(t *testing.T) {
	if fs, opts, ok := UserfsMountLine(userfsTable, "/usr/local"); !ok || fs != "ext4" || opts != "rw,noatime,nodiratime,commit=30" {
		t.Errorf("%q %q %v", fs, opts, ok)
	}
	// a mount on top of it is the one that counts
	if fs, _, ok := UserfsMountLine(userfsTable+"tmpfs /usr/local tmpfs rw 0 0\n", "/usr/local"); !ok || fs != "tmpfs" {
		t.Errorf("the mount on top: %q %v", fs, ok)
	}
	if _, _, ok := UserfsMountLine(userfsTable, "/usr"); ok {
		t.Error("/usr is no mount point")
	}
	if _, _, ok := UserfsMountLine("", "/usr/local"); ok {
		t.Error("an empty table")
	}
}

// occulited task 160: lazytime either way with one of the offered intervals, on the userfs
// alone; nothing on a userfs that is no ext4 mount of its own, no other interval.
func TestSetUserfsMountLocal(t *testing.T) {
	ctx := context.Background()
	calls := userfsFake(t, userfsTable)
	if err := (Local{}).SetUserfsMount(ctx, true, 60); err != nil {
		t.Fatal(err)
	}
	if err := (Local{}).SetUserfsMount(ctx, false, 30); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, "|"); got != "/usr/local remount,lazytime,commit=60|/usr/local remount,nolazytime,commit=30" {
		t.Fatalf("remounts: %s", got)
	}
	*calls = nil
	for _, c := range []int{0, 1, 31, 86400, -5} {
		if err := (Local{}).SetUserfsMount(ctx, true, c); err == nil {
			t.Errorf("commit=%d was taken", c)
		}
	}
	for name, table := range map[string]string{
		"not mounted": "/dev/root / ext4 ro 0 0\n",
		"f2fs":        "/dev/mmcblk0p3 /usr/local f2fs rw 0 0\n",
		"tmpfs on it": userfsTable + "tmpfs /usr/local tmpfs rw 0 0\n",
	} {
		userfsFake(t, table)
		if err := (Local{}).SetUserfsMount(ctx, true, 60); !errors.Is(err, ErrUserfsNotExt4) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("a refused request remounted: %v", *calls)
	}
	userfsFake(t, userfsTable)
	userfsRemount = func(context.Context, string, string) error { return errors.New("mount: busy") }
	if err := (Local{}).SetUserfsMount(ctx, true, 60); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("a failed remount: %v", err)
	}
	userfsMounts = filepath.Join(t.TempDir(), "none")
	if err := (Local{}).SetUserfsMount(ctx, true, 60); err == nil {
		t.Error("no mount table")
	}
}

// Through the helper: the two values alone, a request with anything else is refused.
func TestUserfsMountOperation(t *testing.T) {
	calls := userfsFake(t, userfsTable)
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
	if err := c.SetUserfsMount(ctx, true, 120); err != nil {
		t.Fatalf("through the helper: %v", err)
	}
	if got := strings.Join(*calls, "|"); got != "/usr/local remount,lazytime,commit=120" {
		t.Fatalf("remounts: %s", got)
	}
	*calls = nil
	for _, req := range []request{
		{Op: opUserfsMount, Enabled: true, Commit: 60, Path: "/"},
		{Op: opUserfsMount, Enabled: true, Commit: 60, Args: []string{"remount,rw,suid,exec"}},
		{Op: opUserfsMount, Enabled: true, Commit: 60, Target: "/boot"},
	} {
		if _, err := c.call(ctx, req); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("a request with more than the two values: %v", err)
		}
	}
	if err := c.SetUserfsMount(ctx, true, 7); err == nil {
		t.Error("an interval that is not offered")
	}
	if len(*calls) != 0 {
		t.Errorf("a refused request remounted: %v", *calls)
	}
	if !strings.Contains(strings.Join(logged, "\n"), "userfs-mount") {
		t.Errorf("the refusal was not logged: %v", logged)
	}
	userfsFake(t, "/dev/root / ext4 ro 0 0\n")
	if err := c.SetUserfsMount(ctx, true, 60); !errors.Is(err, ErrUserfsNotExt4) {
		t.Errorf("no userfs mount, through the helper: %v", err)
	}
}
