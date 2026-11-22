package radiowrites

// The longer ring in occulited's database (openccu-lite task 489): the 6 h and 24 h views would be
// empty after every restart of occulited - a deploy, an update - if the five-minute buckets lived
// in memory only. The tracker is the store's Keeper for the bucket "radiowrites", as the system
// history is for "system": one series per client and interface, one row per five minutes that
// saw a write. The minute ring stays in memory: after a restart the levels start from zero.
//
// Nothing here asks for a write of its own: the rows go with the other histories' writes (the
// samplers' minute in persistent mode, the interval in ram-sync), so counting adds no write to the
// flash.

import (
	"encoding/binary"
	"strings"
	"time"

	"github.com/hobbyquaker/occulited/internal/store"
)

// bucketRowV1 is a row's version byte: then the writes and those that changed nothing, each a
// big-endian uint32.
const bucketRowV1 = 1

// seriesName is a client's series on an interface: "HmIP-RF|token:nodered-1". An interface name
// holds no "|" and a kind no ":", so it parses back whatever the client's name holds.
func seriesName(c Client, iface string) string { return iface + "|" + c.Key() }

func parseSeries(name string) (Client, string, bool) {
	iface, key, ok := strings.Cut(name, "|")
	if !ok {
		return Client{}, "", false
	}
	kind, cname, ok := strings.Cut(key, ":")
	if !ok || iface == "" || kind == "" {
		return Client{}, "", false
	}
	return Client{Kind: kind, Name: cname}, iface, true
}

func bucketTime(b int64) time.Time { return time.Unix(b*BucketMinutes*60, 0).UTC() }

// Bucket is the tracker's bucket in occulited's database.
func (t *Tracker) Bucket() string { return "radiowrites" }

// Restore adds the file's buckets of the last 24 hours to the long ring. Memory is empty at a
// start; after a switch out of ram it holds the time since, and the file the time before.
// A series with nothing left in the 24 hours is dropped from the file with the next write.
func (t *Tracker) Restore(all map[string][]store.Row) {
	min := minuteOf(t.now())
	now := min / BucketMinutes
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, rows := range all {
		c, iface, ok := parseSeries(name)
		if !ok || !IsRadio(iface) {
			t.goneLocked(name)
			continue
		}
		var k *counts
		for _, r := range rows {
			if len(r.Value) < 9 || r.Value[0] != bucketRowV1 {
				continue
			}
			b := r.At.Unix() / (BucketMinutes * 60)
			if b > now || b <= now-Buckets {
				continue // older than the ring, or after now (a clock that was wrong)
			}
			if k == nil {
				if k = t.countsLocked(c, iface, min); k == nil {
					break
				}
			}
			k.long.add(b, binary.BigEndian.Uint32(r.Value[1:]), binary.BigEndian.Uint32(r.Value[5:]))
			k.flushed = max(k.flushed, b)
		}
		if k == nil {
			t.goneLocked(name)
		}
	}
}

// Pending returns the buckets the file lacks - the ones since the last write, the newest of
// those again as it was still counting - or, after an open, every series whole; and the series
// of clients no longer kept, emptied.
func (t *Tracker) Pending(all bool) ([]store.Series, func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	type mark struct {
		k    *counts
		gen  uint64
		last int64
	}
	var out []store.Series
	var marks []mark
	for c, byIface := range t.rings {
		for iface, k := range byIface {
			if !all && k.gen == k.synced {
				continue
			}
			from := k.long.last - Buckets + 1
			if !all && k.flushed > from {
				from = k.flushed
			}
			var rows []store.Row
			for b := from; b <= k.long.last; b++ {
				s := k.long.at(b)
				if s.w == 0 {
					continue
				}
				v := make([]byte, 9)
				v[0] = bucketRowV1
				binary.BigEndian.PutUint32(v[1:], s.w)
				binary.BigEndian.PutUint32(v[5:], s.noop)
				rows = append(rows, store.Row{At: bucketTime(b), Value: v})
			}
			out = append(out, store.Series{Bucket: t.Bucket(), Name: seriesName(c, iface), Rows: rows, Replace: all})
			marks = append(marks, mark{k, k.gen, k.long.last})
		}
	}
	gone := make([]string, 0, len(t.gone))
	for name := range t.gone {
		out = append(out, store.Series{Bucket: t.Bucket(), Name: name, Replace: true})
		gone = append(gone, name)
	}
	if len(out) == 0 {
		return nil, func() {}
	}
	return out, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		for _, m := range marks {
			m.k.synced = m.gen
			m.k.flushed = max(m.k.flushed, m.last)
		}
		for _, name := range gone {
			delete(t.gone, name)
		}
	}
}
