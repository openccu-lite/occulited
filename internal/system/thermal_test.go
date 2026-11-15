package system

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hobbyquaker/occulited/internal/priv"
)

func put(t *testing.T, root, p, v string) {
	t.Helper()
	full := filepath.Join(root, p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(v), 0o644); err != nil {
		t.Fatal(err)
	}
}

// piTree is the measured Pi 4 (and Pi 3): one zone, its own hwmon0 the same sensor, rpi_volt
// without a temperature.
func piTree(t *testing.T, milli string) string {
	root := t.TempDir()
	put(t, root, "/sys/class/thermal/thermal_zone0/type", "cpu-thermal\n")
	put(t, root, "/sys/class/thermal/thermal_zone0/temp", milli+"\n")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_0_type", "critical\n")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_0_temp", "110000\n")
	if err := os.MkdirAll(filepath.Join(root, "/sys/class/thermal/thermal_zone0/hwmon0"), 0o755); err != nil {
		t.Fatal(err)
	}
	put(t, root, "/sys/class/hwmon/hwmon0/name", "cpu_thermal\n")
	put(t, root, "/sys/class/hwmon/hwmon0/temp1_input", milli+"\n")
	put(t, root, "/sys/class/hwmon/hwmon1/name", "rpi_volt\n")
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "0\n")
	return root
}

// occulited task 148: a Pi has one reading - the zone, not its hwmon twin - read against the
// firmware's 80/85 °C, not the 110 °C trip
func TestReadTemperaturesPi(t *testing.T) {
	root := piTree(t, "54768")
	got := Root(root).ReadTemperatures("rpi4")
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	g := got[0]
	if g.ID != "thermal_zone0" || g.Kind != TempSoC || g.Name != "cpu-thermal" || g.Celsius != 54.8 || g.WarnC != 80 || g.MaxC != 85 || g.Level() != "ok" {
		t.Errorf("%+v", g)
	}
	if c, ok := SoCTemperature(got); !ok || c != 54.8 {
		t.Errorf("SoC %v %v", c, ok)
	}
	if under, ok := Root(root).UnderVoltageNow(); !ok || under {
		t.Errorf("undervoltage %v %v", under, ok)
	}
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "1\n")
	if under, ok := Root(root).UnderVoltageNow(); !ok || !under {
		t.Errorf("undervoltage %v %v", under, ok)
	}
	// the hwmon twin is known by its name too, without the zone's link
	if err := os.RemoveAll(filepath.Join(root, "/sys/class/thermal/thermal_zone0/hwmon0")); err != nil {
		t.Fatal(err)
	}
	if got := Root(root).ReadTemperatures("rpi3"); len(got) != 1 {
		t.Errorf("by name: %+v", got)
	}
	// the levels
	for _, c := range []struct {
		milli, level string
	}{{"79900", "ok"}, {"80000", "warn"}, {"84900", "warn"}, {"85000", "error"}} {
		put(t, root, "/sys/class/thermal/thermal_zone0/temp", c.milli)
		if l := Root(root).ReadTemperatures("rpi4")[0].Level(); l != c.level {
			t.Errorf("%s: %s, want %s", c.milli, l, c.level)
		}
	}
}

// the VM: no zone, no hwmon - nothing, and no panel; a container: the host's sensors are not its own
func TestReadTemperaturesNone(t *testing.T) {
	if got := Root(t.TempDir()).ReadTemperatures("ova"); len(got) != 0 {
		t.Errorf("VM: %+v", got)
	}
	root := piTree(t, "50000")
	put(t, root, "/run/systemd/container", "lxc\n")
	if got := Root(root).ReadTemperatures("rpi4"); len(got) != 0 {
		t.Errorf("container: %+v", got)
	}
	r := &ThermalReader{Root: Root(root), Platform: "rpi4", Disks: func() []Temperature { return []Temperature{{ID: "disk:sda"}} }}
	if th := r.Read(context.Background()); th != nil {
		t.Errorf("container: %+v", th)
	}
	if th := (&ThermalReader{Root: Root(t.TempDir()), Platform: "ova"}).Read(context.Background()); th != nil {
		t.Errorf("VM: %+v", th)
	}
}

// a PC: zones with passive and critical trips, an x86 package first, coretemp's package once,
// a chip no zone covers, the disks' chips left to SMART, an implausible value skipped
func TestReadTemperaturesPC(t *testing.T) {
	root := t.TempDir()
	put(t, root, "/sys/class/thermal/thermal_zone0/type", "acpitz")
	put(t, root, "/sys/class/thermal/thermal_zone0/temp", "27800")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_0_type", "critical")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_0_temp", "105000")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_1_type", "active")
	put(t, root, "/sys/class/thermal/thermal_zone0/trip_point_1_temp", "50000")
	put(t, root, "/sys/class/thermal/thermal_zone1/type", "x86_pkg_temp")
	put(t, root, "/sys/class/thermal/thermal_zone1/temp", "46000")
	put(t, root, "/sys/class/thermal/thermal_zone1/trip_point_0_type", "passive")
	put(t, root, "/sys/class/thermal/thermal_zone1/trip_point_0_temp", "95000")
	put(t, root, "/sys/class/thermal/thermal_zone2/type", "broken")
	put(t, root, "/sys/class/thermal/thermal_zone2/temp", "-273000")
	put(t, root, "/sys/class/hwmon/hwmon0/name", "acpitz")
	put(t, root, "/sys/class/hwmon/hwmon0/temp1_input", "27800")
	put(t, root, "/sys/class/hwmon/hwmon1/name", "nvme")
	put(t, root, "/sys/class/hwmon/hwmon1/temp1_input", "38850")
	put(t, root, "/sys/class/hwmon/hwmon2/name", "nct6775")
	put(t, root, "/sys/class/hwmon/hwmon2/temp2_input", "31000")
	put(t, root, "/sys/class/hwmon/hwmon2/temp2_label", "SYSTIN")
	put(t, root, "/sys/class/hwmon/hwmon2/temp2_max", "75000")
	got := Root(root).ReadTemperatures("x86_64")
	want := []Temperature{
		{ID: "thermal_zone1", Kind: TempSoC, Name: "x86_pkg_temp", Celsius: 46, WarnC: 95, MaxC: 105},
		{ID: "thermal_zone0", Kind: TempSensor, Name: "acpitz", Celsius: 27.8, WarnC: 95, MaxC: 105},
		{ID: "hwmon2", Kind: TempSensor, Name: "nct6775 SYSTIN", Celsius: 31, WarnC: 75, MaxC: 85},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("\n got %+v\nwant %+v", got, want)
	}
}

func TestLevelsAndDisks(t *testing.T) {
	for _, c := range []struct {
		kind, platform     string
		warn, hi, ww, wmax float64
	}{
		{TempSoC, "rpi3", 0, 110, 80, 85},
		{TempSoC, "ova", 0, 0, 80, 90},
		{TempSensor, "ova", 70, 0, 70, 80},
		{TempSensor, "ova", 0, 100, 90, 100},
		{TempSensor, "ova", 90, 80, 90, 100},
	} {
		if w, m := levels(c.kind, c.platform, c.warn, c.hi); w != c.ww || m != c.wmax {
			t.Errorf("%+v: %v %v", c, w, m)
		}
	}
	for _, c := range []struct{ op, crit, w, m int }{{0, 0, 70, 80}, {60, 0, 60, 70}, {84, 85, 84, 85}, {0, 65, 70, 80}} {
		if w, m := DiskTemperatureLevels(c.op, c.crit); w != float64(c.w) || m != float64(c.m) {
			t.Errorf("%+v: %v %v", c, w, m)
		}
	}
}

func TestThrottleWord(t *testing.T) {
	for _, c := range []struct {
		out string
		w   uint32
		ok  bool
	}{{"throttled=0x0\n", 0, true}, {"throttled=0x50005", 0x50005, true}, {"error=1 error_msg=\"x\"", 0, false}, {"", 0, false}} {
		if w, ok := ParseThrottled(c.out); w != c.w || ok != c.ok {
			t.Errorf("%q: %x %v", c.out, w, ok)
		}
	}
	th := ThrottleFromWord(0x50005)
	if !th.Now.UnderVoltage || th.Now.FreqCapped || !th.Now.Throttled || th.Now.SoftTempLimit || !th.SinceBoot.UnderVoltage || !th.SinceBoot.Throttled || th.SinceBoot.FreqCapped || th.Source != "firmware" || *th.Raw != 0x50005 {
		t.Errorf("%+v", th)
	}
	if !th.Now.Any() || (ThrottleFlags{}).Any() {
		t.Error("Any")
	}
}

// the reader: the firmware's word on a Pi, cached; hwmon's undervoltage when the helper cannot
// answer; none off a Pi; the disks after the sensors
func TestThermalReader(t *testing.T) {
	root := piTree(t, "61000")
	calls := 0
	out, err := "throttled=0x20002", error(nil)
	r := &ThermalReader{Root: Root(root), Platform: "rpi4", Throttled: func(context.Context) (string, error) { calls++; return out, err },
		Disks: func() []Temperature { return []Temperature{{ID: "disk:sda", Kind: TempDisk, Celsius: 35}} }}
	th := r.Read(context.Background())
	if th == nil || len(th.Readings) != 2 || th.Readings[1].ID != "disk:sda" || th.Throttle == nil || !th.Throttle.Now.FreqCapped || !th.Throttle.SinceBoot.FreqCapped || th.Throttle.Now.UnderVoltage {
		t.Fatalf("%+v %+v", th, th.Throttle)
	}
	r.Read(context.Background())
	if calls != 1 {
		t.Errorf("not cached: %d calls", calls)
	}
	// a failed read: rpi_volt's alarm alone
	r2 := &ThermalReader{Root: Root(root), Platform: "rpi4", Log: slog.New(slog.DiscardHandler), Throttled: func(context.Context) (string, error) { return "", errors.New("boom") }}
	if th := r2.Read(context.Background()); th.Throttle == nil || th.Throttle.Source != "hwmon" || th.Throttle.Raw != nil {
		t.Errorf("fallback %+v", th.Throttle)
	}
	// vcgencmd not installed: not asked again
	n := 0
	r3 := &ThermalReader{Root: Root(root), Platform: "rpi3", Throttled: func(context.Context) (string, error) {
		n++
		return "", fmt.Errorf("%w: gone", priv.ErrNotAvailable)
	}}
	r3.Read(context.Background())
	r3.at = r3.at.Add(-throttleEvery * 2)
	r3.Read(context.Background())
	if n != 1 {
		t.Errorf("asked %d times", n)
	}
	// not a Pi: no throttle
	if th := (&ThermalReader{Root: Root(root), Platform: "x86_64", Throttled: func(context.Context) (string, error) { t.Error("asked"); return "", nil }}).Read(context.Background()); th == nil || th.Throttle != nil {
		t.Errorf("%+v", th)
	}
}

// SMART's temperature limits, and the report's disk temperatures
func TestSmartTemperatureLimits(t *testing.T) {
	h := ParseSmartctl([]byte(`{"smart_status":{"passed":true},"model_name":"WD Blue SN570","temperature":{"current":44,"op_limit_max":84,"critical_limit_max":85}}`))
	if h.TemperatureC != 44 || h.TempLimitC != 84 || h.TempCriticalC != 85 {
		t.Errorf("%+v", h)
	}
	s := &Storage{}
	s.keepTemperatures([]StorageDevice{
		{Name: "mmcblk0", Kind: "sd"},
		{Name: "nvme0n1", Kind: "nvme", Model: "WD Blue SN570", Smart: &h},
		{Name: "sda", Kind: "usb", Smart: &SmartHealth{Model: "Samsung T7", TemperatureC: 31}},
	})
	got := s.DiskTemperatures()
	if len(got) != 2 || got[0].ID != "disk:nvme0n1" || got[0].WarnC != 84 || got[0].MaxC != 85 || got[0].DiskKind != "nvme" ||
		got[1].Name != "Samsung T7" || got[1].WarnC != 70 || got[1].MaxC != 80 || got[1].ReadAt == nil {
		t.Errorf("%+v", got)
	}
}
