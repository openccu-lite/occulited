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
	return thermalRule(a.warn.thermal.update(th.Readings), th.Readings), true
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
