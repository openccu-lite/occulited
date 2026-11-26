package system

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// occulited task 35: the switch is read the way the fork's lite-usb-mount reads it.
func TestUSBFsckAtDirtyMount(t *testing.T) {
	for _, c := range []struct {
		name, file string
		want       bool
	}{
		{"no file: off (the default)", "", false},
		{"1", "FSCK_AT_DIRTY_MOUNT=1\n", true},
		{"quoted yes", "FSCK_AT_DIRTY_MOUNT=\"yes\"\n", true},
		{"single-quoted true, blanks", "  FSCK_AT_DIRTY_MOUNT = 'true' \n", true},
		{"on", "FSCK_AT_DIRTY_MOUNT=on\r\n", true},
		{"0", "FSCK_AT_DIRTY_MOUNT=0\n", false},
		{"the last line wins", "FSCK_AT_DIRTY_MOUNT=1\nFSCK_AT_DIRTY_MOUNT=0\n", false},
		{"the last line wins, on", "FSCK_AT_DIRTY_MOUNT=0\nFSCK_AT_DIRTY_MOUNT=1\n", true},
		{"a comment is no line", "# FSCK_AT_DIRTY_MOUNT=1\n", false},
		{"another key", "OTHER=1\n", false},
		{"nonsense", "FSCK_AT_DIRTY_MOUNT=maybe\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{}
			if c.file != "" {
				files["usr/local/etc/config/usbstorage"] = c.file
			}
			if got := rootWith(t, files).USBFsckAtDirtyMount(); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestSetUSBFsckAtDirtyMount(t *testing.T) {
	old := Priv
	Priv = priv.Local{}
	t.Cleanup(func() { Priv = old })
	r := rootWith(t, map[string]string{"usr/local/etc/config/.keep": ""})
	path := filepath.Join(string(r), "usr/local/etc/config/usbstorage")
	if err := r.SetUSBFsckAtDirtyMount(true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !r.USBFsckAtDirtyMount() || string(b) != "# openccu-lite: USB sticks; written by occulited's Storage page, read by lite-usb-mount at each mount\nFSCK_AT_DIRTY_MOUNT=1\n" {
		t.Fatalf("on: %q", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", st.Mode())
	}
	// other lines are kept, every switch line becomes one
	_ = os.WriteFile(path, []byte("# mine\nOTHER=x\nFSCK_AT_DIRTY_MOUNT=1\n FSCK_AT_DIRTY_MOUNT=yes\n"), 0o644)
	if err := r.SetUSBFsckAtDirtyMount(false); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if r.USBFsckAtDirtyMount() || string(b) != "# mine\nOTHER=x\nFSCK_AT_DIRTY_MOUNT=0\n" {
		t.Fatalf("off: %q", b)
	}
}

func TestUSBChecks(t *testing.T) {
	r := rootWith(t, map[string]string{
		"run/lite-usb-check/sda1":    "STATE=unchecked\nFSTYPE=vfat\nEXIT=\nAT=1795700000\n",
		"run/lite-usb-check/sdb1":    "STATE=repaired\nFSTYPE=exfat\nEXIT=1\nAT=1795700100\n",
		"run/lite-usb-check/sdc1":    "STATE=failed\nFSTYPE=vfat\nEXIT=4\nAT=x\n",
		"run/lite-usb-check/sdd1":    "STATE=bogus\n",
		"run/lite-usb-check/sde.tmp": "STATE=clean\n",
		"run/lite-usb-check/sdf1":    "STATE=clean\nEXIT=999\n",
	})
	got := r.USBChecks()
	if len(got) != 4 {
		t.Fatalf("%v", got)
	}
	if c := got["sda1"]; c.State != USBCheckUnchecked || c.FSType != "vfat" || c.Exit != nil || !c.At.Equal(time.Unix(1795700000, 0)) {
		t.Errorf("sda1 %+v", c)
	}
	if c := got["sdb1"]; c.State != USBCheckRepaired || c.Exit == nil || *c.Exit != 1 {
		t.Errorf("sdb1 %+v", c)
	}
	if c := got["sdc1"]; c.State != USBCheckFailed || *c.Exit != 4 || !c.At.IsZero() {
		t.Errorf("sdc1 %+v", c)
	}
	if c := got["sdf1"]; c.Exit != nil {
		t.Errorf("an exit out of range: %+v", c)
	}
	if n := USBCheckNames(got); len(n) != 4 || n[0] != "sda1" || n[3] != "sdf1" {
		t.Errorf("names %v", n)
	}
	if len(rootWith(t, nil).USBChecks()) != 0 {
		t.Error("no directory: no records")
	}
}
