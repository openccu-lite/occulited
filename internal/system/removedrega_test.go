package system

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// occulited task 185: the addons removed because they need the ReGa, for the Welcome page.
func TestRemovedRegaAddons(t *testing.T) {
	for _, c := range []struct {
		name, leftovers string
		neoDisabled     bool
		want            []string
	}{
		{"nothing ran", "", false, nil},
		{"the pass removed the NEO Server", `{"at":"x","removed":["regadom-bak","neoserver","measurement"],"freed_bytes":1}`, false, []string{"NEO SERVER"}},
		{"the pass found no addon", `{"at":"x","removed":["regadom-bak"],"freed_bytes":1}`, true, nil},
		{"the pass waits, the NEO Server is switched off", "", true, []string{"NEO SERVER"}},
		{"a broken marker falls back to the switch-off", `{`, true, []string{"NEO SERVER"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.leftovers != "" {
				_ = os.WriteFile(filepath.Join(dir, LeftoversMarker), []byte(c.leftovers), 0o600)
			}
			if c.neoDisabled {
				_ = os.WriteFile(filepath.Join(dir, NeoServerDisabledMarker), []byte(`{}`), 0o600)
			}
			if got := RemovedRegaAddons(dir); !slices.Equal(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
