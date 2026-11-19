package system

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// uninstallPriv is the data rig's helper with the runtime directories under the test's root: the
// real operation knows only the box's /run.
type uninstallPriv struct {
	*dataPriv
	root  Root
	asked []string
}

func (p *uninstallPriv) RemoveAddonRuntimeDirs(id string) error {
	p.asked = append(p.asked, id)
	for _, base := range priv.AddonRuntimeDirBases {
		if err := os.RemoveAll(p.root.join(base + "/" + id)); err != nil {
			return err
		}
	}
	return nil
}

// occulited B-148: the uninstall removes what the system made for the addon - its runtime
// directories and its user and group - and keeps its uid reserved, so the reinstall gets it back.
func TestUninstallRemovesRuntimeDirsAndUser(t *testing.T) {
	r := uidRoot(t, "sync", "mosq")
	for _, id := range []string{"sync", "mosq"} {
		if err := os.WriteFile(r.join("/usr/local/etc/config/rc.d/"+id), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fp := &uninstallPriv{dataPriv: &dataPriv{}, root: r}
	old := Priv
	Priv = fp
	t.Cleanup(func() { Priv = old })
	a := NewSystemdAddons(r, SystemdServices{Root: r, Run: quietRun})
	syncUID, mosqUID := confine(t, a, "sync"), confine(t, a, "mosq")
	for _, d := range []string{"/run/occulite/backup-targets/sync", "/run/lighttpd-addons/sync", "/run/occulite/backup-targets/mosq"} {
		if err := os.MkdirAll(r.join(d), 0o770); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(r.join("/run/occulite/backup-targets/sync/provider.sock"), nil, 0o600)
	if !strings.Contains(rootFile(t, r, "/etc/passwd"), "addon-sync:") {
		t.Fatal("no user after the confinement")
	}

	out, err := a.Uninstall(context.Background(), "sync")
	if err != nil {
		t.Fatalf("%v %+v", err, out)
	}
	for _, want := range []string{"/run/lighttpd-addons/sync", "/run/occulite/backup-targets/sync", "/etc/passwd: addon-sync", "/etc/group: addon-sync"} {
		if !slices.Contains(out.SystemRemoved, want) {
			t.Errorf("system_removed lacks %q: %v", want, out.SystemRemoved)
		}
	}
	for _, d := range []string{"/run/occulite/backup-targets/sync", "/run/lighttpd-addons/sync"} {
		if _, err := os.Lstat(r.join(d)); !os.IsNotExist(err) {
			t.Errorf("%s left: %v", d, err)
		}
	}
	if _, err := os.Stat(r.join("/run/occulite/backup-targets/mosq")); err != nil {
		t.Error("another addon's directory went")
	}
	for _, f := range []string{"/etc/passwd", "/etc/group"} {
		got := rootFile(t, r, f)
		if strings.Contains(got, "addon-sync:") || !strings.Contains(got, "addon-mosq:") || !strings.HasPrefix(got, "root:") {
			t.Errorf("%s:\n%s", f, got)
		}
	}
	if reg := r.ReadAddonUIDs(); reg["sync"] != syncUID || reg["mosq"] != mosqUID {
		t.Errorf("registry: %v", reg)
	}
	// the reinstall gets the old uid and its user back
	if err := os.WriteFile(r.join("/usr/local/etc/config/rc.d/sync"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := confine(t, a, "sync"); got != syncUID {
		t.Errorf("the reinstall got %d, want %d", got, syncUID)
	}
	if !strings.Contains(rootFile(t, r, "/etc/passwd"), "addon-sync:x:"+itoa(syncUID)+":") {
		t.Error("no user after the reinstall")
	}

	// an addon without user or runtime directories: nothing asked, nothing listed
	fp.asked = nil
	if err := os.WriteFile(r.join("/usr/local/etc/config/rc.d/plain"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err = a.Uninstall(context.Background(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.asked) != 0 || slices.ContainsFunc(out.SystemRemoved, func(s string) bool { return strings.HasPrefix(s, "/etc/") || strings.HasPrefix(s, "/run/") }) {
		t.Errorf("asked %v, removed %v", fp.asked, out.SystemRemoved)
	}
}

// occulited B-148: where the registry cannot hold the user's uid for the id, the user stays.
func TestUninstallKeepsUserWithoutReservation(t *testing.T) {
	r := uidRoot(t, "sync")
	useDataPriv(t)
	a := NewSystemdAddons(r, SystemdServices{Root: r, Run: quietRun})
	uid := confine(t, a, "sync")
	// the registry says another uid for the id
	if err := r.writeAddonUIDs(map[string]int{"sync": uid + 7}); err != nil {
		t.Fatal(err)
	}
	if got := a.removeAddonUser("sync"); got != nil {
		t.Errorf("removed %v", got)
	}
	// the uid registered to another addon
	if err := r.writeAddonUIDs(map[string]int{"other": uid}); err != nil {
		t.Fatal(err)
	}
	if got := a.removeAddonUser("sync"); got != nil {
		t.Errorf("removed %v", got)
	}
	if !strings.Contains(rootFile(t, r, "/etc/passwd"), "addon-sync:") {
		t.Error("the user went without a reservation")
	}
	// no registry entry yet: written, then the user goes
	if err := r.writeAddonUIDs(map[string]int{}); err != nil {
		t.Fatal(err)
	}
	if got := a.removeAddonUser("sync"); len(got) != 2 {
		t.Errorf("removed %v", got)
	}
	if reg := r.ReadAddonUIDs(); reg["sync"] != uid {
		t.Errorf("registry: %v", reg)
	}
}

func rootFile(t *testing.T, r Root, p string) string {
	t.Helper()
	b, err := os.ReadFile(r.join(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
