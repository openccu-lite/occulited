package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/priv"
	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

type fakeUSBHelper struct{ calls []string }

func (f *fakeUSBHelper) USBFormat(_ context.Context, s priv.USBFormatSpec) error {
	f.calls = append(f.calls, "format "+s.Device+" "+s.FS+" "+s.Label)
	return nil
}
func (f *fakeUSBHelper) USBEject(_ context.Context, dev string) error {
	f.calls = append(f.calls, "eject "+dev)
	return nil
}

// openccu-lite task 228: the list, Format and Safely remove through the helper.
func TestStorageUSBRoutes(t *testing.T) {
	r := fakeRoot(t)
	real := filepath.Join(string(r), "sys/devices/pci0/usb1/1-1/host0/block/sda")
	_ = os.MkdirAll(filepath.Join(real, "sda1"), 0o755)
	_ = os.WriteFile(filepath.Join(real, "size"), []byte("62521344\n"), 0o644)
	_ = os.WriteFile(filepath.Join(real, "sda1", "size"), []byte("62519296\n"), 0o644)
	_ = os.WriteFile(filepath.Join(real, "removable"), []byte("1\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(real, "device"), 0o755)
	_ = os.WriteFile(filepath.Join(real, "device", "model"), []byte("Ultra Fit       \n"), 0o644)
	_ = os.MkdirAll(filepath.Join(string(r), "sys/block"), 0o755)
	_ = os.Symlink(real, filepath.Join(string(r), "sys/block/sda"))
	sata := filepath.Join(string(r), "sys/devices/pci0/ata1/host1/block/sdb")
	_ = os.MkdirAll(sata, 0o755)
	_ = os.Symlink(sata, filepath.Join(string(r), "sys/block/sdb"))
	_ = os.MkdirAll(filepath.Join(string(r), "usr/bin"), 0o755)
	_ = os.WriteFile(filepath.Join(string(r), "usr/bin/mkfs.exfat"), nil, 0o755)

	h := &fakeUSBHelper{}
	old := usbHelperOf
	usbHelperOf = func() (usbDiskHelper, bool) { return h, true }
	t.Cleanup(func() { usbHelperOf = old })
	mux := http.NewServeMux()
	(&SystemAPI{Root: r}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	st, out, _ := do(t, srv, "GET", "/api/system/v1/storage/usb", "", nil)
	disks, _ := out["disks"].([]any)
	if st != 200 || len(disks) != 1 {
		t.Fatalf("list: %d %v", st, out)
	}
	d := disks[0].(map[string]any)
	if d["name"] != "sda" || d["model"] != "Ultra Fit" || d["size_bytes"] != float64(62521344*512) || d["removable"] != true {
		t.Errorf("disk: %v", d)
	}
	if fs := out["filesystems"].([]any); len(fs) != 1 || fs[0] != "exfat" {
		t.Errorf("filesystems: %v", fs)
	}
	if st, _, _ := do(t, srv, "POST", "/api/system/v1/storage/usb/sda/format", `{"fs":"exfat","label":"THIS_LABEL_IS_TOO_LONG"}`, nil); st != 422 {
		t.Errorf("a long label: %d", st)
	}
	if st, _, _ := do(t, srv, "POST", "/api/system/v1/storage/usb/sdb/format", `{"fs":"exfat","label":"X"}`, nil); st != 404 {
		t.Errorf("a SATA disk: %d", st)
	}
	st, out, _ = do(t, srv, "POST", "/api/system/v1/storage/usb/sda/format", `{"fs":"ext4","label":"LOGSTICK"}`, nil)
	if st != 200 || out["formatted"] != "sda" {
		t.Fatalf("format: %d %v", st, out)
	}
	st, out, _ = do(t, srv, "POST", "/api/system/v1/storage/usb/sda/eject", "", nil)
	if st != 200 || out["ejected"] != "sda" {
		t.Fatalf("eject: %d %v", st, out)
	}
	if len(h.calls) != 2 || h.calls[0] != "format sda ext4 LOGSTICK" || h.calls[1] != "eject sda" {
		t.Errorf("helper: %q", h.calls)
	}
	var _ = system.USBLoopTestMarker
}

// occulited task 35: the switch of the check at a dirty mount, the mount's record per partition, and
// the Status warning of a stick that was not checked or whose check failed.
func TestStorageUSBCheck(t *testing.T) {
	r := fakeRoot(t)
	real := filepath.Join(string(r), "sys/devices/pci0/usb1/1-1/host0/block/sda")
	_ = os.MkdirAll(filepath.Join(real, "sda1"), 0o755)
	_ = os.WriteFile(filepath.Join(real, "sda1", "size"), []byte("62519296\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(string(r), "sys/block"), 0o755)
	_ = os.Symlink(real, filepath.Join(string(r), "sys/block/sda"))
	_ = os.MkdirAll(filepath.Join(string(r), "usr/local/etc/config"), 0o755)
	check := filepath.Join(string(r), "run/lite-usb-check")
	_ = os.MkdirAll(check, 0o755)
	_ = os.WriteFile(filepath.Join(check, "sda1"), []byte("STATE=unchecked\nFSTYPE=vfat\nEXIT=\nAT=1795700000\n"), 0o644)
	old := system.Priv
	system.Priv = priv.Local{}
	t.Cleanup(func() { system.Priv = old })

	mux := http.NewServeMux()
	a := &SystemAPI{Root: r}
	a.Register(mux)
	a.Warnings = &warnings.Tracker{Sources: a.warningSources()}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	st, out, _ := do(t, srv, "GET", "/api/system/v1/storage/usb", "", nil)
	if st != 200 || out["fsck_at_dirty_mount"] != false {
		t.Fatalf("list: %d %v", st, out)
	}
	p := out["disks"].([]any)[0].(map[string]any)["partitions"].([]any)[0].(map[string]any)
	if c, _ := p["check"].(map[string]any); c["state"] != "unchecked" || c["fstype"] != "vfat" || c["at"] != "2026-11-26T13:33:20Z" {
		t.Errorf("the partition's check: %v", p)
	}
	usb := func() *warnings.Warning {
		for _, w := range a.Warnings.Evaluate(context.Background()) {
			if w.ID == "usb-unchecked" {
				return &w
			}
		}
		return nil
	}
	w := usb()
	if w == nil || w.Href != "/system/storage#usb" || w.Variant != "sda1:unchecked:1795700000" || w.Params["fsck"] != false {
		t.Fatalf("warning: %+v", w)
	}

	// the switch: validated, written for the fork, the answer is the new list
	if st, _, _ := do(t, srv, "PUT", "/api/system/v1/storage/usb/settings", `{}`, nil); st != 422 {
		t.Errorf("no value: %d", st)
	}
	st, out, _ = do(t, srv, "PUT", "/api/system/v1/storage/usb/settings", `{"fsck_at_dirty_mount":true}`, nil)
	if st != 200 || out["fsck_at_dirty_mount"] != true {
		t.Fatalf("on: %d %v", st, out)
	}
	if b, _ := os.ReadFile(filepath.Join(string(r), "usr/local/etc/config/usbstorage")); !strings.Contains(string(b), "\nFSCK_AT_DIRTY_MOUNT=1\n") {
		t.Errorf("the file: %q", b)
	}
	// the stick mounted now stays unchecked until its next mount: the warning stays, and says the switch is on
	if w := usb(); w == nil || w.Params["fsck"] != true {
		t.Errorf("after the switch: %+v", w)
	}
	// a repair is no warning; a failed check is
	_ = os.WriteFile(filepath.Join(check, "sda1"), []byte("STATE=repaired\nFSTYPE=vfat\nEXIT=1\nAT=1795700100\n"), 0o644)
	if w := usb(); w != nil {
		t.Errorf("a repair warned: %+v", w)
	}
	_ = os.WriteFile(filepath.Join(check, "sda1"), []byte("STATE=failed\nFSTYPE=vfat\nEXIT=4\nAT=1795700200\n"), 0o644)
	if w := usb(); w == nil || w.Variant != "sda1:failed:1795700200" {
		t.Errorf("a failed check: %+v", w)
	}
	// the stick removed: no record, no warning
	_ = os.Remove(filepath.Join(check, "sda1"))
	if w := usb(); w != nil {
		t.Errorf("no record: %+v", w)
	}
}
