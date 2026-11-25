package system

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// occulited task 177 (the maintainer, 2026-11-24): the temperature panel shows the processor's
// clock, and when and why the firmware capped it. rpi3-1 said "clock capped since the start" with
// a good supply: get_throttled 0x20000, bit 17 alone (ARM frequency capping occurred), sticky since
// a boot 44 h before, and nothing anywhere said when. The firmware's word has no time in it, so the
// throttle sampler (every read of the word, at most every 30 s) notes the moment each sticky bit
// first appears, with the temperature and the clock of that moment. A bit that is set at the first
// read of this boot appeared before it; the record says so instead of a time it does not know.
// The bits are sticky per boot, so the record is too: it is kept across a restart of occulited in
// its runtime directory (/run, gone with the boot) and dropped when the boot id changes.

// CPUClock is the processor's clock: the firmware's measured ARM clock where vcgencmd answered it
// (measure_clock arm, the real clock also while the firmware caps it), else cpufreq's current
// one; Max is cpufreq's maximum. MHz, 0 for unknown.
type CPUClock struct {
	CurMHz int    `json:"cur_mhz"`
	MaxMHz int    `json:"max_mhz,omitempty"`
	Source string `json:"source"` // firmware or cpufreq
}

// ThrottleCause is the first time one of the firmware's sticky bits was seen this boot.
type ThrottleCause struct {
	// Flag is the condition: under_voltage, freq_capped, throttled or soft_temp_limit.
	Flag string `json:"flag"`
	// At is when occulited first saw the bit; Before says it was already set at occulited's first
	// read of this boot, so it appeared at some time before At.
	At     time.Time `json:"at"`
	Before bool      `json:"before,omitempty"`
	// UptimeS is the boot's age at At, in seconds.
	UptimeS float64 `json:"uptime_s"`
	// Celsius and ClockMHz are the processor's temperature and clock at At, absent when unknown.
	Celsius  *float64 `json:"celsius,omitempty"`
	ClockMHz int      `json:"clock_mhz,omitempty"`
}

// flagNames are the four conditions in the firmware's bit order (bit 16 + i).
var flagNames = [4]string{"under_voltage", "freq_capped", "throttled", "soft_temp_limit"}

var measuredClockRe = regexp.MustCompile(`frequency\(\d+\)=(\d+)`)

// parseMeasuredClock reads `vcgencmd measure_clock arm`'s "frequency(48)=1500398464" in MHz.
func parseMeasuredClock(out string) int {
	m := measuredClockRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	hz, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || hz <= 0 {
		return 0
	}
	return int(math.Round(float64(hz) / 1e6))
}

// cpufreqMHz reads one of cpu0's cpufreq files (kHz) in MHz, 0 when it is not there.
func (r Root) cpufreqMHz(name string) int {
	v, err := strconv.ParseInt(strings.TrimSpace(readFile(r.join("/sys/devices/system/cpu/cpu0/cpufreq/"+name))), 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return int(math.Round(float64(v) / 1000))
}

// CPUClock is the clock from the firmware's answer, else cpufreq; nil when neither tells.
func (r Root) CPUClock(firmwareOut string) *CPUClock {
	c := &CPUClock{CurMHz: parseMeasuredClock(firmwareOut), MaxMHz: r.cpufreqMHz("cpuinfo_max_freq"), Source: "firmware"}
	if c.CurMHz == 0 {
		c.CurMHz, c.Source = r.cpufreqMHz("scaling_cur_freq"), "cpufreq"
	}
	if c.CurMHz == 0 {
		return nil
	}
	return c
}

// throttleRecord is the state file: this boot's causes.
type throttleRecord struct {
	BootID string          `json:"boot_id"`
	Causes []ThrottleCause `json:"causes"`
}

// loadCausesLocked reads the state file once per process; a record of another boot is dropped.
func (t *ThermalReader) loadCausesLocked() {
	if t.causesLoaded {
		return
	}
	t.causesLoaded = true
	if t.StateFile == "" {
		return
	}
	b, err := os.ReadFile(t.StateFile)
	if err != nil {
		return
	}
	var rec throttleRecord
	if json.Unmarshal(b, &rec) != nil || rec.BootID == "" || rec.BootID != t.Root.BootID() {
		return
	}
	t.causes = rec.Causes
}

// saveCausesLocked writes the record (a failure only costs the record across a restart).
func (t *ThermalReader) saveCausesLocked() {
	if t.StateFile == "" {
		return
	}
	b, err := json.Marshal(throttleRecord{BootID: t.Root.BootID(), Causes: t.causes})
	if err != nil {
		return
	}
	// the daemon's own write (as iowatch keeps its figures): the file is occulited's, never the
	// helper's, so it needs no privilege
	tmp := t.StateFile + ".tmp"
	err = os.WriteFile(tmp, b, 0o600)
	if err == nil {
		err = os.Rename(tmp, t.StateFile)
	}
	if err != nil && t.Log != nil {
		t.Log.Debug("temperature: the throttling record could not be kept", "err", err)
	}
}

// noteCausesLocked records the sticky bits of w that this boot has no cause for yet. first says
// this is this process's first word: a bit set then that the record does not hold appeared before
// it (before occulited started, or while it restarted). Called with mu held.
func (t *ThermalReader) noteCausesLocked(w uint32, first bool, clock *CPUClock) {
	t.loadCausesLocked()
	have := map[string]bool{}
	for _, c := range t.causes {
		have[c.Flag] = true
	}
	sticky := (w >> 16) & 0xf
	var fresh []ThrottleCause
	for i, name := range flagNames {
		if sticky&(1<<i) == 0 || have[name] {
			continue
		}
		c := ThrottleCause{Flag: name, At: time.Now().UTC(), Before: first}
		if up, ok := t.Root.Uptime(); ok {
			c.UptimeS = math.Round(up.Seconds())
		}
		if v, ok := SoCTemperature(t.Root.ReadTemperatures(t.Platform)); ok {
			c.Celsius = &v
		}
		if clock != nil {
			c.ClockMHz = clock.CurMHz
		}
		fresh = append(fresh, c)
	}
	if len(fresh) == 0 {
		return
	}
	t.causes = append(t.causes, fresh...)
	t.saveCausesLocked()
}

// Causes is a copy of this boot's record.
func (t *ThermalReader) Causes() []ThrottleCause {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]ThrottleCause(nil), t.causes...)
}
