package httpapi

// occulited task 163: the radio writes per lite-rpc client - the Status page's panel "Radio
// writes", the client list's rate, and the two Status warnings. The counts are
// internal/radiowrites', fed by lite-rpc's request paths (cmd/occulited); the duty cycle beside
// them is the radio sampler's history (internal/health).

import (
	"context"
	"net/http"
	"strings"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/devstate"
	"github.com/hobbyquaker/occulited/internal/literpc"
	"github.com/hobbyquaker/occulited/internal/radiowrites"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// The Status warnings of the radio writes: the hint per client, and the warning for the client
// behind a duty cycle's rise.
const (
	RadioWritesID     = "radio-writes"
	RadioWritesDutyID = "radio-writes-duty-cycle"
)

func (a *SystemAPI) registerRadioWrites(mux *http.ServeMux, p string) {
	route(mux, auth.ScopeSystemRead, "GET "+p+"/radio/writes", a.radioWrites)
}

// radioWritesView is GET /radio/writes.
type radioWritesView struct {
	radiowrites.View
	// Available: false on a system that does not count (no lite-rpc: development).
	Available bool `json:"available"`
	// ClassicRPC: the classic RPC ports are open. Their clients reach the interface processes
	// without passing here and are not counted.
	ClassicRPC bool `json:"classic_rpc"`
}

// radioWritesNow is the tracker's view with the radio sampler's duty cycles and the running
// device firmware transfers.
func (a *SystemAPI) radioWritesNow() radiowrites.View {
	var duty []radiowrites.DutySeries
	if a.Health != nil {
		for key, samples := range a.Health.Status().History {
			iface, address, _ := strings.Cut(key, "/")
			if !radiowrites.IsRadio(iface) {
				continue
			}
			ds := radiowrites.DutySeries{Interface: iface, Address: address}
			for _, s := range samples {
				ds.Samples = append(ds.Samples, radiowrites.DutySample{Time: s.Time, DutyCycle: s.DutyCycle, Connected: s.Connected})
			}
			duty = append(duty, ds)
		}
	}
	v := a.RadioWrites.View(duty, a.transferringInterfaces())
	if a.TokenClient != nil {
		for i := range v.Clients {
			if v.Clients[i].Kind == radiowrites.KindToken {
				v.Clients[i].Label = a.TokenClient(v.Clients[i].Name)
			}
		}
	}
	return v
}

func (a *SystemAPI) radioWrites(w http.ResponseWriter, _ *http.Request) {
	out := radioWritesView{View: radiowrites.View{Thresholds: radiowrites.TheThresholds(), Clients: []radiowrites.ClientView{}, DutyCycle: []radiowrites.DutyView{}}}
	if a.RadioWrites != nil {
		out.View, out.Available = a.radioWritesNow(), true
	}
	if a.ClassicRPC != nil {
		c := a.Root.ReadClassicRPC()
		out.ClassicRPC = c.Plain || c.TLS
	}
	writeJSON(w, 200, out)
}

// radioWritesWarnings (occulited task 163): per client the hint - many radio writes, on average
// or in one minute - or, in its place, the warning when the duty cycle rose and the client sent
// at least half of that interface's radio writes meanwhile. The variant is the client (and for
// the warning the interface), so another client is another warning and a silence of one does not
// cover the next.
func (a *SystemAPI) radioWritesWarnings(context.Context) ([]warnings.Warning, bool) {
	if a.RadioWrites == nil {
		return nil, true
	}
	return radioWritesRule(a.radioWritesNow()), true
}

func radioWritesRule(v radiowrites.View) []warnings.Warning {
	var out []warnings.Warning
	for _, c := range v.Clients {
		params := map[string]any{"client": c.Name, "kind": c.Kind}
		if c.Label != "" {
			params["label"] = c.Label
		}
		switch {
		case c.Level == radiowrites.LevelWarning && c.Cause != nil:
			params["interface"], params["dc_from"], params["dc_to"] = c.Cause.Interface, c.Cause.From, c.Cause.To
			params["writes"], params["share_percent"], params["minutes"] = c.Cause.Writes, c.Cause.SharePercent, radiowrites.RiseMinutes
			out = append(out, warnings.Warning{ID: RadioWritesDutyID, Variant: c.Key() + "@" + c.Cause.Interface, Severity: warnings.SeverityError, Href: "#radio-writes", Params: params})
		case c.Level == radiowrites.LevelHint && c.Hint != nil:
			params["burst"], params["average"] = c.Hint.Burst, c.Hint.Average
			params["peak_minute"], params["peak_average"] = c.Hint.PeakMinute, c.Hint.PeakAverage
			params["average_minutes"] = radiowrites.AvgMinutes
			out = append(out, warnings.Warning{ID: RadioWritesID, Variant: c.Key(), Severity: warnings.SeverityWarning, Href: "#radio-writes", Params: params})
		}
	}
	return out
}

// RadioWriting is lite-rpc's Writing hook (occulited task 163): a setValue or putParamset that
// goes to a radio device is counted for its client once the interface took it - the caller
// liteContext put into the context, or the system itself. Whether it changes anything is asked
// here, before the call goes out: the state store's value is the one from before the write only
// until the interface's event arrives. Only a value the store has heard since occulited started
// counts as known; one restored from the file may be stale.
func RadioWriting(t *radiowrites.Tracker, state *devstate.Store) func(ctx context.Context, iface string, c literpc.Call) func(error) {
	return func(ctx context.Context, iface string, c literpc.Call) func(error) {
		address, ok := literpc.Address(c)
		if !ok || !t.Counted(iface, address) {
			return nil
		}
		client := radiowrites.Client{Kind: radiowrites.KindSystem, Name: "occulited"}
		if kind, name, ok := literpc.ClientOf(ctx); ok {
			client = radiowrites.Client{Kind: kind, Name: name}
		}
		args := make([]any, 0, len(c.Params))
		for _, p := range c.Params[1:] {
			args = append(args, literpc.ToJSON(p))
		}
		noop := radiowrites.Noop(c.Method, address, args, func(address, datapoint string) (any, bool) {
			if state == nil {
				return nil, false
			}
			e, ok := state.Get(iface, address, datapoint)
			return e.Value, ok && e.Confirmed
		})
		return func(err error) {
			if literpc.TookWrite(err) {
				t.Record(client, iface, noop)
			}
		}
	}
}
