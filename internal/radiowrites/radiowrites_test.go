package radiowrites

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/rpcsub"
)

var t0 = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// clock is a tracker with a clock the test moves.
type clock struct {
	*Tracker
	at time.Time
}

func newClock() *clock {
	c := &clock{at: t0}
	c.Tracker = &Tracker{Now: func() time.Time { return c.at }}
	return c
}

// send records n writes of a client at minute m after t0 (plus 10 s).
func (c *clock) send(client, iface string, m, n int, noop bool) {
	c.at = t0.Add(time.Duration(m)*time.Minute + 10*time.Second)
	for i := 0; i < n; i++ {
		c.Record(Client{Kind: KindToken, Name: client}, iface, noop)
	}
}

func (c *clock) viewAt(m int, duty []DutySeries, transfer map[string]bool) View {
	c.at = t0.Add(time.Duration(m)*time.Minute + 30*time.Second)
	return c.View(duty, transfer)
}

func client(v View, name string) *ClientView {
	for i := range v.Clients {
		if v.Clients[i].Name == name {
			return &v.Clients[i]
		}
	}
	return nil
}

func TestCounted(t *testing.T) {
	tr := &Tracker{}
	tr.SetExcluded("HmIP-RF", map[string]bool{"0025000000WIRED": true})
	for _, c := range []struct {
		iface, address string
		want           bool
	}{
		{"BidCos-RF", "JEQ0000001:1", true},
		{"HmIP-RF", "00010000000A10:3", true},
		{"HmIP-RF", "00010000000A10", true},
		{"BidCos-Wired", "JEQ0000001:1", false},   // wired: no airtime
		{"VirtualDevices", "INT0000001:1", false}, // virtual
		{"CUxD", "CUX0100001:1", false},
		{"BidCos-RF", "BidCoS-RF:3", false},     // the central's virtual keys
		{"BidCos-RF", "BidCoS-RF", false},       // the central itself
		{"HmIP-RF", "HmIP-RCV-1:2", false},      // HmIP's virtual remote
		{"HmIP-RF", "0025000000WIRED:4", false}, // listed as HmIPW-*
		{"HmIP-RF", "", false},
	} {
		if got := tr.Counted(c.iface, c.address); got != c.want {
			t.Errorf("Counted(%s, %s) = %v, want %v", c.iface, c.address, got, c.want)
		}
	}
}

func TestNonRadioType(t *testing.T) {
	for typ, want := range map[string]bool{"HmIPW-DRS8": true, "HmIPW-DRAP": true, "HM-RCV-50": true, "HmIP-RCV-50": true,
		"HmIP-HAP": true, "HmIP-HAP-B1": true, "HmIP-RFUSB": true, "RPI-RF-MOD": true, "HmIP-CCU3": true,
		"HmIP-BROLL": false, "HM-LC-Sw1-Pl": false, "HmIP-DRSI4": false, "": false} {
		if got := NonRadioType(typ); got != want {
			t.Errorf("NonRadioType(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestCountsPerClientAndInterface(t *testing.T) {
	c := newClock()
	c.send("nodered-1", "HmIP-RF", 0, 3, false)
	c.send("nodered-1", "HmIP-RF", 0, 2, true)
	c.send("nodered-1", "BidCos-RF", 1, 4, true)
	c.send("hm2mqtt", "BidCos-RF", 2, 1, false)
	v := c.viewAt(2, nil, nil)
	if len(v.Clients) != 2 {
		t.Fatalf("clients = %d, want 2", len(v.Clients))
	}
	n := client(v, "nodered-1")
	if n.Hour != 9 || n.Noop != 6 {
		t.Errorf("nodered-1: hour %d noop %d, want 9 and 6", n.Hour, n.Noop)
	}
	if len(n.Minutes) != Minutes || n.Minutes[Minutes-3] != 5 || n.Minutes[Minutes-2] != 4 || n.Minutes[Minutes-1] != 0 {
		t.Errorf("nodered-1: minutes end %v, want 5 4 0", n.Minutes[Minutes-3:])
	}
	if fmt.Sprint(n.Interfaces) != "[{BidCos-RF 4 4 4} {HmIP-RF 5 2 5}]" {
		t.Errorf("nodered-1: interfaces %v", n.Interfaces)
	}
	if n.Level != "" || n.Hint != nil || n.Cause != nil {
		t.Errorf("nodered-1: a level without a reason: %+v", n)
	}
	// the busier client first
	if v.Clients[0].Name != "nodered-1" {
		t.Errorf("order: %s first", v.Clients[0].Name)
	}
	// per_minute: the running minute plus the rest of the sliding minute (30 s in: half of the
	// minute before)
	if h := client(v, "hm2mqtt"); h.PerMinute != 1 {
		t.Errorf("hm2mqtt: per_minute %d, want 1", h.PerMinute)
	}
	if n.PerMinute != 2 {
		t.Errorf("nodered-1: per_minute %d, want 2 (half of the 4 a minute ago)", n.PerMinute)
	}
}

func TestRingForgetsAfterAnHour(t *testing.T) {
	c := newClock()
	c.send("a", "HmIP-RF", 0, 7, false)
	if v := c.viewAt(59, nil, nil); client(v, "a") == nil || client(v, "a").Hour != 7 || client(v, "a").Minutes[0] != 7 {
		t.Fatalf("after 59 min: %+v", client(v, "a"))
	}
	if v := c.viewAt(60, nil, nil); client(v, "a") != nil {
		t.Fatalf("after 60 min the client is still listed: %+v", client(v, "a"))
	}
	// a write after a long pause starts a clean ring
	c.send("b", "HmIP-RF", 0, 2, false)
	c.send("b", "HmIP-RF", 200, 1, false)
	if v := c.viewAt(200, nil, nil); client(v, "b").Hour != 1 {
		t.Fatalf("after a pause: hour %d, want 1", client(v, "b").Hour)
	}
	// a slot is reused after exactly an hour
	c.send("b", "HmIP-RF", 260, 3, false)
	if v := c.viewAt(260, nil, nil); client(v, "b").Hour != 3 {
		t.Fatalf("slot reuse: hour %d, want 3", client(v, "b").Hour)
	}
}

func TestHint(t *testing.T) {
	for _, tc := range []struct {
		name           string
		per            []int // writes in minutes 0, 1, 2, ...
		at             int
		burst, average bool
		none           bool
	}{
		{name: "quiet", per: []int{20, 20, 20, 20, 20}, at: 4, none: true}, // exactly 20/min is not above
		{name: "average", per: []int{21, 21, 21, 21, 21}, at: 4, average: true},
		{name: "average needs five minutes", per: []int{0, 30, 30, 30, 0, 0}, at: 5, none: true}, // 90 in 5 min = 18/min
		{name: "burst", per: []int{61}, at: 0, burst: true},
		{name: "sixty in a minute is not above", per: []int{60}, at: 0, none: true},
		{name: "both", per: []int{100, 100}, at: 1, burst: true, average: true},
		{name: "held for 15 minutes", per: []int{61}, at: 14, burst: true},
		{name: "gone after the hold", per: []int{61}, at: 15, none: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClock()
			for m, n := range tc.per {
				c.send("x", "BidCos-RF", m, n, false)
			}
			x := client(c.viewAt(tc.at, nil, nil), "x")
			if tc.none {
				if x.Level != "" || x.Hint != nil {
					t.Fatalf("a hint without a reason: %+v", x.Hint)
				}
				return
			}
			if x.Level != LevelHint || x.Hint == nil || x.Hint.Burst != tc.burst || x.Hint.Average != tc.average {
				t.Fatalf("level %q hint %+v, want burst %v average %v", x.Level, x.Hint, tc.burst, tc.average)
			}
		})
	}
	// the figures of the hint
	c := newClock()
	for m := 0; m < 5; m++ {
		c.send("x", "HmIP-RF", m, 30+m, false)
	}
	h := client(c.viewAt(4, nil, nil), "x").Hint
	if h.PeakMinute != 34 || h.PeakAverage != 32 {
		t.Errorf("peak minute %d average %v, want 34 and 32", h.PeakMinute, h.PeakAverage)
	}
}

// duty builds a series: the duty cycle at the given minutes after t0.
func duty(iface string, at map[int]int) []DutySeries {
	ds := DutySeries{Interface: iface, Address: "MOD0000001"}
	for m := -60; m < 120; m++ {
		if dc, ok := at[m]; ok {
			ds.Samples = append(ds.Samples, DutySample{Time: t0.Add(time.Duration(m) * time.Minute), DutyCycle: dc, Connected: true})
		}
	}
	return []DutySeries{ds}
}

func TestWarningNamesTheClientBehindTheRise(t *testing.T) {
	type send struct {
		client, iface string
		minute, n     int
	}
	for _, tc := range []struct {
		name     string
		sends    []send
		duty     []DutySeries
		transfer map[string]bool
		at       int
		want     string // the client warned about; "" = none
		share    int
	}{
		{name: "rise of 10 with one client", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 15}), at: 6, want: "nodered", share: 100},
		{name: "rise of 9 is none", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 14}), at: 6},
		{name: "half of the writes is enough", sends: []send{{"nodered", "HmIP-RF", 2, 40}, {"other", "HmIP-RF", 3, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20}), at: 6, want: "nodered", share: 50},
		{name: "less than half names nobody", sends: []send{{"a", "HmIP-RF", 2, 40}, {"b", "HmIP-RF", 3, 35}, {"c", "HmIP-RF", 3, 35}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20}), at: 6},
		{name: "the other interface's writes do not count", sends: []send{{"nodered", "BidCos-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20}), at: 6},
		{name: "a handful of writes is no cause", sends: []send{{"nodered", "HmIP-RF", 2, CauseMinWrites - 1}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20}), at: 6},
		{name: "a rise more than 15 minutes ago", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20, 10: 20, 21: 20}), at: 21},
		{name: "a rise spread over more than 15 minutes", sends: []send{{"nodered", "HmIP-RF", 18, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 20: 15}), at: 20},
		{name: "a fall is no rise", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 30, 5: 10}), at: 6},
		{name: "not during a device firmware transfer", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("HmIP-RF", map[int]int{0: 5, 5: 20}), transfer: map[string]bool{"HmIP-RF": true}, at: 6},
		{name: "a wired interface's duty cycle is ignored", sends: []send{{"nodered", "HmIP-RF", 2, 40}}, duty: duty("BidCos-Wired", map[int]int{0: 5, 5: 20}), at: 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClock()
			for _, s := range tc.sends {
				c.send(s.client, s.iface, s.minute, s.n, false)
			}
			v := c.viewAt(tc.at, tc.duty, tc.transfer)
			got := ""
			for _, cl := range v.Clients {
				if cl.Level == LevelWarning {
					got = cl.Name
					if cl.Cause == nil || cl.Cause.SharePercent != tc.share || cl.Cause.Interface != "HmIP-RF" {
						t.Errorf("cause %+v, want share %d on HmIP-RF", cl.Cause, tc.share)
					}
				}
			}
			if got != tc.want {
				t.Fatalf("warned about %q, want %q", got, tc.want)
			}
			if tc.want != "" && v.Clients[0].Name != tc.want {
				t.Errorf("the warned client is not first: %s", v.Clients[0].Name)
			}
		})
	}
}

func TestRiseSkipsReadingsWhileDown(t *testing.T) {
	// the interface restarted: 0 while down, then back at its level - no rise
	ds := DutySeries{Interface: "BidCos-RF", Address: "X", Samples: []DutySample{
		{Time: t0, DutyCycle: 20, Connected: true},
		{Time: t0.Add(2 * time.Minute), DutyCycle: 0, Connected: false},
		{Time: t0.Add(4 * time.Minute), DutyCycle: 21, Connected: true},
	}}
	c := newClock()
	c.send("x", "BidCos-RF", 1, 50, false)
	v := c.viewAt(5, []DutySeries{ds}, nil)
	if x := client(v, "x"); x.Level == LevelWarning {
		t.Fatalf("a restart of the interface read as a rise: %+v", x.Cause)
	}
	// a series without a reading in the hour is no row: a gateway that never answered, an old one
	none := []DutySeries{{Interface: "BidCos-RF", Address: "GONE", Samples: []DutySample{{Time: t0, DutyCycle: 0, Connected: false}, {Time: t0.Add(-2 * time.Hour), DutyCycle: 7, Connected: true}}}}
	if got := c.viewAt(5, none, nil).DutyCycle; len(got) != 0 {
		t.Errorf("a series without readings is listed: %+v", got)
	}
	// and the series on the ring's axis leaves the minute out
	m := v.DutyCycle[0].Minutes
	if len(m) != Minutes || m[Minutes-6] == nil || *m[Minutes-6] != 20 || m[Minutes-4] != nil || m[Minutes-2] == nil || *m[Minutes-2] != 21 {
		t.Errorf("duty minutes end: %v %v %v", m[Minutes-6], m[Minutes-4], m[Minutes-2])
	}
}

func TestWarningReplacesTheHint(t *testing.T) {
	c := newClock()
	c.send("nodered", "BidCos-RF", 2, 80, false)
	x := client(c.viewAt(6, duty("BidCos-RF", map[int]int{0: 3, 5: 30}), nil), "nodered")
	if x.Level != LevelWarning || x.Cause == nil || x.Cause.From != 3 || x.Cause.To != 30 || x.Cause.Writes != 80 || x.Hint == nil {
		t.Fatalf("level %q cause %+v hint %+v", x.Level, x.Cause, x.Hint)
	}
}

func TestNoop(t *testing.T) {
	last := func(address, dp string) (any, bool) {
		v, ok := map[string]any{
			"A:1/STATE": true, "A:1/LEVEL": 0.5, "A:1/SET_POINT_TEMPERATURE": 21.0, "A:1/CONTROL_MODE": 1, "A:1/PRESS_SHORT": true, "A:1/TEXT": "x",
		}[address+"/"+dp]
		return v, ok
	}
	set := func(kv ...any) []any { return kv }
	for _, tc := range []struct {
		name, method string
		args         []any
		want         bool
	}{
		{"the same bool", "setValue", set("STATE", true), true},
		{"another bool", "setValue", set("STATE", false), false},
		{"the same float", "setValue", set("LEVEL", 0.5), true},
		{"another float", "setValue", set("LEVEL", 0.6), false},
		{"an integer for a float", "setValue", set("SET_POINT_TEMPERATURE", int64(21)), true},
		{"a float for an integer", "setValue", set("CONTROL_MODE", 1.0), true},
		{"a number is not a bool", "setValue", set("STATE", int64(1)), false},
		{"the same string", "setValue", set("TEXT", "x"), true},
		{"a datapoint the store does not know", "setValue", set("OTHER", true), false},
		{"a key press is always new", "setValue", set("PRESS_SHORT", true), false},
		{"no value", "setValue", set("STATE"), false},
		{"values, all the same", "putParamset", set("VALUES", map[string]any{"STATE": true, "LEVEL": 0.5}), true},
		{"values, one differs", "putParamset", set("VALUES", map[string]any{"STATE": true, "LEVEL": 1.0}), false},
		{"values, one unknown", "putParamset", set("VALUES", map[string]any{"STATE": true, "X": 1.0}), false},
		{"values, empty", "putParamset", set("VALUES", map[string]any{}), false},
		{"the configuration is never judged", "putParamset", set("MASTER", map[string]any{"STATE": true}), false},
		{"a link's paramset", "putParamset", set("B:2", map[string]any{"STATE": true}), false},
		{"another method", "getValue", set("STATE", true), false},
	} {
		if got := Noop(tc.method, "A:1", tc.args, last); got != tc.want {
			t.Errorf("%s: Noop = %v, want %v", tc.name, got, tc.want)
		}
	}
	if Noop("setValue", "A:1", set("STATE", true), nil) {
		t.Error("without a store nothing is known to be the same")
	}
}

func TestClientLimit(t *testing.T) {
	c := newClock()
	for i := 0; i < maxClients+10; i++ {
		c.send(fmt.Sprintf("c%03d", i), "HmIP-RF", 0, 1, false)
	}
	if v := c.viewAt(0, nil, nil); len(v.Clients) != maxClients {
		t.Fatalf("clients = %d, want the limit %d", len(v.Clients), maxClients)
	}
	// still full after the hour: the 24 hours keep them (openccu-lite task 489)
	c.send("early", "HmIP-RF", 90, 1, false)
	if v := c.viewAt(90, nil, nil); len(v.Clients) != 0 {
		t.Fatalf("after the hour: %d clients", len(v.Clients))
	}
	// once the old ones have gone quiet for 24 hours a new client is counted again
	c.send("late", "HmIP-RF", 24*60+10, 1, false)
	if v := c.viewAt(24*60+10, nil, nil); len(v.Clients) != 1 || v.Clients[0].Name != "late" {
		t.Fatalf("after 24 hours: %d clients", len(v.Clients))
	}
}

func TestDeviceWatch(t *testing.T) {
	tr := &Tracker{}
	calls := make(chan string, 8)
	fail := false
	w := &DeviceWatch{Tracker: tr, Delay: time.Millisecond, Types: func(_ context.Context, iface string) (map[string]string, error) {
		defer func() { calls <- iface }()
		if fail {
			return nil, errors.New("down")
		}
		return map[string]string{"0001A": "HmIP-BROLL", "0002W": "HmIPW-DRS8", "HmIP-RCV-1": "HmIP-RCV-50"}, nil
	}}
	read := func() {
		t.Helper()
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatal("the device list was not read")
		}
	}
	if !tr.Counted("HmIP-RF", "0002W:1") {
		t.Fatal("before the first list every device counts")
	}
	w.Interface("HmIP-RF", "up", time.Now())
	read()
	for end := time.Now().Add(5 * time.Second); tr.Counted("HmIP-RF", "0002W:1"); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the wired device still counts")
		}
	}
	if !tr.Counted("HmIP-RF", "0001A:1") {
		t.Fatal("the radio device does not count")
	}
	// a list that cannot be read keeps what is known
	fail = true
	w.Devices("HmIP-RF", "new", nil)
	read()
	time.Sleep(20 * time.Millisecond)
	if tr.Counted("HmIP-RF", "0002W:1") {
		t.Fatal("a failed read forgot the wired device")
	}
	// other interfaces and other states are not read, nor is anything at an event
	w.Interface("BidCos-Wired", "up", time.Now())
	w.Interface("HmIP-RF", "down", time.Now())
	w.Event(rpcsub.Event{})
	select {
	case i := <-calls:
		t.Fatalf("read %s without a reason", i)
	case <-time.After(30 * time.Millisecond):
	}
}
