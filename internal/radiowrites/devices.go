package radiowrites

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hobbyquaker/occulited/internal/rpcsub"
)

// DeviceWatch keeps the tracker's excluded devices current: it reads the radio interfaces' device
// lists when an interface comes up and when a device list changes, and tells the tracker which
// devices cost no airtime (NonRadioType). It hangs on the subscriber's bus (rpcsub.Handler).
// Until the first list is read every device of a radio interface counts.
type DeviceWatch struct {
	Tracker *Tracker
	// Types lists the devices of one interface: address → type.
	Types func(ctx context.Context, iface string) (map[string]string, error)
	// Delay gathers a burst of changes into one read; 0 = 5 s.
	Delay time.Duration
	Log   *slog.Logger

	mu      sync.Mutex
	pending map[string]bool
}

// Event is the bus's: datapoint events do not change the device list.
func (w *DeviceWatch) Event(rpcsub.Event) {}

// Interface is the bus's: an interface that came up is read.
func (w *DeviceWatch) Interface(name, state string, _ time.Time) {
	if state == "up" || state == "restarted" || state == "added" {
		w.Refresh(name)
	}
}

// Devices is the bus's: a changed device list is read again.
func (w *DeviceWatch) Devices(iface, _ string, _ []string) { w.Refresh(iface) }

// Refresh reads one radio interface's device list soon, once for every call within the delay.
func (w *DeviceWatch) Refresh(iface string) {
	if !IsRadio(iface) || w.Types == nil || w.Tracker == nil {
		return
	}
	w.mu.Lock()
	if w.pending == nil {
		w.pending = map[string]bool{}
	}
	if w.pending[iface] {
		w.mu.Unlock()
		return
	}
	w.pending[iface] = true
	w.mu.Unlock()
	delay := w.Delay
	if delay == 0 {
		delay = 5 * time.Second
	}
	time.AfterFunc(delay, func() {
		w.mu.Lock()
		delete(w.pending, iface)
		w.mu.Unlock()
		w.read(iface)
	})
}

func (w *DeviceWatch) read(iface string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	types, err := w.Types(ctx, iface)
	if err != nil {
		if w.Log != nil {
			w.Log.Debug("radio writes: the device list could not be read; the wired and virtual devices stay as they were", "interface", iface, "err", err)
		}
		return
	}
	excluded := map[string]bool{}
	for address, typ := range types {
		if NonRadioType(typ) {
			excluded[address] = true
		}
	}
	w.Tracker.SetExcluded(iface, excluded)
}
