package system

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hobbyquaker/occulited/internal/manifest"
)

// openccu-lite #23: an addon adopted from OpenCCU runs as root without a stored manifest; when the
// manifest in its own directory says an install would give it its own user, the Addons list marks
// it (and the Status page's notice reads the mark). Everything else is not marked: a malformed
// file, another id, a stored manifest, a manifest that declares root or no runtime block, root the
// operator chose, a failed confinement, a confined addon, a system whose default is root, and a
// manifest whose requires.lite the system does not meet.
func TestAdoptedConfinable(t *testing.T) {
	inDir := func(id string) string { return "usr/local/addons/" + id + "/" + manifest.FileName }
	policy := func(id, mode, source string) string {
		return `{"id":"` + id + `","mode":"` + mode + `","source":"` + source + `"}`
	}
	pol := func(id string) string { return "usr/local/etc/config/addon-policy/" + id + ".json" }
	ok := `{"format":1,"id":"%[1]s","name":"X","runtime":{"needs":["rfd"],"data_dirs":["/usr/local/%[1]s"]}}`
	sprintf := fmt.Sprintf
	r := rootWith(t, map[string]string{
		"VERSION": "VERSION=3.89.11\nLITE=2.0.0-beta.5\n",
		// adopted, confinable: migrated root, no stored manifest, a runtime block without root
		pol("loom"):   policy("loom", "root", "migrated"),
		inDir("loom"): `{"format":1,"id":"loom","name":"Loom","runtime":{"needs":["hmipserver"],"api_scopes":["devices:read"]}}`,
		// no policy at all (installed before the systemd product): root as well
		inDir("bare"): sprintf(ok, "bare"),
		// the file does not parse
		pol("broken"):   policy("broken", "root", "migrated"),
		inDir("broken"): `{"format":1,"id":"broken",`,
		// the file names another addon
		pol("other"):   policy("other", "root", "migrated"),
		inDir("other"): sprintf(ok, "someone"),
		// a stored manifest: the install's word stands, no hint
		pol("stored"):   policy("stored", "root", "migrated"),
		inDir("stored"): sprintf(ok, "stored"),
		// the manifest declares root
		pol("rooted"):   policy("rooted", "root", "migrated"),
		inDir("rooted"): `{"format":1,"id":"rooted","name":"R","runtime":{"root":true}}`,
		// no runtime block: it declares nothing
		pol("silent"):   policy("silent", "root", "migrated"),
		inDir("silent"): `{"format":1,"id":"silent","name":"S"}`,
		// root by the operator's own choice
		pol("chosen"):   policy("chosen", "root", "user"),
		inDir("chosen"): sprintf(ok, "chosen"),
		// confining failed: an install would fail the same way
		pol("fell"):   policy("fell", "root", "fallback"),
		inDir("fell"): sprintf(ok, "fell"),
		// already confined
		pol("caged"):   `{"id":"caged","mode":"confined","uid":30001,"user":"addon-caged","source":"migrated"}`,
		inDir("caged"): sprintf(ok, "caged"),
		// needs a newer openccu-lite than the running one
		pol("future"):   policy("future", "root", "migrated"),
		inDir("future"): `{"format":1,"id":"future","name":"F","requires":{"lite":"9.0"},"runtime":{"needs":[]}}`,
		// a runtime block the policy would refuse (a data directory in the shared addons tree)
		pol("fenced"):   policy("fenced", "root", "migrated"),
		inDir("fenced"): `{"format":1,"id":"fenced","name":"D","runtime":{"data_dirs":["/usr/local/addons"]}}`,
		// no file at all
		pol("plain"): policy("plain", "root", "migrated"),
	})
	writeManifest(t, r, "stored", &AddonRuntime{DataDirs: []string{"/usr/local/addons/stored/data"}})
	a := NewSystemdAddons(r, SystemdServices{Root: r, Run: func(context.Context, string, ...string) ([]byte, error) { return nil, nil }})

	want := map[string]bool{"loom": true, "bare": true}
	for _, id := range []string{"loom", "bare", "broken", "other", "stored", "rooted", "silent", "chosen", "fell", "caged", "future", "fenced", "plain"} {
		got := a.AdoptedConfinable(id, a.PolicyView(id))
		if (got != nil) != want[id] {
			t.Errorf("%s: %+v, want %v", id, got, want[id])
		}
	}
	if got := a.AdoptedConfinable("loom", a.PolicyView("loom")); got == nil || !got.Token {
		t.Errorf("loom declares API scopes: %+v", got)
	}
	if got := a.AdoptedConfinable("bare", a.PolicyView("bare")); got == nil || got.Token {
		t.Errorf("bare declares none: %+v", got)
	}
	// the Addons list carries the mark
	list := a.overlayAddonPolicy([]Addon{{ID: "loom"}, {ID: "bare"}, {ID: "chosen"}})
	if !list[0].AdoptedConfinable || !list[0].AdoptedToken || !list[1].AdoptedConfinable || list[1].AdoptedToken || list[2].AdoptedConfinable {
		t.Errorf("list: %+v", list)
	}
	// a system whose default for a new install is root: an install would not confine it either
	a.DefaultMode = "root"
	if got := a.AdoptedConfinable("loom", a.PolicyView("loom")); got != nil {
		t.Errorf("default root: %+v", got)
	}
	a.DefaultMode = ""
	// the hint goes once the addon is installed properly: the stored manifest is there
	writeManifest(t, r, "loom", &AddonRuntime{APIScopes: []string{"devices:read"}})
	if got := a.AdoptedConfinable("loom", a.PolicyView("loom")); got != nil {
		t.Errorf("after the install: %+v", got)
	}
	// and when the file is gone (uninstalled)
	if err := os.RemoveAll(r.join("/usr/local/addons/bare")); err != nil {
		t.Fatal(err)
	}
	if got := a.AdoptedConfinable("bare", a.PolicyView("bare")); got != nil {
		t.Errorf("without its directory: %+v", got)
	}
	// an id the policy rules refuse is never read
	if got := r.confinableManifestInDir("../etc"); got != nil {
		t.Errorf("bad id: %+v", got)
	}
}
