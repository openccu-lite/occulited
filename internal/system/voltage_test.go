package system

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// B-162: the maintainer's production Pi 3 (2.0.0-rc.1, 2026-11-22): get_throttled 0x50000 and
// exactly one episode in the kernel log, rpi_volt's two lines as dmesg -r prints them
const prodBootDip = `<6>[    9.512345] brcmfmac: F1 signature read @0x18000000=0x1541a9a6
<4>[    9.660000] hwmon hwmon1: Undervoltage detected!
<6>[   10.100000] usb 1-1.3: new full-speed USB device number 4 using dwc_otg
<6>[   11.680000] hwmon hwmon1: Voltage normalised
`

// a supply that is not enough: one dip at boot, then two under load hours later (SYNTHETIC, the
// same driver's words), the last one still open
const supplyProblem = `<4>[    8.000000] hwmon hwmon1: Undervoltage detected!
<6>[    9.000000] hwmon hwmon1: Voltage normalised
<4>[ 7200.250000] hwmon hwmon1: Undervoltage detected!
<6>[ 7206.250000] hwmon hwmon1: Voltage normalised
<4>[ 9000.000000] hwmon hwmon1: Undervoltage detected!
`

// the older kernels' wording (raspberrypi-hwmon before 5.x): the word in brackets
const olderKernel = `<4>[   12.000000] hwmon hwmon1: Under-voltage detected! (0x00050005)
<4>[   12.500000] hwmon hwmon1: Under-voltage detected! (0x00050005)
<6>[   14.000000] hwmon hwmon1: Voltage normalised (0x00000000)
<6>[   15.000000] hwmon hwmon1: Voltage normalised (0x00000000)
`

var bootAt = time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)

func kmsg(raw string) []LogLine { return parseDmesg([]byte(raw), bootAt, "pi") }

func TestVoltageEpisodes(t *testing.T) {
	eps := VoltageEpisodes(kmsg(prodBootDip))
	if len(eps) != 1 || eps[0].StartS != 9.7 || eps[0].DurationS == nil || *eps[0].DurationS != 2 || !strings.HasPrefix(eps[0].Start, "2026-11-22T08:00:09") {
		t.Fatalf("%+v", eps)
	}
	eps = VoltageEpisodes(kmsg(supplyProblem))
	if len(eps) != 3 || *eps[0].DurationS != 1 || eps[1].StartS != 7200.3 || *eps[1].DurationS != 6 || eps[2].DurationS != nil {
		t.Fatalf("%+v", eps)
	}
	// a second "detected" inside an episode is the same episode; a "normalised" without one is nothing
	eps = VoltageEpisodes(kmsg(olderKernel))
	if len(eps) != 1 || eps[0].StartS != 12 || *eps[0].DurationS != 2 {
		t.Fatalf("%+v", eps)
	}
	// journalctl's order is kept by the stamps: a newest-first answer is put in order
	ls := kmsg(prodBootDip)
	ls[1], ls[3] = ls[3], ls[1]
	if eps := VoltageEpisodes(ls); len(eps) != 1 || *eps[0].DurationS != 2 {
		t.Errorf("reordered %+v", eps)
	}
	// a kernel without stamps: the start and the duration are not known
	eps = VoltageEpisodes([]LogLine{{Message: "hwmon hwmon1: Undervoltage detected!"}, {Message: "hwmon hwmon1: Voltage normalised"}})
	if len(eps) != 1 || eps[0].StartS != -1 || *eps[0].DurationS != -1 || isBootDip(eps) {
		t.Errorf("unstamped %+v", eps)
	}
	if eps := VoltageEpisodes(kmsg("<6>[ 1.0] regulator: supplied voltage 3300000\n")); len(eps) != 0 {
		t.Errorf("not the driver's: %+v", eps)
	}
}

func dur(v float64) *float64 { return &v }

func TestThrottleAssess(t *testing.T) {
	up := 3 * time.Hour
	cases := []struct {
		name  string
		word  uint32
		eps   []VoltageEpisode
		read  bool
		late  bool
		state string
	}{
		{"nothing", 0, nil, false, false, ThrottleNormal},
		// the maintainer's Pi 3: 0x50000, one dip of 2 s at 9.66 s
		{"prod boot dip", 0x50000, VoltageEpisodes(kmsg(prodBootDip)), true, false, ThrottleBootDip},
		{"the same, the kernel log unread", 0x50000, nil, false, false, ThrottleEarlier},
		{"the sticky bits without an episode in the log", 0x50000, []VoltageEpisode{}, true, false, ThrottleEarlier},
		{"recurs after the boot phase", 0x50000, VoltageEpisodes(kmsg(supplyProblem))[:2], true, false, ThrottleEarlier},
		{"present now", 0x50005, VoltageEpisodes(kmsg(supplyProblem)), true, false, ThrottleUnderVoltage},
		{"throttled now", 0x50004, VoltageEpisodes(kmsg(prodBootDip)), true, false, ThrottleThrottled},
		{"a sticky bit seen after the boot phase", 0x50000, VoltageEpisodes(kmsg(prodBootDip)), true, true, ThrottleEarlier},
		{"a soft temperature limit beside the dip", 0xd0000, VoltageEpisodes(kmsg(prodBootDip)), true, false, ThrottleEarlier},
		{"throttled since the boot without undervoltage", 0x40000, nil, false, false, ThrottleEarlier},
		{"too long", 0x50000, []VoltageEpisode{{StartS: 10, DurationS: dur(11)}}, true, false, ThrottleEarlier},
		{"not normalised", 0x50000, []VoltageEpisode{{StartS: 10}}, true, false, ThrottleEarlier},
		{"begun after the boot phase", 0x50000, []VoltageEpisode{{StartS: 120, DurationS: dur(1)}}, true, false, ThrottleEarlier},
		{"three short dips", 0x50000, []VoltageEpisode{{StartS: 10, DurationS: dur(5)}, {StartS: 30, DurationS: dur(5)}, {StartS: 60, DurationS: dur(5)}}, true, false, ThrottleBootDip},
		{"four add up", 0x50000, []VoltageEpisode{{StartS: 10, DurationS: dur(1)}, {StartS: 20, DurationS: dur(1)}, {StartS: 30, DurationS: dur(1)}, {StartS: 40, DurationS: dur(1)}}, true, false, ThrottleEarlier},
		{"longer than 20 s together", 0x50000, []VoltageEpisode{{StartS: 10, DurationS: dur(8)}, {StartS: 30, DurationS: dur(8)}, {StartS: 60, DurationS: dur(8)}}, true, false, ThrottleEarlier},
		// hwmon's alarm alone: the kernel's episodes are the whole history
		{"hwmon: a boot dip", 0, VoltageEpisodes(kmsg(prodBootDip)), true, false, ThrottleBootDip},
		{"hwmon: a dip later", 0, VoltageEpisodes(kmsg(supplyProblem))[:2], true, false, ThrottleEarlier},
	}
	for _, c := range cases {
		th := ThrottleFromWord(c.word)
		th.assess(c.eps, c.read, c.late, up)
		if th.State != c.state {
			t.Errorf("%s: %s, want %s", c.name, th.State, c.state)
		}
		if c.read != (th.Events != nil) {
			t.Errorf("%s: events %v", c.name, th.Events)
		}
	}
	th := ThrottleFromWord(0x50005)
	th.assess(VoltageEpisodes(kmsg(supplyProblem)), true, false, 9010*time.Second)
	if *th.Events != 3 || th.UnderVoltageS != 17 || th.Last == nil || th.Last.StartS != 9000 || th.Last.DurationS != nil || len(th.Episodes) != 3 {
		t.Errorf("history %+v", th)
	}
	// the answer keeps the newest episodes
	many := make([]VoltageEpisode, 30)
	for i := range many {
		many[i] = VoltageEpisode{StartS: float64(200 + i*10), DurationS: dur(1)}
	}
	th = ThrottleFromWord(0x50000)
	th.assess(many, true, false, up)
	if *th.Events != 30 || len(th.Episodes) != voltageKept || th.Episodes[0].StartS != 300 || th.UnderVoltageS != 30 {
		t.Errorf("kept %d %v %v", len(th.Episodes), th.Episodes[0], th.UnderVoltageS)
	}
}

// the reader: the kernel log is read only when the word shows something, cached; a failed read is
// the sticky bits alone; a sticky bit that appears after the boot phase is no boot dip
func TestThermalReaderVoltage(t *testing.T) {
	root := piTree(t, "50000")
	put(t, root, "/proc/uptime", "10800.00 40000.00\n")
	word := "throttled=0x0"
	kernel := 0
	var kerr error
	r := &ThermalReader{Root: Root(root), Platform: "rpi3",
		Throttled: func(context.Context) (string, error) { return word, nil },
		Kernel: func(context.Context) ([]LogLine, error) {
			kernel++
			return kmsg(prodBootDip), kerr
		}}
	th := r.Read(context.Background()).Throttle
	if th.State != ThrottleNormal || kernel != 0 || th.Events != nil {
		t.Fatalf("%+v, %d reads", th, kernel)
	}
	// the bits appear between two reads, past the boot phase: not the boot's, whatever the kernel says
	r.at = time.Time{}
	word = "throttled=0x50000"
	th = r.Read(context.Background()).Throttle
	if th.State != ThrottleEarlier || kernel != 1 || *th.Events != 1 || !r.late {
		t.Fatalf("late %+v, %d reads", th, kernel)
	}
	// a reader that finds them set at its first read: the kernel's episodes decide, read once a minute
	r1 := &ThermalReader{Root: Root(root), Platform: "rpi3",
		Throttled: func(context.Context) (string, error) { return word, nil },
		Kernel: func(context.Context) ([]LogLine, error) {
			kernel++
			return kmsg(prodBootDip), kerr
		}}
	kernel = 0
	th = r1.Read(context.Background()).Throttle
	if th.State != ThrottleBootDip || kernel != 1 || *th.Events != 1 || th.Last.StartS != 9.7 || th.UnderVoltageS != 2 {
		t.Fatalf("%+v, %d reads", th, kernel)
	}
	r1.at = time.Time{}
	if th := r1.Read(context.Background()).Throttle; th.State != ThrottleBootDip || kernel != 1 || r1.late {
		t.Errorf("cached %+v, %d reads", th, kernel)
	}
	// a failed read is not cached as episodes
	r1.vat, kerr = time.Time{}, errors.New("journalctl: exit 2")
	if th := r1.Read(context.Background()).Throttle; th.State != ThrottleEarlier || th.Events != nil || kernel != 2 {
		t.Errorf("failed %+v, %d reads", th, kernel)
	}
	// a fresh reader that finds the bits set already cannot tell: the kernel decides
	r2 := &ThermalReader{Root: Root(root), Platform: "rpi3",
		Throttled: func(context.Context) (string, error) { return "throttled=0x50000", nil },
		Kernel:    func(context.Context) ([]LogLine, error) { return nil, errors.New("journalctl: exit 1") }}
	if th := r2.Read(context.Background()).Throttle; th.State != ThrottleEarlier || th.Events != nil {
		t.Errorf("unread %+v", th)
	}
	// no kernel reader at all: as before B-162
	r3 := &ThermalReader{Root: Root(root), Platform: "rpi3", Throttled: func(context.Context) (string, error) { return "throttled=0x50000", nil }}
	if th := r3.Read(context.Background()).Throttle; th.State != ThrottleEarlier {
		t.Errorf("no reader %+v", th)
	}
	// hwmon's alarm alone, in the boot phase: an undervoltage now is not late
	put(t, root, "/proc/uptime", "30.00 40.00\n")
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "1\n")
	r4 := &ThermalReader{Root: Root(root), Platform: "rpi3", Throttled: func(context.Context) (string, error) { return "", errors.New("no helper") },
		Kernel: func(context.Context) ([]LogLine, error) { return kmsg(prodBootDip), nil }}
	if th := r4.Read(context.Background()).Throttle; th.Source != "hwmon" || th.State != ThrottleUnderVoltage || r4.late {
		t.Errorf("hwmon %+v", th)
	}
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "0\n")
	r4.vat = time.Time{}
	if th := r4.Read(context.Background()).Throttle; th.State != ThrottleBootDip {
		t.Errorf("hwmon after %+v", th)
	}
	put(t, root, "/proc/uptime", "300.00 40.00\n")
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "1\n")
	r4.Read(context.Background())
	put(t, root, "/sys/class/hwmon/hwmon1/in0_lcrit_alarm", "0\n")
	if th := r4.Read(context.Background()).Throttle; !r4.late || th.State != ThrottleEarlier {
		t.Errorf("hwmon late %+v", th)
	}
}

// the journal read the episodes come from: this boot's kernel lines with the word
func TestVoltageQueryArgs(t *testing.T) {
	var got []string
	j := JournalLog{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte(`{"MESSAGE":"hwmon hwmon1: Undervoltage detected!","_SOURCE_MONOTONIC_TIMESTAMP":"9660000","__REALTIME_TIMESTAMP":"1795000000000000","PRIORITY":"4","_TRANSPORT":"kernel"}
{"MESSAGE":"hwmon hwmon1: Voltage normalised","_SOURCE_MONOTONIC_TIMESTAMP":"11680000","__REALTIME_TIMESTAMP":"1795000002000000","PRIORITY":"6","_TRANSPORT":"kernel"}
`), nil
	}}
	lines, err := j.Read(VoltageQuery)
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(got, " ")
	for _, w := range []string{"-k", "--boot=0", "-g voltage --case-sensitive=false"} {
		if !strings.Contains(a, w) {
			t.Errorf("%q lacks %q", a, w)
		}
	}
	if eps := VoltageEpisodes(lines); len(eps) != 1 || eps[0].StartS != 9.7 || *eps[0].DurationS != 2 {
		t.Errorf("%+v", eps)
	}
}
