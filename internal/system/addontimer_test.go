package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/addonunit"
)

const addonTimerText = "[Unit]\nDescription=Sync: Fotos\n\n[Timer]\nOnCalendar=*-*-* 03:00:00\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n"

// addonTimerRig is the own timers' rig with a root that holds addons: sync (confined, declares
// timers and read_addon_data), history and hmm (confined, with groups), legacy (root mode).
func addonTimerRig(t *testing.T) *timerRig {
	t.Helper()
	rig := newTimerRig(t)
	root := t.TempDir()
	rig.s.Root = Root(root)
	w := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("etc/group", "root:x:0:\ncerts:x:120:\naddon-sync:x:30001:\naddon-history:x:30002:\naddon-hmm:x:30003:\noccu:x:50:\n")
	rig.addon(t, "sync", `"bin": "bin", "timers": {"exec": "lite-sync", "args": ["run"]}, "read_addon_data": true, "api_scopes": ["system:read", "timers:addon"]`, "confined", 30001)
	rig.addon(t, "history", ``, "confined", 30002)
	rig.addon(t, "hmm", ``, "confined", 30003)
	rig.addon(t, "legacy", ``, "root", 0)
	return rig
}

// addon stores a manifest and a policy for id; runtime is the inside of the manifest's runtime
// block, "-" for no manifest at all.
func (rig *timerRig) addon(t *testing.T, id, runtime, mode string, uid int) {
	t.Helper()
	dir := filepath.Join(string(rig.s.Root), AddonPolicyDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime != "-" {
		m := fmt.Sprintf(`{"format": 1, "id": %q, "version": "1.0.0", "name": {"de": "Der %s", "en": "The %s"}, "description": "x", "runtime": {%s}}`, id, id, id, runtime)
		if err := os.WriteFile(filepath.Join(dir, id+AddonManifestSuffix), []byte(m), 0o644); err != nil {
			t.Fatal(err)
		}
	} else {
		_ = os.Remove(filepath.Join(dir, id+AddonManifestSuffix))
	}
	p := fmt.Sprintf(`{"id": %q, "mode": %q, "uid": %d, "data_dirs": ["/usr/local/%sdata"]}`, id, mode, uid, id)
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(p), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

// occulited task 154: an addon's timer is made from its schedule alone; the service is the
// system's rendering, the units are local-<id>-<name>, and the record beside them says whose.
func TestAddonTimerLifecycle(t *testing.T) {
	rig := addonTimerRig(t)
	ctx := context.Background()

	at, created, err := rig.s.PutAddonTimer(ctx, "sync", "fotos", AddonTimerInput{Timer: addonTimerText, Description: ptr("Fotos  Klingel\n"), Read: &[]string{"history"}})
	if err != nil || !created {
		t.Fatal(created, err)
	}
	if at.Addon != "sync" || at.Name != "fotos" || at.Unit != "local-sync-fotos.timer" || at.Service != "local-sync-fotos.service" || !at.Enabled || at.Description != "Fotos Klingel" || strings.Join(at.Read, ",") != "history" || at.TimeoutSec != addonunit.TimerTimeoutDefault || at.MemoryMB != addonunit.TimerMemoryDefault {
		t.Errorf("%+v", at)
	}
	svc := rig.fp.files["/run/systemd/system/local-sync-fotos.service"]
	for _, want := range []string{
		"Description=The sync: Fotos Klingel\n",
		"ExecStart=/usr/local/addons/sync/bin/lite-sync run fotos\n",
		"User=addon-sync\nGroup=addon-sync\nSupplementaryGroups=certs addon-history\n",
		"CapabilityBoundingSet=\n",
		" -/usr/local/syncdata\n",
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("the service lacks %q:\n%s", want, svc)
		}
	}
	if svc != at.ServiceFile || rig.fp.files["/run/systemd/system/local-sync-fotos.timer"] != addonTimerText {
		t.Errorf("/run: %v", rig.fp.files)
	}
	// the Services page sees whose it is
	lt, err := rig.s.ReadLocalTimer("local-sync-fotos.timer")
	if err != nil || lt.Addon != "sync" {
		t.Errorf("own timer: %+v %v", lt, err)
	}
	rig.timers = `[{"unit": "local-sync-fotos.timer", "activates": "local-sync-fotos.service", "next": 1790000000000000, "left": 1, "last": null}]`
	if list, err := rig.s.Timers(ctx); err != nil || len(list) != 1 || !list[0].Own || list[0].Addon != "sync" {
		t.Errorf("timers: %+v %v", list, err)
	}

	// a change of the description alone keeps the timer text - also one a person edited
	edited := strings.Replace(addonTimerText, "03:00:00", "04:30:00", 1) + "WakeSystem=no\n"
	if _, err := rig.s.UpdateLocalTimer(ctx, "sync-fotos", edited, lt.ServiceFile); err != nil {
		t.Fatalf("a person's edit of the timer file: %v", err)
	}
	at, created, err = rig.s.PutAddonTimer(ctx, "sync", "fotos", AddonTimerInput{Description: ptr("Fotos")})
	if err != nil || created || at.TimerFile != edited || !strings.Contains(at.ServiceFile, "Description=The sync: Fotos\n") || strings.Join(at.Read, ",") != "history" {
		t.Errorf("%+v %v %v", at, created, err)
	}
	// the service is not a person's to edit
	if _, err := rig.s.UpdateLocalTimer(ctx, "sync-fotos", edited, strings.Replace(at.ServiceFile, "User=addon-sync\n", "", 1)); !errors.Is(err, ErrAddonTimerService) {
		t.Errorf("a changed service of an addon's timer: %v", err)
	}

	// manual only: switched off, both files stay, a run by hand works
	if _, err := rig.s.SetAddonTimerEnabled(ctx, "sync", "fotos", false); err != nil {
		t.Fatal(err)
	}
	if at, _ := rig.s.ReadAddonTimer("sync", "fotos"); at.Enabled || at.TimerFile != edited {
		t.Errorf("switched off: %+v", at)
	}
	rig.calls = nil
	if _, err := rig.s.RunAddonTimer(ctx, "sync", "fotos"); err != nil || strings.Join(rig.calls, "|") != "systemctl start --no-block --no-pager -- local-sync-fotos.service" {
		t.Errorf("run: %v %v", err, rig.calls)
	}
	rig.calls = nil
	if _, err := rig.s.StopAddonTimer(ctx, "sync", "fotos"); err != nil || strings.Join(rig.calls, "|") != "systemctl stop --no-block --no-pager -- local-sync-fotos.service" {
		t.Errorf("stop: %v %v", err, rig.calls)
	}
	if at, _, err := rig.s.PutAddonTimer(ctx, "sync", "fotos", AddonTimerInput{Enabled: ptr(true), TimeoutSec: ptr(3600), MemoryMB: ptr(150), Read: &[]string{}}); err != nil || !at.Enabled || at.TimeoutSec != 3600 || at.MemoryMB != 150 || len(at.Read) != 0 || !strings.Contains(at.ServiceFile, "TimeoutStartSec=3600\n") || strings.Contains(at.ServiceFile, "addon-history") {
		t.Errorf("%+v %v", at, err)
	}

	// made switched off: nothing is enabled
	rig.calls = nil
	at, created, err = rig.s.PutAddonTimer(ctx, "sync", "export", AddonTimerInput{Timer: addonTimerText, Enabled: ptr(false)})
	if err != nil || !created || at.Enabled {
		t.Errorf("%+v %v", at, err)
	}
	for _, c := range rig.calls {
		if strings.Contains(c, "enable") {
			t.Errorf("a timer made switched off was enabled: %v", rig.calls)
		}
	}
	if l := rig.s.AddonTimers("sync"); len(l) != 2 || l[0].Name != "export" || l[1].Name != "fotos" {
		t.Errorf("list: %+v", l)
	}
	if l := rig.s.AddonTimers("history"); len(l) != 0 {
		t.Errorf("another addon's list: %+v", l)
	}

	if err := rig.s.DeleteAddonTimer(ctx, "sync", "export"); err != nil {
		t.Fatal(err)
	}
	if _, ok := rig.stored("local-sync-export.addon"); ok {
		t.Error("the record of a deleted timer stays")
	}
	// the uninstall takes the rest
	if removed := rig.s.RemoveAddonTimers(ctx, "sync"); strings.Join(removed, ",") != "local-sync-fotos.timer" {
		t.Errorf("removed %v", removed)
	}
	for _, f := range []string{"local-sync-fotos.timer", "local-sync-fotos.service", "local-sync-fotos.addon", "local-sync-fotos.disabled"} {
		if _, ok := rig.stored(f); ok {
			t.Errorf("%s stays after the uninstall", f)
		}
	}
	if len(rig.fp.files) != 0 {
		t.Errorf("/run after the uninstall: %v", rig.fp.files)
	}
}

func TestAddonTimerRefusals(t *testing.T) {
	rig := addonTimerRig(t)
	ctx := context.Background()
	put := func(addon, name string, in AddonTimerInput) error {
		_, _, err := rig.s.PutAddonTimer(ctx, addon, name, in)
		return err
	}
	ok := AddonTimerInput{Timer: addonTimerText}
	for what, c := range map[string]struct {
		addon, name string
		in          AddonTimerInput
		kind        error
	}{
		"an addon without the declaration": {"history", "x", ok, ErrAddonTimersUndeclared},
		"an addon that is not installed":   {"ghost", "x", ok, ErrAddonTimersUndeclared},
		"a name with a dot":                {"sync", "a.timer", ok, ErrAddonTimerInvalid},
		"an upper-case name":               {"sync", "Fotos", ok, ErrAddonTimerInvalid},
		"a name too long with the id":      {"sync", strings.Repeat("a", 28), ok, ErrAddonTimerInvalid},
		"a path as a name":                 {"sync", "../x", ok, ErrAddonTimerInvalid},
		"no timer text":                    {"sync", "x", AddonTimerInput{}, ErrAddonTimerInvalid},
		"a timer that starts another unit": {"sync", "x", AddonTimerInput{Timer: "[Timer]\nOnCalendar=hourly\nUnit=poweroff.target\n"}, ErrAddonTimerInvalid},
		"a timer with a service section":   {"sync", "x", AddonTimerInput{Timer: "[Timer]\nOnCalendar=hourly\n[Service]\nExecStart=/bin/sh\n"}, ErrAddonTimerInvalid},
		"reading an unknown addon":         {"sync", "x", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"ghost"}}, ErrAddonTimerInvalid},
		"reading a root addon":             {"sync", "x", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"legacy"}}, ErrAddonTimerInvalid},
		"reading itself":                   {"sync", "x", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"sync"}}, ErrAddonTimerInvalid},
		"reading a system group":           {"sync", "x", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"occu"}}, ErrAddonTimerInvalid},
		"a timeout out of bounds":          {"sync", "x", AddonTimerInput{Timer: addonTimerText, TimeoutSec: ptr(5)}, ErrAddonTimerInvalid},
		"memory out of bounds":             {"sync", "x", AddonTimerInput{Timer: addonTimerText, MemoryMB: ptr(100000)}, ErrAddonTimerInvalid},
	} {
		if err := put(c.addon, c.name, c.in); !errors.Is(err, c.kind) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if len(rig.fp.files) != 0 || len(rig.s.ListLocalTimers()) != 0 {
		t.Errorf("a refused timer left something: %v %v", rig.fp.files, rig.s.ListLocalTimers())
	}

	// a timer a person made under the addon's prefix stays the person's
	if _, err := rig.s.CreateLocalTimer(ctx, "sync-mine", testTimerText, testServiceText); err != nil {
		t.Fatal(err)
	}
	if err := put("sync", "mine", ok); !errors.Is(err, ErrLocalTimerExists) {
		t.Errorf("a person's timer under the addon's prefix: %v", err)
	}
	if _, err := rig.s.ReadAddonTimer("sync", "mine"); !errors.Is(err, ErrLocalTimerNotFound) {
		t.Errorf("reading a person's timer as the addon's: %v", err)
	}
	if err := rig.s.DeleteAddonTimer(ctx, "sync", "mine"); !errors.Is(err, ErrLocalTimerNotFound) {
		t.Errorf("deleting a person's timer as the addon: %v", err)
	}
	if _, err := rig.s.RunAddonTimer(ctx, "sync", "mine"); !errors.Is(err, ErrLocalTimerNotFound) {
		t.Errorf("running a person's timer as the addon: %v", err)
	}
	if lt, err := rig.s.ReadLocalTimer("sync-mine"); err != nil || lt.Addon != "" || lt.ServiceFile != testServiceText {
		t.Errorf("the person's timer: %+v %v", lt, err)
	}
	// a record that names another timer proves nothing
	if err := os.WriteFile(filepath.Join(rig.s.unitsDir(), "local-sync-mine.addon"), []byte(`{"addon": "sync", "name": "other"}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.s.ReadAddonTimer("sync", "mine"); !errors.Is(err, ErrLocalTimerNotFound) {
		t.Errorf("a record that names another timer: %v", err)
	}
	// another addon's timer is not this addon's, whatever the names
	rig.addon(t, "sy", `"bin": "bin", "timers": {"exec": "job"}`, "confined", 30009)
	if err := put("sy", "nc-fotos", ok); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.s.ReadAddonTimer("sync", "fotos"); !errors.Is(err, ErrLocalTimerNotFound) {
		t.Errorf("sy's timer nc-fotos read as sync's fotos: %v", err)
	}
	// without read_addon_data no timer gets a foreign group
	if err := put("sy", "x", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"history"}}); !errors.Is(err, ErrAddonTimerInvalid) {
		t.Errorf("a read grant without the declaration: %v", err)
	}
	if got := rig.s.AddonTimerReadable("sy"); len(got) != 0 {
		t.Errorf("readable without the declaration: %v", got)
	}
	if got := strings.Join(rig.s.AddonTimerReadable("sync"), ","); got != "history,hmm" {
		t.Errorf("readable: %s", got)
	}

	// the limit
	for i := 0; i < AddonTimersMax; i++ {
		if err := put("sync", fmt.Sprintf("t%d", i), AddonTimerInput{Timer: addonTimerText, Enabled: ptr(false)}); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := put("sync", "one-more", ok); !errors.Is(err, ErrAddonTimerLimit) {
		t.Errorf("past the limit: %v", err)
	}
}

// The services follow the addon: a changed policy or manifest renders them again, an addon that
// is gone takes its timers with it, and an addon whose data was read and is gone is left out.
func TestAddonTimerRefresh(t *testing.T) {
	rig := addonTimerRig(t)
	ctx := context.Background()
	if _, _, err := rig.s.PutAddonTimer(ctx, "sync", "fotos", AddonTimerInput{Timer: addonTimerText, Read: &[]string{"history", "hmm"}}); err != nil {
		t.Fatal(err)
	}
	if changed, removed := rig.s.RefreshAddonTimers(ctx, "", true, true); len(changed)+len(removed) != 0 {
		t.Errorf("nothing changed, yet: %v %v", changed, removed)
	}

	// the addon is switched to root mode by the user: its job is root's like the addon
	rig.addon(t, "sync", `"bin": "bin", "timers": {"exec": "lite-sync", "args": ["run"]}, "read_addon_data": true`, "root", 0)
	rig.calls = nil
	changed, removed := rig.s.RefreshAddonTimers(ctx, "sync", true, false)
	svc := rig.fp.files["/run/systemd/system/local-sync-fotos.service"]
	if strings.Join(changed, ",") != "local-sync-fotos.service" || len(removed) != 0 || strings.Contains(svc, "User=") || strings.Contains(svc, "SupplementaryGroups") || strings.Join(rig.calls, "|") != "systemctl daemon-reload" {
		t.Errorf("root mode: %v %v %v\n%s", changed, removed, rig.calls, svc)
	}

	// back to confined, a new program name with the update, and history uninstalled meanwhile
	rig.addon(t, "sync", `"bin": "libexec", "timers": {"exec": "job"}, "read_addon_data": true`, "confined", 30001)
	_ = os.Remove(filepath.Join(string(rig.s.Root), AddonPolicyDir, "history.json"))
	changed, _ = rig.s.RefreshAddonTimers(ctx, "", false, true)
	svc, _ = rig.stored("local-sync-fotos.service")
	if len(changed) != 1 || !strings.Contains(svc, "ExecStart=/usr/local/addons/sync/libexec/job fotos\n") || !strings.Contains(svc, "SupplementaryGroups=certs addon-hmm\n") {
		t.Errorf("after the update: %v\n%s", changed, svc)
	}
	if strings.Contains(rig.fp.files["/run/systemd/system/local-sync-fotos.service"], "libexec") {
		t.Error("without apply /run is the replay's to write")
	}
	// history is back: the record kept it
	rig.addon(t, "history", ``, "confined", 30002)
	rig.s.RefreshAddonTimers(ctx, "", false, true)
	if svc, _ = rig.stored("local-sync-fotos.service"); !strings.Contains(svc, "SupplementaryGroups=certs addon-history addon-hmm\n") {
		t.Errorf("history back:\n%s", svc)
	}
	// the replay writes what was rendered
	rig.fp.files = map[string]string{}
	if applied, problems := rig.s.ReplayLocalTimers(ctx); len(applied) != 1 || len(problems) != 0 || rig.fp.files["/run/systemd/system/local-sync-fotos.service"] != svc {
		t.Errorf("replay: %v %v", applied, problems)
	}

	// a policy change in the middle of an install, the manifest not stored: nothing is removed
	rig.addon(t, "sync", "-", "confined", 30001)
	if changed, removed := rig.s.RefreshAddonTimers(ctx, "sync", true, false); len(changed)+len(removed) != 0 {
		t.Errorf("without a manifest and without orphans: %v %v", changed, removed)
	}
	// the addon declares no timers any more (or is gone): its timers go at the next start
	if _, removed := rig.s.RefreshAddonTimers(ctx, "", false, true); strings.Join(removed, ",") != "local-sync-fotos.timer" {
		t.Errorf("removed: %v", removed)
	}
	if l := rig.s.ListLocalTimers(); len(l) != 0 {
		t.Errorf("left: %+v", l)
	}
}
