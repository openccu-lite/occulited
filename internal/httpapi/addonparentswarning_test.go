package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/system"
)

// occulited B-145: addon-parents names the parents of the addons' directories the ownership walk
// refuses, with owner, group, mode and reason; nothing when there is none, nothing on a system
// without systemd (no addon is confined there), and it clears as soon as the check is clean.
func TestAddonParentsWarning(t *testing.T) {
	root := t.TempDir()
	a := &SystemAPI{Root: system.Root(root)}
	var found []system.AddonParent
	a.warn.addonParents = func() []system.AddonParent { return found }

	// busybox product: no warning even with a finding
	found = []system.AddonParent{{Path: "/usr/local", UID: 0, GID: 0, Mode: "0777", Reason: "mode"}}
	if w, ok := a.addonParentsWarning(context.Background()); !ok || len(w) != 0 {
		t.Fatalf("without systemd: %v %v", w, ok)
	}
	if err := os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, ok := a.addonParentsWarning(context.Background())
	if !ok || len(w) != 1 {
		t.Fatalf("%v %v", w, ok)
	}
	if w[0].ID != "addon-parents" || w[0].Variant != "/usr/local" || w[0].Severity != "warning" || w[0].Href != "/services" {
		t.Errorf("%+v", w[0])
	}
	list, _ := w[0].Params["parents"].([]map[string]any)
	if len(list) != 1 || list[0]["path"] != "/usr/local" || list[0]["mode"] != "0777" || list[0]["reason"] != "mode" || list[0]["uid"] != uint32(0) {
		t.Errorf("params %+v", w[0].Params)
	}
	// two: the variant is both paths in the check's order
	found = []system.AddonParent{{Path: "/usr/local", Mode: "0777", Reason: "mode"}, {Path: "/usr/local/addons", UID: 1000, GID: 1000, Mode: "0755", Reason: "owner"}}
	if w, _ := a.addonParentsWarning(context.Background()); len(w) != 1 || w[0].Variant != "/usr/local,/usr/local/addons" {
		t.Errorf("%+v", w)
	}
	// fixed: it clears
	found = nil
	if w, ok := a.addonParentsWarning(context.Background()); !ok || len(w) != 0 {
		t.Errorf("clean: %v %v", w, ok)
	}
	// the real check on a temp root: the test's own directories are not root's, so all six that
	// exist are named (owner) - the live stat, not a cache
	a.warn.addonParents = nil
	if err := os.MkdirAll(filepath.Join(root, "usr/local/addons"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, _ = a.addonParentsWarning(context.Background())
	if os.Getuid() != 0 && (len(w) != 1 || w[0].Variant != "/usr/local,/usr/local/addons") {
		t.Errorf("live: %+v", w)
	}
}
