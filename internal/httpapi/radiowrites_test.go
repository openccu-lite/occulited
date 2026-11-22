package httpapi

import (
	"fmt"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/radiowrites"
	"github.com/hobbyquaker/occulited/internal/warnings"
)

// occulited task 163: a write through lite-rpc is counted for its client - a token by its name, an
// account's session by the account - with the writes that changed nothing beside it; the central's
// own virtual devices and reads are not counted, and GET /radio/writes answers the lot.
func TestRadioWritesCountedPerClient(t *testing.T) {
	rig := newLiteRig(t)
	rig.api.TokenClient = func(name string) string {
		if name == "ha" {
			return "Home Assistant"
		}
		return ""
	}
	set := func(address, key, value string) string {
		return `<methodCall><methodName>setValue</methodName><params><param><value>` + address + `</value></param><param><value>` + key + `</value></param><param><value>` + value + `</value></param></params></methodCall>`
	}
	post := func(body string, hdr map[string]string) {
		t.Helper()
		if st, _, raw := do(t, rig.srv, "POST", "/api/rpc/v1/xmlrpc/HmIP-RF", body, hdr); st != 200 {
			t.Fatalf("call: %d %s", st, raw)
		}
	}
	// the store has heard STATE true of this channel
	rig.state.Reconcile("HmIP-RF", "00010000000A10:3", map[string]any{"STATE": true, "LEVEL": 0.5}, time.Now(), nil)

	rig.as(tokenOperate)
	post(set("00010000000A10:3", "STATE", "<boolean>1</boolean>"), nil)   // nothing changes
	post(set("00010000000A10:3", "STATE", "<boolean>0</boolean>"), nil)   // a change
	post(set("00010000000A10:3", "LEVEL", "<double>0.5</double>"), nil)   // nothing changes
	post(set("00010000000A11:1", "STATE", "<boolean>1</boolean>"), nil)   // a channel the store does not know
	post(set("HmIP-RCV-1:1", "PRESS_SHORT", "<boolean>1</boolean>"), nil) // the virtual remote: no airtime
	post(`<methodCall><methodName>getValue</methodName><params><param><value>00010000000A10:3</value></param><param><value>STATE</value></param></params></methodCall>`, nil)
	// JSON-RPC, a batch: two more writes, one of them the same value again
	if st, _, raw := do(t, rig.srv, "POST", "/api/rpc/v1/json/HmIP-RF", `[{"jsonrpc":"2.0","method":"setValue","params":["00010000000A10:3","LEVEL",0.5],"id":1},{"jsonrpc":"2.0","method":"putParamset","params":["00010000000A10:3","VALUES",{"LEVEL":0.7}],"id":2}]`, nil); st != 200 {
		t.Fatalf("batch: %d %s", st, raw)
	}
	// an account's session: the system's own pages
	rig.as(adminSess)
	post(set("00010000000A10:3", "STATE", "<boolean>0</boolean>"), map[string]string{"Authorization": "Bearer " + adminSess.ID})

	st, out, raw := do(t, rig.srv, "GET", "/api/system/v1/radio/writes", "", nil)
	if st != 200 || out["available"] != true || out["classic_rpc"] != false {
		t.Fatalf("radio/writes: %d %s", st, raw)
	}
	th := out["thresholds"].(map[string]any)
	if th["average_per_minute"] != float64(20) || th["burst_per_minute"] != float64(60) || th["duty_cycle_rise"] != float64(10) || th["duty_cycle_minutes"] != float64(15) || th["share"] != 0.5 {
		t.Errorf("thresholds: %v", th)
	}
	clients := out["clients"].([]any)
	if len(clients) != 2 {
		t.Fatalf("clients: %s", raw)
	}
	ha := clients[0].(map[string]any)
	if ha["kind"] != "token" || ha["name"] != "ha" || ha["label"] != "Home Assistant" || ha["hour"] != float64(6) || ha["noop"] != float64(3) || len(ha["minutes"].([]any)) != 60 {
		t.Errorf("the token: %v", ha)
	}
	if fmt.Sprint(ha["interfaces"]) != "[map[hour:6 name:HmIP-RF noop:3 writes:6]]" {
		t.Errorf("the token's interfaces: %v", ha["interfaces"])
	}
	if _, has := ha["level"]; has {
		t.Errorf("six writes are no hint: %v", ha)
	}
	admin := clients[1].(map[string]any)
	if admin["kind"] != "user" || admin["name"] != "admin" || admin["hour"] != float64(1) || admin["noop"] != float64(0) {
		t.Errorf("the account: %v", admin)
	}
	if len(out["duty_cycle"].([]any)) != 0 {
		t.Errorf("duty cycle without a sampler: %v", out["duty_cycle"])
	}

	// openccu-lite task 489: the span's buckets - 6 h by five minutes, 24 h by a quarter hour
	for span, want := range map[string][2]int{"": {60, 1}, "60": {60, 1}, "360": {360, 5}, "1440": {1440, 15}} {
		st, out, raw := do(t, rig.srv, "GET", "/api/system/v1/radio/writes?span="+span, "", nil)
		ha := out["clients"].([]any)[0].(map[string]any)
		if st != 200 || out["span"] != float64(want[0]) || out["bucket_minutes"] != float64(want[1]) || len(ha["buckets"].([]any)) != want[0]/want[1] || ha["writes"] != float64(6) || ha["writes_noop"] != float64(3) {
			t.Errorf("span %q: %d %s", span, st, raw)
		}
	}
	for _, bad := range []string{"30", "x", "-60"} {
		if st, _, raw := do(t, rig.srv, "GET", "/api/system/v1/radio/writes?span="+bad, "", nil); st != 400 {
			t.Errorf("span %q: %d %s", bad, st, raw)
		}
	}

	// a system without the counter answers an empty view
	rig.api.RadioWrites = nil
	if st, out, raw := do(t, rig.srv, "GET", "/api/system/v1/radio/writes", "", nil); st != 200 || out["available"] != false || len(out["clients"].([]any)) != 0 {
		t.Fatalf("without the counter: %d %s", st, raw)
	}
	if list, ok := rig.api.radioWritesWarnings(t.Context()); !ok || len(list) != 0 {
		t.Fatalf("warnings without the counter: %v %v", list, ok)
	}
}

// occulited task 163: the Status warnings - a hint per client, the warning in its place for the
// client behind a duty cycle's rise; each client its own variant.
func TestRadioWritesRule(t *testing.T) {
	v := radiowrites.View{Clients: []radiowrites.ClientView{
		{Client: radiowrites.Client{Kind: "token", Name: "nodered-1"}, Label: "Node-RED", Level: radiowrites.LevelWarning,
			Hint:  &radiowrites.Hint{Burst: true, PeakMinute: 90, PeakAverage: 30},
			Cause: &radiowrites.Cause{Interface: "HmIP-RF", Address: "X", From: 4, To: 22, Writes: 180, Total: 200, SharePercent: 90}},
		{Client: radiowrites.Client{Kind: "token", Name: "hm2mqtt"}, Level: radiowrites.LevelHint, Hint: &radiowrites.Hint{Average: true, PeakMinute: 30, PeakAverage: 24.4}},
		{Client: radiowrites.Client{Kind: "user", Name: "admin"}},
	}}
	got := radioWritesRule(v)
	if len(got) != 2 {
		t.Fatalf("warnings: %+v", got)
	}
	red, yellow := got[0], got[1]
	if red.ID != RadioWritesDutyID || red.Variant != "token:nodered-1@HmIP-RF" || red.Severity != warnings.SeverityError || red.Href != "#radio-writes" {
		t.Errorf("the warning: %+v", red)
	}
	if p := red.Params; p["client"] != "nodered-1" || p["label"] != "Node-RED" || p["interface"] != "HmIP-RF" || p["dc_from"] != 4 || p["dc_to"] != 22 || p["share_percent"] != 90 || p["writes"] != 180 || p["minutes"] != 15 {
		t.Errorf("the warning's params: %v", p)
	}
	if yellow.ID != RadioWritesID || yellow.Variant != "token:hm2mqtt" || yellow.Severity != warnings.SeverityWarning {
		t.Errorf("the hint: %+v", yellow)
	}
	if p := yellow.Params; p["average"] != true || p["burst"] != false || p["peak_average"] != 24.4 || p["peak_minute"] != 30 || p["average_minutes"] != 5 {
		t.Errorf("the hint's params: %v", p)
	}
	if _, has := yellow.Params["label"]; has {
		t.Errorf("a label without one: %v", yellow.Params)
	}
	if got := radioWritesRule(radiowrites.View{}); len(got) != 0 {
		t.Errorf("no clients: %v", got)
	}
}
