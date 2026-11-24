package system

import (
	"testing"

	"github.com/hobbyquaker/occulited/internal/manifest"
)

// occulited B-174: the install question's mode - root only by the manifest's word or the system's
// default, confined otherwise, also for a manifest without a runtime block.
func TestInstallMode(t *testing.T) {
	for _, c := range []struct {
		def  string
		m    *manifest.Manifest
		want string
	}{
		{"", &manifest.Manifest{Runtime: &manifest.Runtime{}}, "confined"},
		{"", &manifest.Manifest{Runtime: &manifest.Runtime{Root: true}}, "root"},
		{"", &manifest.Manifest{}, "confined"},
		{"", nil, "confined"},
		{"root", &manifest.Manifest{Runtime: &manifest.Runtime{}}, "root"},
		{"confined", &manifest.Manifest{Runtime: &manifest.Runtime{Root: true}}, "root"},
	} {
		if got := (&SystemdAddons{DefaultMode: c.def}).InstallMode(c.m); got != c.want {
			t.Errorf("default %q, %+v: %s, want %s", c.def, c.m, got, c.want)
		}
	}
}
