package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// userfsFixture is a UserfsMount over a mount table file whose remount rewrites the table's
// line as the kernel would.
func userfsFixture(t *testing.T, fstype, opts string) (*UserfsMount, *int, func(string)) {
	t.Helper()
	dir := t.TempDir()
	table := filepath.Join(dir, "mounts")
	write := func(o string) {
		line := "/dev/root / ext4 ro,noatime 0 0\n"
		if fstype != "" {
			line += fmt.Sprintf("/dev/mmcblk0p3 /usr/local %s %s 0 0\n", fstype, o)
		}
		if err := os.WriteFile(table, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(opts)
	remounts := 0
	u := &UserfsMount{File: filepath.Join(dir, "state", "userfs-mount.json"), Mounts: table,
		Set: func(_ context.Context, lazytime bool, commit int) error {
			remounts++
			o := "rw,noatime,nodiratime"
			if lazytime {
				o = "rw,lazytime,noatime,nodiratime"
			}
			if commit != 5 {
				o += fmt.Sprintf(",commit=%d", commit)
			}
			write(o)
			return nil
		}}
	return u, &remounts, write
}

// occulited task 160: the state from the mount table, the choice stored only while it differs
// from the image's, applied at once and replayed after a boot.
func TestUserfsMount(t *testing.T) {
	ctx := context.Background()
	u, remounts, write := userfsFixture(t, "ext4", "rw,noatime,nodiratime,commit=30")
	st := u.State()
	if !st.Mounted || !st.Supported || st.FSType != "ext4" || st.Options != "rw,noatime,nodiratime,commit=30" || st.Active != DefaultUserfsMount {
		t.Fatalf("state: %+v", st)
	}
	if o, custom := u.Stored(); custom || o != DefaultUserfsMount {
		t.Fatalf("nothing stored: %+v %v", o, custom)
	}
	if changed, err := u.Replay(ctx); changed || err != nil || *remounts != 0 {
		t.Fatalf("replay with nothing stored: %v %v", changed, err)
	}

	want := UserfsMountOptions{Lazytime: true, CommitS: 120}
	if err := u.Apply(ctx, want); err != nil {
		t.Fatal(err)
	}
	if o, custom := u.Stored(); !custom || o != want {
		t.Fatalf("stored: %+v %v", o, custom)
	}
	if st := u.State(); st.Active != want {
		t.Fatalf("active: %+v", st)
	}
	if fi, err := os.Stat(u.File); err != nil || fi.Mode().Perm() != 0o640 {
		t.Errorf("the file: %v %v", fi, err)
	}
	// a start that finds the mount as chosen leaves it alone
	if changed, err := u.Replay(ctx); changed || err != nil || *remounts != 1 {
		t.Fatalf("replay, already applied: %v %v %d", changed, err, *remounts)
	}
	// after a boot the image's options are back: the choice is applied again
	write("rw,noatime,nodiratime,commit=30")
	if changed, err := u.Replay(ctx); !changed || err != nil || *remounts != 2 || u.State().Active != want {
		t.Fatalf("replay after a boot: %v %v %d %+v", changed, err, *remounts, u.State())
	}
	// ext4's own default interval is not named in the table
	if err := u.Apply(ctx, UserfsMountOptions{Lazytime: false, CommitS: 5}); err != nil {
		t.Fatal(err)
	}
	if st := u.State(); st.Active != (UserfsMountOptions{CommitS: 5}) || st.Options != "rw,noatime,nodiratime" {
		t.Fatalf("commit=5: %+v", st)
	}
	// back to the image's: remounted, and nothing stored any more
	if err := u.Apply(ctx, DefaultUserfsMount); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(u.File); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the image's choice is stored: %v", err)
	}
	if st := u.State(); st.Active != DefaultUserfsMount {
		t.Errorf("active after the default: %+v", st)
	}
	if err := u.Apply(ctx, DefaultUserfsMount); err != nil {
		t.Errorf("the default twice: %v", err)
	}

	// refusals: an interval that is not offered, a failed remount keeps the stored choice
	before := *remounts
	if err := u.Apply(ctx, UserfsMountOptions{CommitS: 45}); err == nil || *remounts != before {
		t.Errorf("commit_s 45: %v", err)
	}
	if err := u.Apply(ctx, want); err != nil {
		t.Fatal(err)
	}
	u.Set = func(context.Context, bool, int) error { return errors.New("mount: busy") }
	if err := u.Apply(ctx, UserfsMountOptions{Lazytime: false, CommitS: 60}); err == nil {
		t.Error("a failed remount is no error")
	}
	if o, _ := u.Stored(); o != want {
		t.Errorf("a failed remount changed the stored choice: %+v", o)
	}
	// a broken file is no choice
	if err := os.WriteFile(u.File, []byte(`{"lazytime":true,"commit_s":7}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if o, custom := u.Stored(); custom || o != DefaultUserfsMount {
		t.Errorf("an interval that is not offered, stored: %+v %v", o, custom)
	}
	_ = os.WriteFile(u.File, []byte(`{`), 0o640)
	if _, custom := u.Stored(); custom {
		t.Error("a broken file is a choice")
	}
}

func TestUserfsMountUnsupported(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct{ name, fstype string }{{"no mount of its own", ""}, {"f2fs", "f2fs"}} {
		u, remounts, _ := userfsFixture(t, c.fstype, "rw")
		st := u.State()
		if st.Supported || st.Mounted != (c.fstype != "") {
			t.Errorf("%s: %+v", c.name, st)
		}
		if err := u.Apply(ctx, UserfsMountOptions{Lazytime: true, CommitS: 60}); !errors.Is(err, priv.ErrUserfsNotExt4) {
			t.Errorf("%s: %v", c.name, err)
		}
		// a choice a backup brought along from an ext4 system is not replayed here
		_ = os.MkdirAll(filepath.Dir(u.File), 0o750)
		_ = os.WriteFile(u.File, []byte(`{"lazytime":true,"commit_s":60}`), 0o640)
		if changed, err := u.Replay(ctx); changed || err != nil || *remounts != 0 {
			t.Errorf("%s: replay %v %v %d", c.name, changed, err, *remounts)
		}
	}
	// no mount table, no state directory, no helper
	u := &UserfsMount{File: filepath.Join(t.TempDir(), "f"), Mounts: filepath.Join(t.TempDir(), "none")}
	if st := u.State(); st.Mounted || st.Supported {
		t.Errorf("no table: %+v", st)
	}
	if err := (&UserfsMount{}).Apply(ctx, DefaultUserfsMount); err == nil {
		t.Error("no state directory")
	}
	var none *UserfsMount
	if o, custom := none.Stored(); custom || o != DefaultUserfsMount {
		t.Error("a nil setting")
	}
	u2, _, _ := userfsFixture(t, "ext4", "rw,commit=30")
	u2.Set = nil
	old := Priv
	Priv = nil
	t.Cleanup(func() { Priv = old })
	if err := u2.Apply(ctx, UserfsMountOptions{Lazytime: true, CommitS: 60}); err == nil {
		t.Error("no helper")
	}
}
