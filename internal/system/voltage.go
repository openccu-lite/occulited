package system

import (
	"context"
	"math"
	"regexp"
	"sort"
	"time"
)

// occulited B-162 (the maintainer, 2026-11-22): the firmware's sticky bits say only that the
// supply dipped at some time since the boot. On his Pi 3 one dip of two seconds in the boot's load
// peak - [9.66] "Undervoltage detected!", [11.68] "Voltage normalised" - showed as "undervoltage
// since the boot" and "throttled since the boot" for the whole uptime, on a supply that has run
// the system for years. The kernel's rpi_volt driver says when each episode began and ended; read
// beside the word, a short dip in the boot phase that normalised is told apart from a supply
// problem: it is an info line, not a warning.

// The rules for a boot dip, against which an undervoltage episode is a warning.
const (
	// BootPhase is the boot's load peak: the radio module, USB and every service starting (on a
	// Pi 3 hmipserver's Java is the last, well within two minutes)
	BootPhase = 120 * time.Second
	// bootDipMax is the longest episode a boot dip may be
	bootDipMax = 10 * time.Second
	// bootDipsMax and bootDipTotal: more episodes, or longer together, add up to a warning
	bootDipsMax  = 3
	bootDipTotal = 20 * time.Second
	// voltageEvery is how long the kernel's episodes are reused: each read is one journalctl run
	voltageEvery = 60 * time.Second
	// voltageKept is how many episodes the answer lists, the newest
	voltageKept = 20
)

// The throttle's states: what the panel's pill and its line say.
const (
	ThrottleNormal       = "normal"       // nothing since the boot
	ThrottleBootDip      = "boot_dip"     // a short undervoltage in the boot phase that normalised: info
	ThrottleEarlier      = "earlier"      // undervoltage or throttling since the boot, beyond a boot dip: warning
	ThrottleThrottled    = "throttled"    // throttled now: warning
	ThrottleUnderVoltage = "undervoltage" // undervoltage now: error
)

// VoltageEpisode is one undervoltage episode of this boot by the kernel's messages.
type VoltageEpisode struct {
	// Start is when it began on the wall clock (RFC 3339); absent when the line carried no time
	Start string `json:"start,omitempty"`
	// StartS is when it began, in seconds since the boot (the kernel's own stamp); -1 unknown
	StartS float64 `json:"start_s"`
	// DurationS is how long it lasted; absent while it has not normalised
	DurationS *float64 `json:"duration_s,omitempty"`
}

var (
	// rpi_volt (raspberrypi-hwmon): "hwmon hwmon1: Undervoltage detected!", and the older kernels'
	// "Under-voltage detected! (0x00050005)"
	underVoltageRe = regexp.MustCompile(`(?i)\bunder-?voltage detected`)
	// "hwmon hwmon1: Voltage normalised", the older "Voltage normalised (0x00000000)"
	voltageNormalRe = regexp.MustCompile(`(?i)\bvoltage normali[sz]ed`)
)

// VoltageQuery is the kernel log read the episodes come from: this boot's kernel lines with the word.
var VoltageQuery = LogQuery{Kernel: true, Boot: "0", Contains: "voltage", Limit: 1000}

func seconds(us int64) float64 { return math.Round(float64(us)/1e5) / 10 }

// VoltageEpisodes pairs the kernel's lines into episodes, oldest first: "detected" opens one
// (again while one is open: the same episode), "normalised" closes the open one. A line without
// the kernel's stamp has an unknown start (-1) and no duration.
func VoltageEpisodes(lines []LogLine) []VoltageEpisode {
	ls := make([]LogLine, 0, len(lines))
	for _, l := range lines {
		if underVoltageRe.MatchString(l.Message) || voltageNormalRe.MatchString(l.Message) {
			ls = append(ls, l)
		}
	}
	stamped := true
	for _, l := range ls {
		stamped = stamped && l.MonotonicUS > 0
	}
	if stamped {
		sort.SliceStable(ls, func(i, j int) bool { return ls[i].MonotonicUS < ls[j].MonotonicUS })
	}
	var out []VoltageEpisode
	open := -1
	var openUS int64
	for _, l := range ls {
		if underVoltageRe.MatchString(l.Message) {
			if open >= 0 {
				continue
			}
			e := VoltageEpisode{Start: l.Timestamp, StartS: -1}
			if l.MonotonicUS > 0 {
				e.StartS = seconds(l.MonotonicUS)
			}
			out = append(out, e)
			open, openUS = len(out)-1, l.MonotonicUS
			continue
		}
		if open < 0 {
			continue
		}
		if openUS > 0 && l.MonotonicUS >= openUS {
			d := seconds(l.MonotonicUS - openUS)
			out[open].DurationS = &d
		} else {
			// normalised, but how long it took is not known: no stamps
			d := -1.0
			out[open].DurationS = &d
		}
		open = -1
	}
	return out
}

// isBootDip says whether the episodes are a boot dip: at least one, each begun in the boot phase,
// normalised within bootDipMax, at most bootDipsMax of them and no longer than bootDipTotal together.
func isBootDip(eps []VoltageEpisode) bool {
	if len(eps) == 0 || len(eps) > bootDipsMax {
		return false
	}
	var total float64
	for _, e := range eps {
		if e.StartS < 0 || e.StartS >= BootPhase.Seconds() || e.DurationS == nil || *e.DurationS < 0 || *e.DurationS > bootDipMax.Seconds() {
			return false
		}
		total += *e.DurationS
	}
	return total <= bootDipTotal.Seconds()
}

// assess fills the throttle's history and its state. eps is nil when the kernel log could not be
// read; late says the reader saw a sticky bit appear, or a condition hold, after the boot phase;
// uptime closes an open episode for the total.
func (th *Throttle) assess(eps []VoltageEpisode, read, late bool, uptime time.Duration) {
	if read {
		n := len(eps)
		th.Events = &n
		var total float64
		for _, e := range eps {
			switch {
			case e.DurationS != nil && *e.DurationS >= 0:
				total += *e.DurationS
			case e.DurationS == nil && e.StartS >= 0 && uptime > 0:
				total += math.Max(0, uptime.Seconds()-e.StartS)
			}
		}
		th.UnderVoltageS = math.Round(total*10) / 10
		if n > 0 {
			last := eps[n-1]
			th.Last = &last
		}
		if n > voltageKept {
			eps = eps[n-voltageKept:]
		}
		th.Episodes = eps
	}
	s := th.SinceBoot
	switch {
	case th.Now.UnderVoltage:
		th.State = ThrottleUnderVoltage
	case th.Now.Throttled || th.Now.FreqCapped || th.Now.SoftTempLimit:
		th.State = ThrottleThrottled
	case !s.Any() && (th.Events == nil || *th.Events == 0):
		th.State = ThrottleNormal
	case read && isBootDip(eps) && !late && !s.SoftTempLimit:
		// the throttling and the capped clock since the boot are the firmware's answer to that dip;
		// a soft temperature limit is a reason of its own
		th.State = ThrottleBootDip
	default:
		th.State = ThrottleEarlier
	}
}

// voltage answers this boot's episodes from the kernel log, cached for voltageEvery; false when
// there is no reader or it failed.
func (t *ThermalReader) voltage(ctx context.Context) ([]VoltageEpisode, bool) {
	if t.Kernel == nil {
		return nil, false
	}
	t.vmu.Lock()
	defer t.vmu.Unlock()
	if !t.vat.IsZero() && time.Since(t.vat) < voltageEvery && time.Since(t.vat) >= 0 {
		return t.eps, t.vok
	}
	lines, err := t.Kernel(ctx)
	t.vat = time.Now()
	t.eps, t.vok = nil, err == nil
	if err == nil {
		t.eps = VoltageEpisodes(lines)
	}
	return t.eps, t.vok
}
