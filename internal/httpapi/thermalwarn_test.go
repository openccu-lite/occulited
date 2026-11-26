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
		found = found || slices.Equal(s.IDs, []string{"soc-hot", "disk-hot", PowerThrottleID})
	}
	if !found {
		t.Error("soc-hot is not a warning source")
	}
}

// occulited task 173: the throttle state as power-throttle - red for an undervoltage now, yellow for
// a throttling now or since the boot beyond a boot dip, nothing for a boot dip or a quiet firmware;
// a throttling now that is the heat's alone beside soc-hot is soc-hot's
func TestThrottleRule(t *testing.T) {
	n := func(v int) *int { return &v }
	last := &system.VoltageEpisode{Start: "2026-11-22T16:44:09+01:00", StartS: 9.4}
	socHot := []warnings.Warning{{ID: "soc-hot"}}
	cases := []struct {
		name    string
		th      *system.Throttle
		raised  []warnings.Warning
		variant string
		sev     string
		cause   string
	}{
		{"no throttle (not a Pi)", nil, nil, "", "", ""},
		{"normal", &system.Throttle{State: system.ThrottleNormal}, nil, "", "", ""},
		{"a boot dip", &system.Throttle{State: system.ThrottleBootDip, SinceBoot: system.ThrottleFlags{UnderVoltage: true, Throttled: true}, Events: n(1), Last: last}, nil, "", "", ""},
		{"undervoltage now", &system.Throttle{State: system.ThrottleUnderVoltage, Now: system.ThrottleFlags{UnderVoltage: true}, Events: n(3), Last: last}, nil, system.ThrottleUnderVoltage, warnings.SeverityError, "supply"},
		{"throttled now after dips", &system.Throttle{State: system.ThrottleThrottled, Now: system.ThrottleFlags{Throttled: true}, SinceBoot: system.ThrottleFlags{UnderVoltage: true, Throttled: true}, Events: n(2)}, socHot, system.ThrottleThrottled, warnings.SeverityWarning, "supply"},
		{"throttled now by the heat, soc-hot raised", &system.Throttle{State: system.ThrottleThrottled, Now: system.ThrottleFlags{Throttled: true, SoftTempLimit: true}, SinceBoot: system.ThrottleFlags{Throttled: true, SoftTempLimit: true}}, socHot, "", "", ""},
		{"the soft limit below soc-hot (a Pi 3B+ at 60 °C)", &system.Throttle{State: system.ThrottleThrottled, Now: system.ThrottleFlags{SoftTempLimit: true}, SinceBoot: system.ThrottleFlags{SoftTempLimit: true}}, nil, system.ThrottleThrottled, warnings.SeverityWarning, "heat"},
		{"earlier, the supply", &system.Throttle{State: system.ThrottleEarlier, SinceBoot: system.ThrottleFlags{UnderVoltage: true, Throttled: true}, Events: n(3), Last: last}, nil, system.ThrottleEarlier, warnings.SeverityWarning, "supply"},
		{"earlier, the heat", &system.Throttle{State: system.ThrottleEarlier, SinceBoot: system.ThrottleFlags{Throttled: true, SoftTempLimit: true}}, socHot, system.ThrottleEarlier, warnings.SeverityWarning, "heat"},
		{"earlier, no reason known", &system.Throttle{State: system.ThrottleEarlier, SinceBoot: system.ThrottleFlags{Throttled: true}}, nil, system.ThrottleEarlier, warnings.SeverityWarning, "unknown"},
		// B-186: where the power supply goes in, for the advice
		{"earlier, through the RPI-RF-MOD", &system.Throttle{State: system.ThrottleEarlier, SinceBoot: system.ThrottleFlags{UnderVoltage: true}, Events: n(1), Last: last, Supply: system.SupplyRPIRFMOD}, nil, system.ThrottleEarlier, warnings.SeverityWarning, "supply"},
		{"undervoltage now, the Pi's socket", &system.Throttle{State: system.ThrottleUnderVoltage, Now: system.ThrottleFlags{UnderVoltage: true}, Supply: system.SupplyPi}, nil, system.ThrottleUnderVoltage, warnings.SeverityError, "supply"},
	}
	for _, c := range cases {
		w := throttleRule(c.th, c.raised)
		if c.variant == "" {
			if w != nil {
				t.Errorf("%s: %+v", c.name, w)
			}
			continue
		}
		if w == nil || w.ID != PowerThrottleID || w.Variant != c.variant || w.Severity != c.sev || w.Href != "#temperature" || w.Params["cause"] != c.cause {
			t.Errorf("%s: %+v", c.name, w)
			continue
		}
		if c.th.Events != nil && w.Params["count"] != *c.th.Events {
			t.Errorf("%s: count %v", c.name, w.Params["count"])
		}
		if (c.th.Last != nil) != (w.Params["last"] != nil) {
			t.Errorf("%s: last %v", c.name, w.Params["last"])
		}
		if s, ok := w.Params["supply"]; (c.th.Supply != "") != ok || (ok && s != c.th.Supply) {
			t.Errorf("%s: supply %v", c.name, w.Params["supply"])
		}
	}
}

// the reader's answer reaches the warning: a supply problem on a Pi, read through thermalWarnings
func TestThermalWarningsThrottle(t *testing.T) {
	root := t.TempDir()
	zone := filepath.Join(root, "sys/class/thermal/thermal_zone0")
	if err := os.MkdirAll(zone, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, v := range map[string]string{"temp": "52000", "type": "cpu-thermal"} {
		if err := os.WriteFile(filepath.Join(zone, f), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	word := "throttled=0x50005"
	a := &SystemAPI{Thermal: &system.ThermalReader{Root: system.Root(root), Platform: "rpi3",
		Throttled: func(context.Context) (string, error) { return word, nil }}}
	list, ok := a.thermalWarnings(context.Background())
	if !ok || len(list) != 1 || list[0].ID != PowerThrottleID || list[0].Severity != warnings.SeverityError || list[0].Params["cause"] != "supply" {
		t.Fatalf("%+v", list)
	}
}
