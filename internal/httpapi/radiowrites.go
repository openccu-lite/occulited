package httpapi

// occulited task 163: the radio writes per lite-rpc client - the Status page's panel "Radio
// writes", the client list's rate, and the two Status warnings. The counts are
// internal/radiowrites', fed by lite-rpc's request paths (cmd/occulited); the duty cycle beside
// them is the radio sampler's history (internal/health).

import (
	"context"
	"net/http"
	"strconv"
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

// radioWritesNow is the tracker's view of the last hour, for the warnings.
func (a *SystemAPI) radioWritesNow() radiowrites.View { return a.radioWritesSpan(60) }

// radioWritesSpan is the tracker's view of a span with the radio sampler's duty cycles - each
// series named by its physical radio from the radio plan, as GET /radio/health does (task 156),
// so a module shared by BidCos-RF and HmIP-RF is one row (openccu-lite task 489) - and the
// running device firmware transfers.
func (a *SystemAPI) radioWritesSpan(span int) radiowrites.View {
	var duty []radiowrites.DutySeries
	if a.Health != nil {
		st := a.Health.Status()
		type radioOf struct{ key, name string }
		radios := map[string]radioOf{}
		if p, ok := a.RadioConnections.BootPlan(); ok {
			hmip := 0
			for _, ri := range st.Interfaces {
				if ri.Interface == "HmIP-RF" {
					hmip++
				}
			}
			for _, ri := range st.Interfaces {
				k, n := p.Transmitter(ri.Interface, ri.Address, ri.Type, hmip)
				radios[ri.Interface+"/"+ri.Address] = radioOf{k, n}
			}
		}
		for key, samples := range st.History {
			iface, address, _ := strings.Cut(key, "/")
			if !radiowrites.IsRadio(iface) {
				continue
			}
			r := radios[key]
			ds := radiowrites.DutySeries{Interface: iface, Address: address, Radio: r.key, RadioName: r.name}
			for _, s := range samples {
				ds.Samples = append(ds.Samples, radiowrites.DutySample{Time: s.Time, DutyCycle: s.DutyCycle, Connected: s.Connected})
			}
			duty = append(duty, ds)
		}
	}
	v := a.RadioWrites.ViewSpan(duty, a.transferringInterfaces(), span)
	if a.TokenClient != nil {
		for i := range v.Clients {
			if v.Clients[i].Kind == radiowrites.KindToken {
				v.Clients[i].Label = a.TokenClient(v.Clients[i].Name)
			}
		}
	}
	return v
}

func (a *SystemAPI) radioWrites(w http.ResponseWriter, r *http.Request) {
	// openccu-lite task 489: ?span=60|360|1440, the hour by default
	span := 60
	if q := r.URL.Query().Get("span"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || radiowrites.SpanBucket(n) == 0 {
			writeJSON(w, http.StatusBadRequest, apiError{Error: "invalid", Message: "span is 60, 360 or 1440 (minutes)"})
			return
		}
		span = n
	}
	out := radioWritesView{View: radiowrites.View{Span: span, BucketMinutes: radiowrites.SpanBucket(span), Thresholds: radiowrites.TheThresholds(), Clients: []radiowrites.ClientView{}, DutyCycle: []radiowrites.DutyView{}}}
	if a.RadioWrites != nil {
		out.View, out.Available = a.radioWritesSpan(span), true
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
