package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// occulited task 148: soc-hot from 80 °C on a Pi, red from 85, held until 3 °C below; disk-hot
// for a disk at its limit; nothing on a system without sensors
func TestThermalWarnings(t *testing.T) {
	root := t.TempDir()
	zone := filepath.Join(root, "sys/class/thermal/thermal_zone0")
	if err := os.MkdirAll(zone, 0o755); err != nil {
		t.Fatal(err)
	}
	set := func(milli string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(zone, "temp"), []byte(milli), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(zone, "type"), []byte("cpu-thermal"), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := 40.0
	a := &SystemAPI{Thermal: &system.ThermalReader{Root: system.Root(root), Platform: "rpi4",
		Throttled: func(context.Context) (string, error) { return "throttled=0x0", nil },
		Disks: func() []system.Temperature {
			return []system.Temperature{{ID: "disk:sda", Kind: system.TempDisk, Name: "Samsung T7", Device: "sda", DiskKind: "usb", Celsius: disk, WarnC: 70, MaxC: 80}}
		}}}
	eval := func() map[string]warnings.Warning {
		t.Helper()
		list, ok := a.thermalWarnings(context.Background())
		if !ok {
			t.Fatal("unknown")
		}
		out := map[string]warnings.Warning{}
		for _, w := range list {
			out[w.ID] = w
		}
		return out
	}
	for _, c := range []struct {
		milli, soc string
	}{
		{"60000", ""}, {"79900", ""}, {"80000", "warning"}, {"77100", "warning"}, {"77000", ""},
		{"85000", "error"}, {"82100", "error"}, {"82000", "warning"}, {"76000", ""},
	} {
		set(c.milli)
		w := eval()["soc-hot"]
		if w.Variant != c.soc || (c.soc != "" && (w.Severity != c.soc || w.Href != "#temperature" || w.Params["name"] != "cpu-thermal" || w.Params["warn_c"] != 80.0)) {
			t.Errorf("%s: %+v, want %q", c.milli, w, c.soc)
		}
	}
	set("50000")
	disk = 71
	w := eval()
	if _, ok := w["soc-hot"]; ok || w["disk-hot"].Variant != "warning" {
		t.Fatalf("%+v", w)
	}
	if d := w["disk-hot"].Params["disks"].([]map[string]any); len(d) != 1 || d[0]["device"] != "sda" || d[0]["model"] != "Samsung T7" || d[0]["celsius"] != 71.0 {
		t.Errorf("%+v", d)
	}
	disk = 80
	if w := eval(); w["disk-hot"].Variant != "error" {
		t.Errorf("%+v", w)
	}
	disk = 40
	if w := eval(); len(w) != 0 {
		t.Errorf("%+v", w)
	}
	// no sensor, no reader: nothing
	if list, ok := (&SystemAPI{Thermal: &system.ThermalReader{Root: system.Root(t.TempDir()), Platform: "ova"}}).thermalWarnings(context.Background()); !ok || len(list) != 0 {
		t.Errorf("%v %v", list, ok)
	}
	if list, ok := (&SystemAPI{}).thermalWarnings(context.Background()); !ok || len(list) != 0 {
		t.Errorf("%v %v", list, ok)
	}
	found := false
	for _, s := range a.warningSources() {
		found = found || slices.Equal(s.IDs, []string{"soc-hot", "disk-hot"})
	}
	if !found {
		t.Error("soc-hot is not a warning source")
	}
}
