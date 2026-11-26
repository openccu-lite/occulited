package system

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// occulited B-186: the maintainer's production Pi - one episode begun at 19:52 the evening before,
// no "Voltage normalised" after it in the kernel log, and 0x50000 (no undervoltage now). The panel
// drew it up to now, the whole 24 h chart red. An episode is open only while the undervoltage
// holds now; otherwise it has ended at a moment the log does not tell (duration -1).
const unendedDip = `<4>[ 7200.000000] hwmon hwmon1: Undervoltage detected!
`

func TestThrottleAssessUnendedEpisode(t *testing.T) {
	up := 13 * time.Hour
	cases := []struct {
		name   string
		th     Throttle
		eps    []VoltageEpisode
		dur    *float64 // the last episode's duration; nil = open
		totalS float64
		state  string
	}{
		{"firmware: undervoltage now, the episode stays open", ThrottleFromWord(0x50005), VoltageEpisodes(kmsg(unendedDip)), nil, up.Seconds() - 7200, ThrottleUnderVoltage},
		{"firmware: the supply is fine now, the episode has ended (B-186)", ThrottleFromWord(0x50000), VoltageEpisodes(kmsg(unendedDip)), dur(-1), 0, ThrottleEarlier},
		{"firmware: only throttled now, the episode has ended", ThrottleFromWord(0x50004), VoltageEpisodes(kmsg(unendedDip)), dur(-1), 0, ThrottleThrottled},
		{"firmware: the older episodes keep their lengths", ThrottleFromWord(0x50000), VoltageEpisodes(kmsg(supplyProblem)), dur(-1), 7, ThrottleEarlier},
		{"firmware: a closed episode is untouched", ThrottleFromWord(0x50000), VoltageEpisodes(kmsg(prodBootDip)), dur(2), 2, ThrottleBootDip},
		{"hwmon: the alarm holds, open", Throttle{Source: "hwmon", Now: ThrottleFlags{UnderVoltage: true}}, VoltageEpisodes(kmsg(unendedDip)), nil, up.Seconds() - 7200, ThrottleUnderVoltage},
		{"hwmon: the alarm is clear, ended", Throttle{Source: "hwmon"}, VoltageEpisodes(kmsg(unendedDip)), dur(-1), 0, ThrottleEarlier},
		// a boot dip whose end the log lost is not told apart from a supply problem: earlier
		{"firmware: an unended dip in the boot phase", ThrottleFromWord(0x50000), []VoltageEpisode{{StartS: 9.4}}, dur(-1), 0, ThrottleEarlier},
	}
	for _, c := range cases {
		cached := append([]VoltageEpisode(nil), c.eps...)
		th := c.th
		th.assess(c.eps, true, false, up)
		if th.State != c.state {
			t.Errorf("%s: state %s, want %s", c.name, th.State, c.state)
		}
		last := th.Episodes[len(th.Episodes)-1]
		switch {
		case c.dur == nil && last.DurationS != nil, c.dur != nil && (last.DurationS == nil || *last.DurationS != *c.dur):
			t.Errorf("%s: duration %v", c.name, last.DurationS)
		}
		if th.Last == nil || (th.Last.DurationS == nil) != (c.dur == nil) {
			t.Errorf("%s: last %+v", c.name, th.Last)
		}
		if th.UnderVoltageS != c.totalS {
			t.Errorf("%s: total %v, want %v", c.name, th.UnderVoltageS, c.totalS)
		}
		// the reader's cache is not changed: the next read with the bit set finds it open again
		for i := range c.eps {
			if (c.eps[i].DurationS == nil) != (cached[i].DurationS == nil) {
				t.Errorf("%s: the cached episode %d changed", c.name, i)
			}
		}
	}
}

// the reader: the open episode is drawn as ended while the word says the supply is fine, open again
// while it is not, and the case is logged once, with rpi_volt's alarm
func TestThermalReaderUnendedEpisode(t *testing.T) {
	root := piTree(t, "49400")
	put(t, root, "/proc/uptime", "46800.00 40000.00\n")
	word := "throttled=0x50000"
	var buf bytes.Buffer
	r := &ThermalReader{Root: Root(root), Platform: "rpi3", Log: slog.New(slog.NewTextHandler(&buf, nil)),
		Throttled: func(context.Context) (string, error) { return word, nil },
		Kernel:    func(context.Context) ([]LogLine, error) { return kmsg(unendedDip), nil }}
	th := r.Read(context.Background()).Throttle
	if th.State != ThrottleEarlier || th.Last == nil || th.Last.DurationS == nil || *th.Last.DurationS != -1 {
		t.Fatalf("%+v", th)
	}
	if n := strings.Count(buf.String(), "no end for an undervoltage episode"); n != 1 || !strings.Contains(buf.String(), "rpi_volt_alarm=false") || !strings.Contains(buf.String(), "word=0x50000") {
		t.Fatalf("log %q", buf.String())
	}
	r.at = time.Time{}
	r.Read(context.Background())
	if n := strings.Count(buf.String(), "no end for an undervoltage episode"); n != 1 {
		t.Errorf("logged %d times", n)
	}
	r.at, word = time.Time{}, "throttled=0x50005"
	if th = r.Read(context.Background()).Throttle; th.State != ThrottleUnderVoltage || th.Last.DurationS != nil {
		t.Errorf("now %+v", th)
	}
}

func TestPowerSupply(t *testing.T) {
	cases := []struct {
		name string
		kv   map[string]string
		want string
	}{
		{"not detected yet", map[string]string{}, ""},
		{"the RPI-RF-MOD on the header", map[string]string{"HM_MODE": "NORMAL", "HM_HMIP_DEV": "RPI-RF-MOD", "HM_HMIP_DEVTYPE": "GPIO@3f201000.serial", "HM_HMRF_DEV": "RPI-RF-MOD"}, SupplyRPIRFMOD},
		{"the RPI-RF-MOD without a type", map[string]string{"HM_MODE": "NORMAL", "HM_HMRF_DEV": "RPI-RF-MOD"}, SupplyRPIRFMOD},
		{"the RPI-RF-MOD on an HB-RF-USB-2", map[string]string{"HM_MODE": "NORMAL", "HM_HMIP_DEV": "RPI-RF-MOD", "HM_HMIP_DEVTYPE": "HB-RF-USB-2@usb-1-1.3"}, SupplyPi},
		{"the RPI-RF-MOD on an HB-RF-ETH", map[string]string{"HM_MODE": "NORMAL", "HM_HMIP_DEV": "RPI-RF-MOD", "HM_HMIP_DEVTYPE": "HB-RF-ETH@172.16.0.9"}, SupplyPi},
		{"an HmIP-RFUSB", map[string]string{"HM_MODE": "NORMAL", "HM_HMIP_DEV": "HMIP-RFUSB"}, SupplyPi},
		{"the old HM-MOD-RPI-PCB", map[string]string{"HM_MODE": "NORMAL", "HM_HMRF_DEV": "HM-MOD-RPI-PCB", "HM_HMRF_DEVTYPE": "GPIO@3f201000.serial"}, SupplyPi},
		{"no module", map[string]string{"HM_MODE": "NORMAL"}, SupplyPi},
	}
	for _, c := range cases {
		if got := PowerSupply(c.kv); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// occulited B-186, the production read of 2026-11-26 09:36: the Pi 3 booted 19:52:45, up 13:42.
// `dmesg` held the boot dip - detected at 9.535987 s, normalised at 11.552027 s - while the
// volatile, size-capped journal's kernel lines of this boot began at 46673 s: no voltage line at
// all (and, at the time of the screenshot, at most the first of the two). With the journal alone
// the dip was an open episode from 19:52 and the state "earlier" with a warning; with the ring
// buffer it is the boot dip it was - normal, a 2 s mark.
const prodRing186 = `<2>[    9.535987] hwmon hwmon1: Undervoltage detected!
<6>[   11.552027] hwmon hwmon1: Voltage normalised
`

func TestKernelVoltageLinesProduction(t *testing.T) {
	ring := func() ([]LogLine, error) { return kmsg(prodRing186), nil }
	ringFails := func() ([]LogLine, error) { return nil, errors.New("dmesg: Operation not permitted") }
	// the journal as at 09:36 (nothing), and as it may have been at 08:44 (the first line alone,
	// with the journal's own stamp of it)
	journalNone := func() ([]LogLine, error) { return []LogLine{}, nil }
	journalFirst := func() ([]LogLine, error) { return kmsg(prodRing186)[:1], nil }
	journalFails := func() ([]LogLine, error) { return nil, errors.New("journalctl: exit 2") }
	up := 13*time.Hour + 42*time.Minute
	cases := []struct {
		name    string
		ring    func() ([]LogLine, error)
		journal func() ([]LogLine, error)
		state   string
		dur     float64 // the one episode's duration
		err     bool
	}{
		{"the ring buffer, the journal without a line", ring, journalNone, ThrottleBootDip, 2, false},
		{"the ring buffer, the journal with the first line only", ring, journalFirst, ThrottleBootDip, 2, false},
		{"the ring buffer alone (no journal)", ring, nil, ThrottleBootDip, 2, false},
		{"the ring buffer, the journal failing", ring, journalFails, ThrottleBootDip, 2, false},
		// without the ring buffer the journal's half is all there is: ended, length unknown, a warning
		{"the journal's first line alone", ringFails, journalFirst, ThrottleEarlier, -1, false},
		{"neither", ringFails, journalFails, "", 0, true},
	}
	for _, c := range cases {
		lines, err := KernelVoltageLines(c.ring, c.journal)
		if c.err {
			if err == nil {
				t.Errorf("%s: no error, %v", c.name, lines)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		th := ThrottleFromWord(0x50000)
		th.assess(VoltageEpisodes(lines), true, false, up)
		if th.State != c.state || *th.Events != 1 || th.Last.DurationS == nil || *th.Last.DurationS != c.dur || th.Last.StartS != 9.5 {
			t.Errorf("%s: %s %+v", c.name, th.State, th.Last)
		}
		if !strings.HasPrefix(th.Last.Start, "2026-11-22T08:00:09") {
			t.Errorf("%s: start %s", c.name, th.Last.Start)
		}
	}
	if _, err := KernelVoltageLines(nil, nil); err == nil {
		t.Error("no source: no error")
	}
}

// a line in both sources is one line; unstamped lines are kept once
func TestMergeVoltageLines(t *testing.T) {
	a := kmsg(prodRing186)
	b := append(kmsg(prodRing186), kmsg(supplyProblem)[2:]...)
	if got := MergeVoltageLines(a, b); len(got) != 5 {
		t.Errorf("merged %d: %+v", len(got), got)
	}
	u := []LogLine{{Message: "hwmon hwmon1: Undervoltage detected!"}, {Message: "regulator: supplied voltage 3300000"}}
	if got := MergeVoltageLines(u, u); len(got) != 1 {
		t.Errorf("unstamped %+v", got)
	}
}

// the helper's voltage-log read: its lines parsed as the ring buffer's; a failure is an error
type fakeVoltageLog struct {
	priv.Local
	out  string
	exit int
	err  error
}

func (f fakeVoltageLog) VoltageLog(context.Context) (priv.Result, error) {
	return priv.Result{Stdout: []byte(f.out), Exit: f.exit}, f.err
}

func TestHelperRing(t *testing.T) {
	old := Priv
	t.Cleanup(func() { Priv = old })
	root := piTree(t, "49400")
	put(t, root, "/proc/uptime", "49320.00 40000.00\n")
	Priv = fakeVoltageLog{out: prodRing186}
	lines, err := HelperRing(Root(root)).Read(VoltageQuery)
	if err != nil || len(lines) != 2 || lines[0].MonotonicUS != 9535987 || lines[0].Timestamp == "" {
		t.Fatalf("%+v %v", lines, err)
	}
	for _, f := range []fakeVoltageLog{{exit: 1}, {err: errors.New("helper: refused")}} {
		Priv = f
		if _, err := HelperRing(Root(root)).Read(VoltageQuery); err == nil {
			t.Errorf("%+v: no error", f)
		}
	}
	Priv = nil
	if _, err := HelperRing(Root(root)).Read(VoltageQuery); err == nil {
		t.Error("no helper: no error")
	}
}
