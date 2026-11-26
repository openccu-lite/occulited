package system

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// The Status page's temperature panel (occulited task 148, the maintainer 2026-11-15): one ring
// per reading - every thermal zone, the hwmon sensors no zone already covers, and the SMART
// temperature of every disk that reports one - beside the Raspberry Pi firmware's throttling and
// undervoltage state. A system without any sensor (the VM) has no readings and no panel.
//
// Each reading carries the two levels it is read against: Warn, where it gets hot (a Pi's
// firmware starts to throttle the ARM cores at 80 °C), and Max, the end of its ring's scale and
// its error level (a Pi's hard limit of 85 °C, where the GPU is throttled as well) - not the
// thermal zone's critical trip of 110 °C, which only ends in a shutdown.

// Temperature kinds.
const (
	TempSoC    = "soc"    // the processor: a Pi's SoC, a PC's CPU package
	TempDisk   = "disk"   // a disk's SMART temperature
	TempSensor = "sensor" // any other sensor (a board, a chipset)
)

// The Raspberry Pi firmware's limits (all models): from 80 °C the ARM cores are throttled step
// by step, at 85 °C the ARM cores and the GPU.
const (
	PiSoftLimitC = 80
	PiHardLimitC = 85
)

// The levels of a reading that names none of its own.
const (
	defaultWarnC     = 80
	defaultMaxC      = 90
	defaultDiskWarnC = 70 // a disk without a SMART limit: most SSDs are rated to 70 °C
	diskSpanC        = 10 // a disk's ring ends this far above its warning level without a critical limit
)

// Temperature is one reading.
type Temperature struct {
	// ID is stable for the reading: thermal_zone0, hwmon2, disk:sda
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Name is the sensor's own name (cpu-thermal, coretemp) or the disk's model
	Name string `json:"name"`
	// Device and DiskKind are a disk's kernel name and kind (sd, usb, sata, nvme)
	Device   string  `json:"device,omitempty"`
	DiskKind string  `json:"disk_kind,omitempty"`
	Celsius  float64 `json:"celsius"`
	WarnC    float64 `json:"warn_c"`
	MaxC     float64 `json:"max_c"`
	// ReadAt is when a disk's SMART was read (at most once an hour)
	ReadAt *time.Time `json:"read_at,omitempty"`
}

// Level is the reading's level: ok, warn (at or above WarnC) or error (at or above MaxC).
func (t Temperature) Level() string {
	switch {
	case t.MaxC > 0 && t.Celsius >= t.MaxC:
		return "error"
	case t.WarnC > 0 && t.Celsius >= t.WarnC:
		return "warn"
	}
	return "ok"
}

// Throttle is the Raspberry Pi firmware's get_throttled word: what holds now, and what has held
// since the boot. Source is "firmware" (the whole word, vcgencmd through the helper) or "hwmon"
// (only the undervoltage now, rpi_volt's alarm, when the firmware could not be asked).
type Throttle struct {
	Source    string        `json:"source"`
	Raw       *uint32       `json:"raw,omitempty"`
	Now       ThrottleFlags `json:"now"`
	SinceBoot ThrottleFlags `json:"since_boot"`
	// State is what the panel says (B-162): normal, boot_dip, earlier, throttled or undervoltage
	State string `json:"state"`
	// Events is the number of undervoltage episodes of this boot by the kernel's messages (rpi_volt),
	// UnderVoltageS their seconds together, Last the newest and Episodes the newest voltageKept;
	// all absent when the kernel log was not read (nothing to explain) or could not be
	Events        *int             `json:"events,omitempty"`
	UnderVoltageS float64          `json:"undervoltage_s,omitempty"`
	Last          *VoltageEpisode  `json:"last,omitempty"`
	Episodes      []VoltageEpisode `json:"episodes,omitempty"`
	// Clock is the processor's clock now (task 177), absent when neither the firmware nor cpufreq
	// tells; Causes when each sticky bit first held this boot and the temperature and clock then
	Clock  *CPUClock       `json:"clock,omitempty"`
	Causes []ThrottleCause `json:"causes,omitempty"`
	// Supply is the power supply's way in (B-186): rpi-rf-mod (through the RPI-RF-MOD on the
	// header), pi (the Pi's own socket), absent while the radio detection has not answered
	Supply string `json:"supply,omitempty"`
}

// ThrottleFlags are the four conditions of the firmware's word.
type ThrottleFlags struct {
	UnderVoltage  bool `json:"under_voltage"`
	FreqCapped    bool `json:"freq_capped"`
	Throttled     bool `json:"throttled"`
	SoftTempLimit bool `json:"soft_temp_limit"`
}

// Any says whether one of the flags is set.
func (f ThrottleFlags) Any() bool {
	return f.UnderVoltage || f.FreqCapped || f.Throttled || f.SoftTempLimit
}

// ThrottleFromWord splits the firmware's word: bits 0-3 now, bits 16-19 since the boot.
func ThrottleFromWord(w uint32) Throttle {
	flags := func(b uint32) ThrottleFlags {
		return ThrottleFlags{UnderVoltage: b&1 != 0, FreqCapped: b&2 != 0, Throttled: b&4 != 0, SoftTempLimit: b&8 != 0}
	}
	raw := w
	return Throttle{Source: "firmware", Raw: &raw, Now: flags(w), SinceBoot: flags(w >> 16)}
}

var throttledRe = regexp.MustCompile(`throttled=0x([0-9a-fA-F]{1,8})`)

// ParseThrottled reads `vcgencmd get_throttled`'s answer, "throttled=0x50005".
func ParseThrottled(out string) (uint32, bool) {
	m := throttledRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseUint(m[1], 16, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// Thermal is the temperature panel's answer.
type Thermal struct {
	Readings []Temperature `json:"readings"`
	Throttle *Throttle     `json:"throttle,omitempty"`
}

// isPi says whether the platform is a Raspberry Pi product (rpi3, rpi4, rpi5).
func isPi(platform string) bool { return strings.HasPrefix(platform, "rpi") }

// socName says whether a thermal zone's type or a hwmon chip's name is the processor's.
func socName(name string) bool {
	n := strings.ToLower(name)
	for _, s := range []string{"cpu", "soc", "x86_pkg_temp", "coretemp", "k10temp", "zenpower"} {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

// diskHwmon are the hwmon chips of disks: their temperature is SMART's, read per disk.
var diskHwmon = map[string]bool{"nvme": true, "drivetemp": true}

// sysMilli reads a sysfs millidegree file; false when it is missing, unreadable or implausible.
func sysMilli(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || v <= -50000 || v >= 200000 {
		return 0, false
	}
	return math.Round(float64(v)/100) / 10, true
}

func sysString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// numbered sorts names like thermal_zone10 after thermal_zone9.
func numbered(prefix string, names []string) []string {
	n := func(s string) int { v, _ := strconv.Atoi(strings.TrimPrefix(s, prefix)); return v }
	sort.Slice(names, func(i, j int) bool { return n(names[i]) < n(names[j]) })
	return names
}

// levels picks a reading's Warn and Max: a Pi's SoC by the firmware's limits, else what the
// sensor names (warn: a passive trip or a max; max: a critical or hot trip or a crit), else
// the defaults; Max always above Warn.
func levels(kind, platform string, lo, hi float64) (float64, float64) {
	if kind == TempSoC && isPi(platform) {
		return PiSoftLimitC, PiHardLimitC
	}
	switch {
	case lo <= 0 && hi <= 0:
		return defaultWarnC, defaultMaxC
	case lo <= 0:
		return hi - 10, hi
	case hi <= lo:
		return lo, lo + 10
	}
	return lo, hi
}

// ReadTemperatures reads the thermal zones and the hwmon sensors no zone covers; platform is
// /VERSION's (a Pi's SoC is read against the firmware's limits). None in a container: the
// sensors it might see are the host's.
func (r Root) ReadTemperatures(platform string) []Temperature {
	if r.Container() != "" {
		return nil
	}
	var out []Temperature
	covered := map[string]bool{} // hwmon dirs and names a zone already reads
	zdir := r.join("/sys/class/thermal")
	var zones []string
	if es, err := os.ReadDir(zdir); err == nil {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), "thermal_zone") {
				zones = append(zones, e.Name())
			}
		}
	}
	for _, z := range numbered("thermal_zone", zones) {
		dir := filepath.Join(zdir, z)
		c, ok := sysMilli(filepath.Join(dir, "temp"))
		if !ok {
			continue
		}
		typ := sysString(filepath.Join(dir, "type"))
		// the zone's own hwmon device (thermal_zone0/hwmon0) is the same sensor again
		if es, err := os.ReadDir(dir); err == nil {
			for _, e := range es {
				if strings.HasPrefix(e.Name(), "hwmon") {
					covered[e.Name()] = true
				}
			}
		}
		covered["name:"+strings.ReplaceAll(typ, "-", "_")] = true
		var lo, hi float64
		for i := 0; i < 16; i++ {
			tt := sysString(filepath.Join(dir, "trip_point_"+strconv.Itoa(i)+"_type"))
			if tt == "" {
				break
			}
			v, ok := sysMilli(filepath.Join(dir, "trip_point_"+strconv.Itoa(i)+"_temp"))
			if !ok || v <= 0 {
				continue
			}
			switch tt {
			case "passive": // an active trip only starts a fan
				if lo == 0 || v < lo {
					lo = v
				}
			case "hot", "critical":
				if hi == 0 || v < hi {
					hi = v
				}
			}
		}
		kind := TempSensor
		if socName(typ) {
			kind = TempSoC
		}
		w, m := levels(kind, platform, lo, hi)
		out = append(out, Temperature{ID: z, Kind: kind, Name: typ, Celsius: c, WarnC: w, MaxC: m})
	}
	hdir := r.join("/sys/class/hwmon")
	var chips []string
	if es, err := os.ReadDir(hdir); err == nil {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), "hwmon") {
				chips = append(chips, e.Name())
			}
		}
	}
	for _, h := range numbered("hwmon", chips) {
		dir := filepath.Join(hdir, h)
		name := sysString(filepath.Join(dir, "name"))
		if covered[h] || covered["name:"+strings.ReplaceAll(name, "-", "_")] || diskHwmon[name] {
			continue
		}
		// the chip's first temperature (a CPU package's, before its cores): one reading per chip
		for i := 1; i <= 32; i++ {
			p := filepath.Join(dir, "temp"+strconv.Itoa(i)+"_")
			c, ok := sysMilli(p + "input")
			if !ok {
				continue
			}
			label := name
			if l := sysString(p + "label"); l != "" && name != "" {
				label = name + " " + l
			} else if l != "" {
				label = l
			}
			lo, _ := sysMilli(p + "max")
			hi, _ := sysMilli(p + "crit")
			kind := TempSensor
			if socName(name) {
				kind = TempSoC
			}
			w, m := levels(kind, platform, lo, hi)
			out = append(out, Temperature{ID: h, Kind: kind, Name: label, Celsius: c, WarnC: w, MaxC: m})
			break
		}
	}
	// the processor first
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind == TempSoC && out[j].Kind != TempSoC })
	return out
}

// SoCTemperature is the first processor reading, for the history graph (syshist); false without one.
func SoCTemperature(list []Temperature) (float64, bool) {
	for _, t := range list {
		if t.Kind == TempSoC {
			return t.Celsius, true
		}
	}
	return 0, false
}

// UnderVoltageNow reads rpi_volt's alarm (hwmon), the undervoltage the kernel's driver reports
// without the firmware's mailbox; false, false without the chip.
func (r Root) UnderVoltageNow() (under, ok bool) {
	hdir := r.join("/sys/class/hwmon")
	es, err := os.ReadDir(hdir)
	if err != nil {
		return false, false
	}
	for _, e := range es {
		dir := filepath.Join(hdir, e.Name())
		if sysString(filepath.Join(dir, "name")) != "rpi_volt" {
			continue
		}
		v := sysString(filepath.Join(dir, "in0_lcrit_alarm"))
		if v == "" {
			return false, false
		}
		return v != "0", true
	}
	return false, false
}

// DiskTemperatureLevels are a disk's levels from SMART: its operating (NVMe: warning) limit,
// else 70 °C; its critical limit, else ten above.
func DiskTemperatureLevels(opMax, critMax int) (float64, float64) {
	lo := float64(defaultDiskWarnC)
	if opMax > 0 {
		lo = float64(opMax)
	}
	hi := lo + diskSpanC
	if float64(critMax) > lo {
		hi = float64(critMax)
	}
	return lo, hi
}

// throttleEvery is how long the firmware's word is reused: the Status page reads every ten
// seconds, and each read is a run of vcgencmd through the helper.
const throttleEvery = 30 * time.Second

// ThermalReader answers the temperature panel: the sensors, the disks' SMART temperatures of the
// storage report and, on a Raspberry Pi, the firmware's throttling word.
type ThermalReader struct {
	Root     Root
	Platform string
	// Disks are the disks' temperatures (Storage.DiskTemperatures); nil = none
	Disks func() []Temperature
	// Throttled returns `vcgencmd get_throttled`'s output; nil = through the privilege helper on a
	// real box (Root "/") and nothing anywhere else
	Throttled func(ctx context.Context) (string, error)
	Log       *slog.Logger
	// Kernel reads this boot's kernel lines about the supply voltage (VoltageQuery: the journal, or
	// the ring buffer without one); nil = no history, the sticky bits alone (B-162)
	Kernel func(ctx context.Context) ([]LogLine, error)

	mu       sync.Mutex
	at       time.Time
	word     *uint32
	prev     *uint32     // the word before, to see a sticky bit appear
	late     bool        // a condition held now after the boot phase (B-162, B-169)
	appeared uint32      // sticky bits seen to appear after the boot phase, logged once each (B-169)
	reread   atomic.Bool // a sticky bit appeared: the kernel's episodes are read again at once
	vmu      sync.Mutex
	vat      time.Time
	eps      []VoltageEpisode
	vok      bool
	noVcgen  bool // the helper said vcgencmd is not installed: not asked again
	reported bool // a failed read is logged once
	// task 177: the firmware's last answer (its measured clock, where the helper sent one), this
	// boot's causes and whether the state file was read
	out          string
	causes       []ThrottleCause
	causesLoaded bool
	// B-186: the open episode last logged as ended by the firmware's word
	unended    float64
	unendedSet bool
	// StateFile keeps the causes across a restart of occulited within one boot (task 177); "" =
	// this process only
	StateFile string
}

// Read answers the panel; nil without any reading (the VM, a container).
func (t *ThermalReader) Read(ctx context.Context) *Thermal {
	list := t.Root.ReadTemperatures(t.Platform)
	if t.Disks != nil && t.Root.Container() == "" {
		list = append(list, t.Disks()...)
	}
	if len(list) == 0 {
		return nil
	}
	return &Thermal{Readings: list, Throttle: t.throttle(ctx)}
}

// throttle is the firmware's word on a Pi, else rpi_volt's undervoltage alone, else nil.
func (t *ThermalReader) throttle(ctx context.Context) *Throttle {
	if !isPi(t.Platform) {
		return nil
	}
	var th Throttle
	if w, ok := t.firmwareWord(ctx); ok {
		th = ThrottleFromWord(w)
	} else if under, ok := t.Root.UnderVoltageNow(); ok {
		th = Throttle{Source: "hwmon", Now: ThrottleFlags{UnderVoltage: under}}
		if under {
			t.markLate()
		}
	} else {
		return nil
	}
	// the kernel's episodes only when there is something to explain; the alarm alone says nothing
	// of the past, so with it they are always read
	var eps []VoltageEpisode
	read := false
	if th.Source == "hwmon" || th.SinceBoot.Any() || th.Now.Any() {
		eps, read = t.voltage(ctx)
	}
	t.mu.Lock()
	late := t.late
	out := t.out
	if len(t.causes) > 0 {
		th.Causes = append([]ThrottleCause(nil), t.causes...)
	}
	t.mu.Unlock()
	if read && !th.Now.UnderVoltage {
		t.noteUnended(eps, th)
	}
	up, _ := t.Root.Uptime()
	th.assess(eps, read, late, up)
	th.Clock = t.Root.CPUClock(out) // task 177
	th.Supply = PowerSupply(t.Root.HMMode())
	return &th
}

// noteUnended logs once per episode that the kernel log has no end for it while the supply is
// fine now (B-186), with what rpi_volt's alarm says, for the next such case: the driver prints
// "Voltage normalised" once the firmware's sticky bit is clear at its two-second poll.
func (t *ThermalReader) noteUnended(eps []VoltageEpisode, th Throttle) {
	e, ok := openEpisode(eps)
	if !ok {
		return
	}
	t.mu.Lock()
	seen := t.unended == e.StartS && t.unendedSet
	t.unended, t.unendedSet = e.StartS, true
	t.mu.Unlock()
	if seen {
		return
	}
	log := t.Log
	if log == nil {
		log = slog.Default()
	}
	alarm, aok := t.Root.UnderVoltageNow()
	raw := ""
	if th.Raw != nil {
		raw = fmt.Sprintf("0x%x", *th.Raw)
	}
	log.Info("temperature: the kernel log has no end for an undervoltage episode, but the supply is fine now; drawn as ended", "start", e.Start, "start_s", e.StartS, "word", raw, "rpi_volt_alarm", alarm, "rpi_volt_read", aok)
}

// The power supply's way into the system on a Raspberry Pi (B-186), for the advice of the warning
// power-throttle: with an RPI-RF-MOD on the Pi's header (a Charly, a Pi with the module) the
// system is powered through the radio module - ELV's power supply goes into the RPI-RF-MOD, not
// into the Pi; with another module (an HmIP-RFUSB, an RPI-RF-MOD on a radio adapter) or none it
// is the Pi's own socket. "" while the radio detection has not answered.
const (
	SupplyRPIRFMOD = "rpi-rf-mod"
	SupplyPi       = "pi"
)

// PowerSupply reads it from /var/hm_mode.
func PowerSupply(kv map[string]string) string {
	if kv["HM_MODE"] == "" {
		return ""
	}
	for _, role := range []string{"HMIP", "HMRF"} {
		if kv["HM_"+role+"_DEV"] != "RPI-RF-MOD" {
			continue
		}
		t, _, _ := strings.Cut(strings.ToLower(kv["HM_"+role+"_DEVTYPE"]), "@")
		if !strings.HasPrefix(t, "hb-rf-") {
			return SupplyRPIRFMOD
		}
	}
	return SupplyPi
}

// markLate notes a condition that held after the boot phase.
func (t *ThermalReader) markLate() {
	if up, ok := t.Root.Uptime(); ok && up >= BootPhase {
		t.mu.Lock()
		t.late = true
		t.mu.Unlock()
	}
}

// seeLate compares a fresh word with the one before. Past the boot phase, a condition that holds
// now is not the boot's: late. A sticky bit that appears past it is not late by itself (B-169):
// the firmware's word can show the bits of a boot dip later than the kernel's lines say the dip
// was - the production Pi 3 had one dip at 9.4 s, rpi_volt's lines and 0x50000, and was assessed
// "earlier" all the same - so the kernel's episodes decide (an undervoltage after the boot phase
// is an episode of its own there), and a throttling with another cause carries the soft
// temperature limit's bit. Such a bit is logged once and has the episodes read again at once.
// Called with mu held.
func (t *ThermalReader) seeLate(w uint32) {
	up, ok := t.Root.Uptime()
	past := ok && up >= BootPhase
	if past && w&0xf != 0 {
		t.late = true
	}
	if t.prev != nil {
		if fresh := (w >> 16) &^ (*t.prev >> 16) & 0xf; fresh != 0 {
			t.reread.Store(true)
			if past && fresh&^t.appeared != 0 {
				t.appeared |= fresh
				log := t.Log
				if log == nil {
					log = slog.Default()
				}
				log.Info("temperature: a throttling bit appeared after the boot phase", "before", fmt.Sprintf("0x%x", *t.prev), "now", fmt.Sprintf("0x%x", w), "uptime_s", int(up.Seconds()))
			}
		}
	}
	p := w
	t.prev = &p
}

func (t *ThermalReader) firmwareWord(ctx context.Context) (uint32, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.at.IsZero() && time.Since(t.at) < throttleEvery && time.Since(t.at) >= 0 {
		if t.word == nil {
			return 0, false
		}
		return *t.word, true
	}
	if t.noVcgen {
		return 0, false
	}
	read := t.Throttled
	if read == nil {
		if string(t.Root) != "/" || Priv == nil {
			return 0, false
		}
		read = func(ctx context.Context) (string, error) {
			r, err := Priv.Throttled(ctx)
			return string(r.Stdout), err
		}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	out, err := read(ctx)
	t.at = time.Now()
	t.word = nil
	t.out = ""
	if errors.Is(err, priv.ErrNotAvailable) {
		t.noVcgen = true
		return 0, false
	}
	w, ok := ParseThrottled(out)
	if !ok {
		if !t.reported {
			t.reported = true
			log := t.Log
			if log == nil {
				log = slog.Default()
			}
			log.Warn("temperature: the firmware's throttling state could not be read", "err", err, "output", strings.TrimSpace(out))
		}
		return 0, false
	}
	t.word = &w
	first := t.prev == nil
	t.seeLate(w)
	t.out = out
	t.noteCausesLocked(w, first, t.Root.CPUClock(out)) // task 177: when a sticky bit first held, and why
	return w, true
}
