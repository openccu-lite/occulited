package radiowrites

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/store"
)

func (c *clock) spanAt(m, span int, duty []DutySeries) View {
	c.at = t0.Add(time.Duration(m)*time.Minute + 30*time.Second)
	return c.ViewSpan(duty, nil, span)
}

// openccu-lite task 489: 1 h by the minute, 6 h by five minutes, 24 h by a quarter hour; the
// span's sums beside the hour's.
func TestSpans(t *testing.T) {
	c := newClock() // t0 is 12:00, a quarter hour's start
	c.send("a", "HmIP-RF", 0, 2, true)
	c.send("a", "HmIP-RF", 4, 3, false)
	c.send("a", "BidCos-RF", 5, 1, false)
	c.send("a", "HmIP-RF", 100, 4, false)
	c.send("b", "HmIP-RF", 10, 1, false)

	v := c.spanAt(100, 60, nil)
	a := client(v, "a")
	if v.Span != 60 || v.BucketMinutes != 1 || len(a.Buckets) != 60 || a.Writes != 4 || a.Hour != 4 || client(v, "b") != nil {
		t.Fatalf("1 h: span %d/%d, a %+v, b %+v", v.Span, v.BucketMinutes, a, client(v, "b"))
	}
	if fmt.Sprint(a.Buckets[56:]) != fmt.Sprint(a.Minutes[56:]) {
		t.Errorf("1 h: buckets %v are not the minutes %v", a.Buckets[56:], a.Minutes[56:])
	}
	if fmt.Sprint(a.Interfaces) != "[{HmIP-RF 4 0 4}]" {
		t.Errorf("1 h: interfaces %v (BidCos-RF had nothing in the hour)", a.Interfaces)
	}

	v = c.spanAt(100, 360, nil)
	a = client(v, "a")
	if v.BucketMinutes != 5 || len(a.Buckets) != 72 {
		t.Fatalf("6 h: %d buckets of %d", len(a.Buckets), v.BucketMinutes)
	}
	// minute 100 is bucket 20 after t0, the last; 0 and 4 are bucket 0 = index 71-20 = 51; 5 is 52
	if a.Buckets[71] != 4 || a.Buckets[51] != 5 || a.Buckets[52] != 1 || a.Writes != 10 || a.WritesNoop != 2 || a.Hour != 4 {
		t.Errorf("6 h: %v, writes %d noop %d hour %d", a.Buckets[48:], a.Writes, a.WritesNoop, a.Hour)
	}
	if fmt.Sprint(a.Interfaces) != "[{BidCos-RF 0 0 1} {HmIP-RF 4 0 9}]" {
		t.Errorf("6 h: interfaces %v", a.Interfaces)
	}
	if b := client(v, "b"); b == nil || b.Writes != 1 || b.Hour != 0 {
		t.Errorf("6 h: b %+v (no write in the hour, one in the span)", b)
	}
	// the busier client of the span first
	if v.Clients[0].Name != "a" {
		t.Errorf("6 h: %s first", v.Clients[0].Name)
	}

	v = c.spanAt(100, 1440, nil)
	a = client(v, "a")
	if v.BucketMinutes != 15 || len(a.Buckets) != 96 {
		t.Fatalf("24 h: %d buckets of %d", len(a.Buckets), v.BucketMinutes)
	}
	// minute 100 is quarter 6, the last; 0, 4, 5 are quarter 0 = index 89
	if a.Buckets[95] != 4 || a.Buckets[89] != 6 || a.Writes != 10 {
		t.Errorf("24 h: %v, writes %d", a.Buckets[88:], a.Writes)
	}
	// after 24 hours the first quarter has left the ring
	v = c.spanAt(24*60+3, 1440, nil)
	if a = client(v, "a"); a == nil || a.Writes != 4 {
		t.Errorf("24 h later: %+v", a)
	}
	if v = c.spanAt(24*60+101, 1440, nil); len(v.Clients) != 0 {
		t.Errorf("a day after the last write: %+v", v.Clients)
	}
	// a span not offered is the hour
	if v = c.spanAt(100, 42, nil); v.Span != 60 || v.BucketMinutes != 1 {
		t.Errorf("span 42: %d/%d", v.Span, v.BucketMinutes)
	}
}

// openccu-lite task 489: one duty-cycle row per physical radio - BidCos-RF and HmIP-RF on one
// module are one row with the higher reading, two radios two rows.
func TestDutyRowPerRadio(t *testing.T) {
	c := newClock()
	at := func(m, dc int) DutySample {
		return DutySample{Time: t0.Add(time.Duration(m)*time.Minute + 5*time.Second), DutyCycle: dc, Connected: true}
	}
	shared := []DutySeries{
		{Interface: "HmIP-RF", Address: "3014F711A0000000000000AA", Radio: "rpi-rf-mod", RadioName: "RPI-RF-MOD", Samples: []DutySample{at(10, 4), at(11, 9), at(30, 2)}},
		{Interface: "BidCos-RF", Address: "OEQ0000001", Radio: "rpi-rf-mod", RadioName: "RPI-RF-MOD", Samples: []DutySample{at(10, 6), at(11, 3)}},
		{Interface: "BidCos-RF", Address: "OEQ0000002", Samples: []DutySample{at(12, 1)}},
		{Interface: "BidCos-RF", Address: "OEQ0000003", Samples: []DutySample{at(-340, 1)}}, // a gateway without a reading in 6 h
	}
	v := c.spanAt(30, 360, shared)
	if len(v.DutyCycle) != 2 {
		t.Fatalf("rows %+v", v.DutyCycle)
	}
	r := v.DutyCycle[0]
	if r.Radio != "rpi-rf-mod" || r.Name != "RPI-RF-MOD" || fmt.Sprint(r.Interfaces) != "[BidCos-RF HmIP-RF]" || r.Interface != "BidCos-RF" {
		t.Errorf("shared row %+v", r)
	}
	// minute 10: max(4, 6); minute 11: max(9, 3)
	if *r.Minutes[59-20] != 6 || *r.Minutes[59-19] != 9 || r.Minutes[0] != nil {
		t.Errorf("shared minutes %v %v", *r.Minutes[39], *r.Minutes[40])
	}
	// six hours by five minutes: minutes 10-14 are one bucket (9), 30 the last (2)
	if len(r.Buckets) != 72 || *r.Buckets[71] != 2 || *r.Buckets[71-4] != 9 || r.Buckets[70] != nil {
		t.Errorf("shared buckets %v", r.Buckets[64:])
	}
	if o := v.DutyCycle[1]; o.Address != "OEQ0000002" || o.Radio != "" || fmt.Sprint(o.Interfaces) != "[BidCos-RF]" {
		t.Errorf("own radio %+v", o)
	}
	if v = c.spanAt(30, 1440, shared); len(v.DutyCycle) != 3 {
		t.Errorf("24 h rows %d", len(v.DutyCycle))
	}
	if v = c.spanAt(100, 60, shared); len(v.DutyCycle) != 0 {
		t.Errorf("1 h rows %d, nothing in the hour", len(v.DutyCycle))
	}
}

func row(b int64, w, noop uint32) store.Row {
	v := make([]byte, 9)
	v[0] = bucketRowV1
	binary.BigEndian.PutUint32(v[1:], w)
	binary.BigEndian.PutUint32(v[5:], noop)
	return store.Row{At: bucketTime(b), Value: v}
}

// openccu-lite task 489: the five-minute buckets go to the database and come back after a restart;
// a client gone quiet for a day leaves the file.
func TestPersistence(t *testing.T) {
	c := newClock()
	if got, _ := c.Pending(false); got != nil {
		t.Fatalf("pending before a write: %+v", got)
	}
	c.send("nodered", "HmIP-RF", 0, 3, true)
	c.send("nodered", "HmIP-RF", 7, 2, false)
	got, done := c.Pending(false)
	if len(got) != 1 || got[0].Bucket != "radiowrites" || got[0].Name != "HmIP-RF|token:nodered" || len(got[0].Rows) != 2 || got[0].Replace {
		t.Fatalf("pending %+v", got)
	}
	file := append([]store.Row(nil), got[0].Rows...)
	done()
	if again, _ := c.Pending(false); again != nil {
		t.Fatalf("written rows pending again: %+v", again)
	}
	// the running bucket grows: it is sent again, the one before is not
	c.send("nodered", "HmIP-RF", 8, 1, false)
	got, done = c.Pending(false)
	if len(got) != 1 || len(got[0].Rows) != 1 || !got[0].Rows[0].At.Equal(t0.Add(5*time.Minute)) || binary.BigEndian.Uint32(got[0].Rows[0].Value[1:]) != 3 {
		t.Fatalf("pending after one more %+v", got)
	}
	file[1] = got[0].Rows[0]
	done()
	// after an open: every series whole, replacing the file's
	if all, _ := c.Pending(true); len(all) != 1 || len(all[0].Rows) != 2 || !all[0].Replace {
		t.Fatalf("all: %+v", all)
	}

	// a restart: the buckets of the last 24 hours come back, the rest is dropped
	n := newClock()
	n.at = t0.Add(20 * time.Minute)
	now := minuteOf(n.at) / BucketMinutes
	n.Restore(map[string][]store.Row{
		"HmIP-RF|token:nodered":  append([]store.Row{row(now-Buckets, 99, 0), {At: t0, Value: []byte{7}}}, append(file, row(now+1, 50, 0))...),
		"BidCos-RF|user:admin":   {row(now-Buckets-3, 1, 0)},
		"garbage":                {row(now, 1, 0)},
		"VirtualDevices|system:": {row(now, 1, 0)},
	})
	v := n.spanAt(20, 360, nil)
	a := client(v, "nodered")
	if len(v.Clients) != 1 || a.Writes != 6 || a.WritesNoop != 3 || a.Hour != 0 {
		t.Fatalf("restored %+v", v.Clients)
	}
	// the restored rows are not pending, and the series without a row in the day leave the file
	got, done = n.Pending(false)
	names := map[string]bool{}
	for _, s := range got {
		if len(s.Rows) != 0 || !s.Replace {
			t.Errorf("pending after a restore: %+v", s)
		}
		names[s.Name] = true
	}
	if len(names) != 3 || !names["BidCos-RF|user:admin"] || !names["garbage"] || !names["VirtualDevices|system:"] {
		t.Errorf("dropped %v", names)
	}
	done()
	if again, _ := n.Pending(false); again != nil {
		t.Errorf("dropped again: %+v", again)
	}
	// a day of silence: the client leaves memory and the file
	n.spanAt(24*60+30, 60, nil)
	got, _ = n.Pending(false)
	if len(got) != 1 || got[0].Name != "HmIP-RF|token:nodered" || len(got[0].Rows) != 0 || !got[0].Replace {
		t.Errorf("after a day: %+v", got)
	}
	// it writes again before the drop went out: kept, not dropped
	n.send("nodered", "HmIP-RF", 24*60+31, 1, false)
	got, _ = n.Pending(false)
	if len(got) != 1 || len(got[0].Rows) != 1 || got[0].Replace {
		t.Errorf("back before the write: %+v", got)
	}
}

func TestParseSeries(t *testing.T) {
	for name, want := range map[string]string{
		"HmIP-RF|token:a:b|c": "token/a:b|c/HmIP-RF/true",
		"HmIP-RF|token":       "///false",
		"token:a":             "///false",
		"|token:a":            "///false",
		"HmIP-RF|:a":          "///false",
	} {
		c, iface, ok := parseSeries(name)
		if got := fmt.Sprintf("%s/%s/%s/%v", c.Kind, c.Name, iface, ok); got != want {
			t.Errorf("%q: %s, want %s", name, got, want)
		}
	}
	if c, iface, _ := parseSeries(seriesName(Client{Kind: KindUser, Name: "x|y:z"}, "BidCos-RF")); c.Name != "x|y:z" || iface != "BidCos-RF" {
		t.Errorf("round trip: %v %s", c, iface)
	}
}
