package httpapi

import (
	"context"
	"sync"

	"github.com/hobbyquaker/occulited/internal/system"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// occulited task 148 (the maintainer, 2026-11-15): a Status warning when the system runs hot -
// soc-hot from the processor's warning level (a Pi's firmware throttles from 80 °C), red from its
// maximum (85 °C, where the GPU is throttled too); disk-hot for a disk at or above its own SMART
// limit (70 °C without one), red from its critical limit. The variant is the severity step, so a
// silenced yellow warning comes back as red. A reading that hovers at a level does not switch
// the warning on and off with every read: once raised, a level holds until the temperature is
// thermalHysteresisC below it.

// thermalHysteresisC is how far below a level a reading must fall before the level is left.
const thermalHysteresisC = 3

// thermalLevels keeps each reading's level between evaluations, for the hysteresis.
type thermalLevels struct {
	mu    sync.Mutex
	level map[string]string
}

// next is a reading's level after prev: "", "warning" or "error".
func thermalNext(prev string, t system.Temperature) string {
	c := t.Celsius
	switch {
	case t.MaxC > 0 && (c >= t.MaxC || (prev == warnings.SeverityError && c > t.MaxC-thermalHysteresisC)):
		return warnings.SeverityError
	case t.WarnC > 0 && (c >= t.WarnC || (prev != "" && c > t.WarnC-thermalHysteresisC)):
		return warnings.SeverityWarning
	}
	return ""
}

// update moves every reading's level on and forgets the readings that are gone.
func (l *thermalLevels) update(list []system.Temperature) map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	next := make(map[string]string, len(list))
	for _, t := range list {
		if lv := thermalNext(l.level[t.ID], t); lv != "" {
			next[t.ID] = lv
		}
	}
	l.level = next
	out := make(map[string]string, len(next))
	for k, v := range next {
		out[k] = v
	}
	return out
}

func worse(a, b string) string {
	if a == warnings.SeverityError || b == warnings.SeverityError {
		return warnings.SeverityError
	}
	if a != "" {
		return a
	}
	return b
}

// thermalWarnings evaluates soc-hot and disk-hot over the temperature panel's readings.
func (a *SystemAPI) thermalWarnings(ctx context.Context) ([]warnings.Warning, bool) {
	if a.Thermal == nil {
		return nil, true
	}
	th := a.Thermal.Read(ctx)
	if th == nil {
		a.warn.thermal.update(nil)
		return nil, true
	}
	out := thermalRule(a.warn.thermal.update(th.Readings), th.Readings)
	if w := throttleRule(th.Throttle, out); w != nil {
		out = append(out, *w)
	}
	return out, true
}

// occulited task 173 (the maintainer, 2026-11-23: "if my CCU has been throttled since the start,
// shouldn't the panel have the yellow left border and a message on the status page?"): the
// firmware's throttle state as the warning power-throttle - red while the supply is too low now,
// yellow while the processor is throttled now or was since the boot beyond a boot dip (B-162);
// a boot dip and a quiet firmware raise nothing. The variant is the state, so a silenced "since
// the boot" comes back when it turns into "now". cause says what the page advises: supply (an
// undervoltage now, since the boot, or an episode in the kernel log), heat (the soft temperature
// limit alone), or unknown. A throttling now that is the heat's alone while soc-hot is raised is
// soc-hot's matter: no second warning.
const PowerThrottleID = "power-throttle"

func throttleRule(th *system.Throttle, raised []warnings.Warning) *warnings.Warning {
	if th == nil {
		return nil
	}
	var variant, sev string
	switch th.State {
	case system.ThrottleUnderVoltage:
		variant, sev = th.State, warnings.SeverityError
	case system.ThrottleThrottled, system.ThrottleEarlier:
		variant, sev = th.State, warnings.SeverityWarning
	default:
		return nil
	}
	cause := "unknown"
	switch {
	case th.Now.UnderVoltage || th.SinceBoot.UnderVoltage || (th.Events != nil && *th.Events > 0):
		cause = "supply"
	case th.Now.SoftTempLimit || th.SinceBoot.SoftTempLimit:
		cause = "heat"
	}
	if variant == system.ThrottleThrottled && cause == "heat" {
		for _, w := range raised {
			if w.ID == "soc-hot" {
				return nil
			}
		}
	}
	params := map[string]any{"cause": cause}
	if th.Events != nil {
		params["count"] = *th.Events
	}
	if th.Last != nil && th.Last.Start != "" {
		params["last"] = th.Last.Start
	}
	// B-186: where the power supply goes in, for the advice (the RPI-RF-MOD's own socket, not the Pi's)
	if th.Supply != "" {
		params["supply"] = th.Supply
	}
	return &warnings.Warning{ID: PowerThrottleID, Variant: variant, Severity: sev, Href: "#temperature", Params: params}
}

// thermalRule builds the two warnings from the readings' levels.
func thermalRule(levels map[string]string, list []system.Temperature) []warnings.Warning {
	var out []warnings.Warning
	socSev := ""
	var soc map[string]any
	diskSev := ""
	var disks []map[string]any
	for _, t := range list {
		lv := levels[t.ID]
		if lv == "" {
			continue
		}
		switch t.Kind {
		case system.TempSoC:
			if soc == nil || lv == warnings.SeverityError && socSev != warnings.SeverityError {
				soc = map[string]any{"name": t.Name, "celsius": t.Celsius, "warn_c": t.WarnC, "max_c": t.MaxC}
			}
			socSev = worse(socSev, lv)
		case system.TempDisk:
			disks = append(disks, map[string]any{"device": t.Device, "kind": t.DiskKind, "model": t.Name, "celsius": t.Celsius, "warn_c": t.WarnC, "max_c": t.MaxC})
			diskSev = worse(diskSev, lv)
		}
	}
	if socSev != "" {
		out = append(out, warnings.Warning{ID: "soc-hot", Variant: socSev, Severity: socSev, Href: "#temperature", Params: soc})
	}
	if diskSev != "" {
		out = append(out, warnings.Warning{ID: "disk-hot", Variant: diskSev, Severity: diskSev, Href: "#temperature", Params: map[string]any{"disks": disks}})
	}
	return out
}
