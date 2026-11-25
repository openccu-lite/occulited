package system

import (
	"context"
	"path/filepath"
	"testing"
)

// occulited task 177: the clock - the firmware's measured one before cpufreq's - and the moment
// each sticky bit first held, with the temperature and clock then; a bit set at the first read
// appeared before it; the record survives a restart of occulited within the boot, not a new boot.
func TestThrottleCausesAndClock(t *testing.T) {
	root := piTree(t, "79000")
	put(t, root, "/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq", "600000\n")
	put(t, root, "/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq", "1500000\n")
	put(t, root, "/proc/sys/kernel/random/boot_id", "11111111-2222-3333-4444-555555555555\n")
	put(t, root, "/proc/uptime", "3600.5 100\n")
	state := filepath.Join(t.TempDir(), "throttle.json")
	out := "throttled=0x0\nfrequency(48)=1200398464\n"
	newReader := func() *ThermalReader {
		return &ThermalReader{Root: Root(root), Platform: "rpi4", StateFile: state, Throttled: func(context.Context) (string, error) { return out, nil }}
	}
	r := newReader()
	th := r.Read(context.Background())
	if th.Throttle.Clock == nil || th.Throttle.Clock.CurMHz != 1200 || th.Throttle.Clock.MaxMHz != 1500 || th.Throttle.Clock.Source != "firmware" || len(th.Throttle.Causes) != 0 {
		t.Fatalf("clock %+v causes %+v", th.Throttle.Clock, th.Throttle.Causes)
	}
	// bit 17 appears later: recorded with the temperature and clock of that read
	out = "throttled=0x20002\nfrequency(48)=1000000000\n"
	r.at = r.at.Add(-throttleEvery * 2)
	th = r.Read(context.Background())
	c := th.Throttle.Causes
	if len(c) != 1 || c[0].Flag != "freq_capped" || c[0].Before || c[0].ClockMHz != 1000 || c[0].Celsius == nil || *c[0].Celsius != 79 || c[0].UptimeS != 3601 || c[0].At.IsZero() {
		t.Fatalf("cause %+v", c)
	}
	at := c[0].At
	// it stays the first time: a later read with more bits adds only the new one
	out = "throttled=0x60000\n"
	r.at = r.at.Add(-throttleEvery * 2)
	th = r.Read(context.Background())
	if c := th.Throttle.Causes; len(c) != 2 || !c[0].At.Equal(at) || c[1].Flag != "throttled" {
		t.Fatalf("causes %+v", c)
	}
	// without the firmware's clock, cpufreq's
	if th.Throttle.Clock.CurMHz != 600 || th.Throttle.Clock.Source != "cpufreq" {
		t.Errorf("cpufreq clock %+v", th.Throttle.Clock)
	}
	// a restart of occulited: the record is read back; a bit new at its first read appeared before it
	out = "throttled=0x70000\n"
	r2 := newReader()
	th = r2.Read(context.Background())
	if c := th.Throttle.Causes; len(c) != 3 || !c[0].At.Equal(at) || c[2].Flag != "under_voltage" || !c[2].Before {
		t.Fatalf("after a restart %+v", c)
	}
	// a new boot drops the record
	put(t, root, "/proc/sys/kernel/random/boot_id", "99999999-2222-3333-4444-555555555555\n")
	out = "throttled=0x20000\n"
	th = newReader().Read(context.Background())
	if c := th.Throttle.Causes; len(c) != 1 || c[0].Flag != "freq_capped" || !c[0].Before {
		t.Fatalf("new boot %+v", c)
	}
	// not a Pi: no clock either
	if th := (&ThermalReader{Root: Root(root), Platform: "x86_64"}).Read(context.Background()); th == nil || th.Throttle != nil {
		t.Errorf("%+v", th)
	}
}

func TestParseMeasuredClock(t *testing.T) {
	for in, want := range map[string]int{"frequency(48)=1500398464": 1500, "frequency(0)=0": 0, "": 0, "throttled=0x0\nfrequency(48)=600000000\n": 600} {
		if got := parseMeasuredClock(in); got != want {
			t.Errorf("%q: %d", in, got)
		}
	}
}
