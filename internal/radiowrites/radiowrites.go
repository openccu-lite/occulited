// Package radiowrites counts the radio writes each lite-rpc client causes (occulited task 163):
// every setValue and putParamset that reaches a device on BidCos-RF or HmIP-RF, per client and
// interface, in a ring of 60 one-minute counts kept in memory - nothing is written to a disk. A
// program that sends too many of them, often in bursts, drives the radio's duty cycle up until
// the radio module stops sending; the counts say which program it is.
//
// Beside each count stands how many of the writes changed nothing: the value written was the
// one the datapoint already had, as the state store (internal/devstate) last heard it.
//
// Two levels come out of the counts (the maintainer, 2026-11-21; fixed for now):
//
//   - a hint, when a client sent more than AvgPerMinute writes a minute averaged over AvgMinutes,
//     or more than BurstPerMinute within one minute. It stays for HoldMinutes after the minute
//     that raised it, so a burst is still on the Status page when somebody looks.
//   - a warning, when an interface's duty cycle rose by RisePoints or more within RiseMinutes and
//     one client sent at least half of that interface's radio writes in that time; the warning
//     names it.
//
// Nothing here delays or refuses a write: the package counts and says, it never throttles.
//
// Wired and virtual devices cost no airtime and are not counted: other interfaces are left out by
// their name, and on the two radio interfaces the central's own virtual keys and the wired HmIP
// devices by their type (Excluded, fed from the interfaces' device lists).
package radiowrites

import (
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// The thresholds (occulited task 163).
const (
	// Minutes is the minute ring's length: the levels and the 1 h view.
	Minutes = 60
	// BucketMinutes and Buckets are the longer ring (openccu-lite task 489): 24 hours in buckets of
	// five minutes, for the 6 h and 24 h views, kept in occulited's database (history.go).
	BucketMinutes = 5
	Buckets       = 24 * 60 / BucketMinutes
	// AvgPerMinute over AvgMinutes, or BurstPerMinute in one minute, raise the hint.
	AvgPerMinute   = 20
	AvgMinutes     = 5
	BurstPerMinute = 60
	// HoldMinutes is how long the hint stays after the minute that raised it.
	HoldMinutes = 15
	// RisePoints of duty cycle within RiseMinutes raise the warning for the client that sent
	// CauseShare of the interface's radio writes in that time.
	RisePoints  = 10
	RiseMinutes = 15
	CauseShare  = 0.5
	// CauseMinWrites is the least a client must have sent in that time to be named: a duty cycle
	// that rose while one client sent a handful of writes rose for another reason (a device
	// firmware transfer, the devices' own traffic through the central).
	CauseMinWrites = 30
	// maxClients bounds the map; a client beyond it is not counted until one has gone quiet.
	maxClients = 256
)

// The kinds of client.
const (
	KindToken  = "token"  // a program with an API token: the token's name
	KindUser   = "user"   // an account's session: the pages of the system itself
	KindSystem = "system" // the system's own calls
)

// The levels of a client.
const (
	LevelHint    = "hint"
	LevelWarning = "warning"
)

// Client is who sent a write.
type Client struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// Key is the client as one string, "token:nodered-1".
func (c Client) Key() string { return c.Kind + ":" + c.Name }

// IsRadio says whether an interface sends by radio: BidCos-RF and HmIP-RF.
func IsRadio(iface string) bool { return iface == "BidCos-RF" || iface == "HmIP-RF" }

// NonRadioType says whether a device of a radio interface costs no airtime, by its type: the
// central's virtual receivers (HM-RCV-50, HmIP-RCV-50), HmIP's wired devices (HmIPW-*, the wired
// access point among them), the access points on the LAN (HmIP-HAP*) and the radio module's own
// device (HmIP-RFUSB, RPI-RF-MOD, HmIP-CCU3) - a write to those goes over a wire or nowhere.
func NonRadioType(typ string) bool {
	t := strings.ToUpper(typ)
	switch t {
	case "HMIP-RFUSB", "RPI-RF-MOD", "HMIP-CCU3":
		return true
	}
	return strings.Contains(t, "RCV") || strings.HasPrefix(t, "HMIPW-") || strings.HasPrefix(t, "HMIP-HAP")
}

// device is the device part of an address: 00010000000A10:1 → 00010000000A10.
func device(address string) string {
	d, _, _ := strings.Cut(address, ":")
	return d
}

type slot struct{ w, noop uint32 }

// ring is a run of counts, one per bucket (a minute, or BucketMinutes): slot i%len holds bucket i
// while last-len < i <= last.
type ring struct {
	last  int64
	slots []slot
}

func newRing(n int, last int64) *ring { return &ring{last: last, slots: make([]slot, n)} }

func (r *ring) advance(i int64) {
	if i <= r.last {
		return
	}
	n := int64(len(r.slots))
	if i-r.last >= n {
		clear(r.slots)
	} else {
		for k := r.last + 1; k <= i; k++ {
			r.slots[k%n] = slot{}
		}
	}
	r.last = i
}

// add counts w writes, noop of them changing nothing, into bucket i.
func (r *ring) add(i int64, w, noop uint32) {
	r.advance(i)
	if i <= r.last-int64(len(r.slots)) || i < 0 {
		return // older than the ring (the clock went back)
	}
	s := &r.slots[i%int64(len(r.slots))]
	s.w += w
	s.noop += noop
}

// at is bucket i's counts; zero outside the ring.
func (r *ring) at(i int64) slot {
	if i > r.last || i <= r.last-int64(len(r.slots)) || i < 0 {
		return slot{}
	}
	return r.slots[i%int64(len(r.slots))]
}

// series is the n buckets up to bucket now, oldest first; the last entry is the running one.
func (r *ring) series(now int64, n int) (w, noop []int) {
	w, noop = make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		s := r.at(now - int64(n-1-i))
		w[i], noop[i] = int(s.w), int(s.noop)
	}
	return w, noop
}

// counts are one client's writes to one interface: the last hour by the minute, for the levels
// and the 1 h view, and the last 24 hours by BucketMinutes, for the longer views and the database.
type counts struct {
	min  *ring
	long *ring
	// the database (history.go): gen counts the writes recorded, synced is gen when the file
	// last took them, flushed the newest bucket it has (sent again: the running bucket grows)
	gen, synced uint64
	flushed     int64
}

func newCounts(min int64) *counts {
	return &counts{min: newRing(Minutes, min), long: newRing(Buckets, min/BucketMinutes), flushed: -1}
}

// Tracker keeps the counts.
type Tracker struct {
	// Now is the clock; nil = time.Now.
	Now func() time.Time

	mu       sync.Mutex
	rings    map[Client]map[string]*counts
	excluded map[string]map[string]bool
	// gone are the database's series of clients no longer kept: the next write drops them
	gone map[string]bool
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func minuteOf(at time.Time) int64 { return at.Unix() / 60 }

// SetExcluded replaces the devices of one interface that cost no airtime (NonRadioType).
func (t *Tracker) SetExcluded(iface string, devices map[string]bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.excluded == nil {
		t.excluded = map[string]map[string]bool{}
	}
	t.excluded[iface] = devices
}

// Counted says whether a write to address on iface is a radio write: a radio interface, and
// neither the central itself (rfd's BidCoS-RF and its virtual keys, the HmIP-RCV-1) nor a device
// the interface lists as wired or virtual.
func (t *Tracker) Counted(iface, address string) bool {
	if !IsRadio(iface) {
		return false
	}
	d := device(address)
	if d == "" || strings.EqualFold(d, "BidCoS-RF") || strings.HasPrefix(strings.ToUpper(d), "HMIP-RCV-") {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.excluded[iface][d]
}

// Record counts one radio write of a client; noop says it changed nothing.
func (t *Tracker) Record(c Client, iface string, noop bool) {
	min := minuteOf(t.now())
	t.mu.Lock()
	defer t.mu.Unlock()
	k := t.countsLocked(c, iface, min)
	if k == nil {
		return
	}
	n := uint32(0)
	if noop {
		n = 1
	}
	k.min.add(min, 1, n)
	k.long.add(min/BucketMinutes, 1, n)
	k.gen++
}

// countsLocked is a client's counts on iface, made when missing; nil when maxClients are kept and
// none of them has gone quiet.
func (t *Tracker) countsLocked(c Client, iface string, min int64) *counts {
	if t.rings == nil {
		t.rings = map[Client]map[string]*counts{}
	}
	byIface := t.rings[c]
	if byIface == nil {
		if len(t.rings) >= maxClients {
			t.pruneLocked(min)
			if len(t.rings) >= maxClients {
				return nil
			}
		}
		byIface = map[string]*counts{}
		t.rings[c] = byIface
	}
	k := byIface[iface]
	if k == nil {
		k = newCounts(min)
		byIface[iface] = k
		delete(t.gone, seriesName(c, iface))
	}
	return k
}

// pruneLocked forgets the clients that sent nothing within the 24 hours; the database's series
// of them go with the next write.
func (t *Tracker) pruneLocked(now int64) {
	for c, byIface := range t.rings {
		quiet := true
		for _, k := range byIface {
			if now/BucketMinutes-k.long.last < Buckets {
				quiet = false
			}
		}
		if quiet {
			for iface := range byIface {
				t.goneLocked(seriesName(c, iface))
			}
			delete(t.rings, c)
		}
	}
}

func (t *Tracker) goneLocked(name string) {
	if t.gone == nil {
		t.gone = map[string]bool{}
	}
	t.gone[name] = true
}

// DutySample is one reading of an interface's duty cycle.
type DutySample struct {
	Time      time.Time
	DutyCycle int
	// Connected false: the interface did not answer; its duty cycle then says nothing.
	Connected bool
}

// DutySeries is the duty cycle of one radio module or access point of an interface.
type DutySeries struct {
	Interface string
	Address   string
	// Radio is the physical transmitter (the radio plan's key, occulited task 156) and RadioName
	// its name: the series of one radio - BidCos-RF and HmIP-RF on one module through multimacd -
	// are one row of the view. Empty: the series is a radio of its own.
	Radio     string
	RadioName string
	Samples   []DutySample
}

// Thresholds are the constants, for the page.
type Thresholds struct {
	AvgPerMinute   int     `json:"average_per_minute"`
	AvgMinutes     int     `json:"average_minutes"`
	BurstPerMinute int     `json:"burst_per_minute"`
	HoldMinutes    int     `json:"hold_minutes"`
	RisePoints     int     `json:"duty_cycle_rise"`
	RiseMinutes    int     `json:"duty_cycle_minutes"`
	CauseShare     float64 `json:"share"`
	CauseMinWrites int     `json:"min_writes"`
}

// Hint is why a client carries the hint: the busiest minute and the highest average over
// AvgMinutes within the last HoldMinutes, and which of the two is above its threshold.
type Hint struct {
	Burst       bool    `json:"burst"`
	Average     bool    `json:"average"`
	PeakMinute  int     `json:"peak_minute"`
	PeakAverage float64 `json:"peak_average"`
}

// Cause is why a client carries the warning: the duty cycle's rise on one interface and the
// client's share of that interface's radio writes in the same time.
type Cause struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	// From and To are the duty cycle before and after the rise, in percent.
	From int `json:"from"`
	To   int `json:"to"`
	// Writes are the client's, Total every client's on the interface, within RiseMinutes.
	Writes int `json:"writes"`
	Total  int `json:"total"`
	// SharePercent is Writes of Total.
	SharePercent int `json:"share_percent"`
}

// InterfaceCount is a client's hour on one interface, and its writes in the view's span.
type InterfaceCount struct {
	Name   string `json:"name"`
	Hour   int    `json:"hour"`
	Noop   int    `json:"noop"`
	Writes int    `json:"writes"`
}

// ClientView is one client as the API answers it.
type ClientView struct {
	Client
	// Label is the name an administrator gave a paired program; set by the API layer.
	Label string `json:"label,omitempty"`
	// PerMinute is the rate now: the running minute, plus the part of the minute before that a
	// sliding minute still covers.
	PerMinute int `json:"per_minute"`
	// Hour and Noop are the last 60 minutes: the writes, and those that changed nothing.
	Hour int `json:"hour"`
	Noop int `json:"noop"`
	// Minutes are the writes per minute, oldest first; the last is the running minute.
	Minutes []int `json:"minutes"`
	// Buckets are the writes per bucket of the view's span (openccu-lite task 489), oldest first,
	// the last the running one; Writes and WritesNoop their sums.
	Buckets    []int            `json:"buckets"`
	Writes     int              `json:"writes"`
	WritesNoop int              `json:"writes_noop"`
	Interfaces []InterfaceCount `json:"interfaces"`
	// Level is empty, hint or warning.
	Level string `json:"level,omitempty"`
	Hint  *Hint  `json:"hint,omitempty"`
	Cause *Cause `json:"cause,omitempty"`
}

// DutyView is the duty cycle of one physical radio on the ring's time axis: per minute (and per
// bucket of the span) the highest reading of its series, nil where there was none. Interface and
// Address are its first series'; a radio shared by BidCos-RF and HmIP-RF through multimacd is one
// row with both in Interfaces (openccu-lite task 489). A radio without a reading in the span is
// left out.
type DutyView struct {
	Interface  string   `json:"interface"`
	Address    string   `json:"address"`
	Interfaces []string `json:"interfaces"`
	Radio      string   `json:"radio,omitempty"`
	Name       string   `json:"name,omitempty"`
	Minutes    []*int   `json:"minutes"`
	Buckets    []*int   `json:"buckets"`
}

// View is the whole answer.
type View struct {
	At time.Time `json:"at"`
	// Span is the minutes the buckets cover (Spans), BucketMinutes one bucket's.
	Span          int          `json:"span"`
	BucketMinutes int          `json:"bucket_minutes"`
	Thresholds    Thresholds   `json:"thresholds"`
	Clients       []ClientView `json:"clients"`
	DutyCycle     []DutyView   `json:"duty_cycle"`
}

// Spans are the views offered, in minutes: 1 h by the minute, 6 h by five minutes, 24 h by a
// quarter of an hour - 60, 72 and 96 bars.
var Spans = []int{60, 360, 1440}

// SpanBucket is the bucket of a span, in minutes; 0 for a span not offered.
func SpanBucket(span int) int {
	switch span {
	case 60:
		return 1
	case 360:
		return 5
	case 1440:
		return 15
	}
	return 0
}

// TheThresholds are the constants as the API states them.
func TheThresholds() Thresholds {
	return Thresholds{AvgPerMinute: AvgPerMinute, AvgMinutes: AvgMinutes, BurstPerMinute: BurstPerMinute, HoldMinutes: HoldMinutes,
		RisePoints: RisePoints, RiseMinutes: RiseMinutes, CauseShare: CauseShare, CauseMinWrites: CauseMinWrites}
}

// View is the counts of the last hour with their levels. duty are the radio interfaces' duty
// cycles (other interfaces' series are ignored); transfer names the interfaces on which a device
// firmware transfer runs - it takes airtime by itself, so no client is blamed there meanwhile.
func (t *Tracker) View(duty []DutySeries, transfer map[string]bool) View {
	return t.ViewSpan(duty, transfer, 60)
}

// ViewSpan is View with the buckets of a span (Spans; another is 60): the levels are always the
// last hour's.
func (t *Tracker) ViewSpan(duty []DutySeries, transfer map[string]bool, span int) View {
	bm := SpanBucket(span)
	if bm == 0 {
		span, bm = 60, 1
	}
	nb := span / bm
	now := t.now()
	min := minuteOf(now)
	v := View{At: now, Span: span, BucketMinutes: bm, Thresholds: TheThresholds(), Clients: []ClientView{}, DutyCycle: []DutyView{}}

	type counted struct {
		view    ClientView
		byIface map[string][]int
	}
	var list []counted
	t.mu.Lock()
	t.pruneLocked(min)
	for c, byIface := range t.rings {
		cv := counted{view: ClientView{Client: c, Minutes: make([]int, Minutes), Buckets: make([]int, nb), Interfaces: []InterfaceCount{}}, byIface: map[string][]int{}}
		for name, k := range byIface {
			w, noop := k.min.series(min, Minutes)
			bw, bnoop := k.spanSeries(min, bm, nb)
			ic := InterfaceCount{Name: name}
			for i := range w {
				cv.view.Minutes[i] += w[i]
				ic.Hour += w[i]
				ic.Noop += noop[i]
			}
			for i := range bw {
				cv.view.Buckets[i] += bw[i]
				ic.Writes += bw[i]
				cv.view.WritesNoop += bnoop[i]
			}
			if ic.Hour == 0 && ic.Writes == 0 {
				continue
			}
			cv.byIface[name] = w
			cv.view.Hour += ic.Hour
			cv.view.Noop += ic.Noop
			cv.view.Writes += ic.Writes
			cv.view.Interfaces = append(cv.view.Interfaces, ic)
		}
		if cv.view.Hour == 0 && cv.view.Writes == 0 {
			continue
		}
		sort.Slice(cv.view.Interfaces, func(a, b int) bool { return cv.view.Interfaces[a].Name < cv.view.Interfaces[b].Name })
		list = append(list, cv)
	}
	t.mu.Unlock()
	// a fixed order, so two clients with the same share are always named the same way round
	sort.Slice(list, func(a, b int) bool { return list[a].view.Key() < list[b].view.Key() })

	frac := float64(now.Unix()%60) / 60
	for i := range list {
		m := list[i].view.Minutes
		list[i].view.PerMinute = m[Minutes-1] + int(math.Round(float64(m[Minutes-2])*(1-frac)))
		if h := hintOf(m); h != nil {
			list[i].view.Hint, list[i].view.Level = h, LevelHint
		}
	}

	// the warning: per duty-cycle series that rose, the client with the largest share
	sort.Slice(duty, func(a, b int) bool {
		if duty[a].Interface != duty[b].Interface {
			return duty[a].Interface < duty[b].Interface
		}
		return duty[a].Address < duty[b].Address
	})
	for _, ds := range duty {
		if !IsRadio(ds.Interface) {
			continue
		}
		from, to, ok := rise(ds.Samples, now)
		if !ok || transfer[ds.Interface] {
			continue
		}
		total, best, bestN := 0, -1, 0
		for i := range list {
			w, n := list[i].byIface[ds.Interface], 0
			if w == nil {
				continue
			}
			for k := Minutes - 1 - RiseMinutes; k < Minutes; k++ {
				n += w[k]
			}
			total += n
			if n > bestN {
				best, bestN = i, n
			}
		}
		if best < 0 || bestN < CauseMinWrites || float64(bestN) < CauseShare*float64(total) {
			continue
		}
		cv := &list[best].view
		// one cause per client: the larger rise stays
		if cv.Cause != nil && cv.Cause.To-cv.Cause.From >= to-from {
			continue
		}
		cv.Level = LevelWarning
		cv.Cause = &Cause{Interface: ds.Interface, Address: ds.Address, From: from, To: to, Writes: bestN, Total: total, SharePercent: int(math.Round(float64(bestN) * 100 / float64(total)))}
	}
	v.DutyCycle = dutyRows(duty, min, bm, nb)

	rank := map[string]int{LevelWarning: 0, LevelHint: 1, "": 2}
	sort.Slice(list, func(a, b int) bool {
		x, y := list[a].view, list[b].view
		if rank[x.Level] != rank[y.Level] {
			return rank[x.Level] < rank[y.Level]
		}
		if x.Writes != y.Writes {
			return x.Writes > y.Writes
		}
		if x.Hour != y.Hour {
			return x.Hour > y.Hour
		}
		return x.Key() < y.Key()
	})
	for _, c := range list {
		v.Clients = append(v.Clients, c.view)
	}
	return v
}

// spanSeries is the nb buckets of bm minutes up to the running one: the minute ring for 1 h, the
// long ring's five minutes as they are or summed three to a quarter of an hour (both start at
// the epoch, so a quarter is exactly three of them).
func (k *counts) spanSeries(min int64, bm, nb int) (w, noop []int) {
	switch bm {
	case 1:
		return k.min.series(min, nb)
	case BucketMinutes:
		return k.long.series(min/BucketMinutes, nb)
	}
	per := int64(bm / BucketMinutes)
	w, noop = make([]int, nb), make([]int, nb)
	now := min / int64(bm)
	for i := 0; i < nb; i++ {
		b := now - int64(nb-1-i)
		for j := int64(0); j < per; j++ {
			s := k.long.at(b*per + j)
			w[i] += int(s.w)
			noop[i] += int(s.noop)
		}
	}
	return w, noop
}

// dutyRows is one row per physical radio: its series' highest reading per minute and per bucket.
// duty is sorted by interface and address, so a radio's row carries its first series' names.
func dutyRows(duty []DutySeries, min int64, bm, nb int) []DutyView {
	out := []DutyView{}
	idx := map[string]int{}
	for _, ds := range duty {
		if !IsRadio(ds.Interface) {
			continue
		}
		key := ds.Radio
		if key == "" {
			key = "if:" + ds.Interface + "/" + ds.Address
		}
		i, ok := idx[key]
		if !ok {
			i = len(out)
			idx[key] = i
			out = append(out, DutyView{Interface: ds.Interface, Address: ds.Address, Interfaces: []string{}, Radio: ds.Radio, Name: ds.RadioName, Minutes: make([]*int, Minutes), Buckets: make([]*int, nb)})
		}
		r := &out[i]
		if !slices.Contains(r.Interfaces, ds.Interface) {
			r.Interfaces = append(r.Interfaces, ds.Interface)
		}
		maxInto(r.Minutes, ds.Samples, min, 1)
		maxInto(r.Buckets, ds.Samples, min, bm)
	}
	// a radio without a reading in the span (a gateway that is configured and not there) is no row
	kept := out[:0]
	for _, r := range out {
		if slices.ContainsFunc(r.Buckets, func(p *int) bool { return p != nil }) || slices.ContainsFunc(r.Minutes, func(p *int) bool { return p != nil }) {
			sort.Strings(r.Interfaces)
			kept = append(kept, r)
		}
	}
	return kept
}

// maxInto puts samples onto buckets of bm minutes ending with the one of minute now: the highest
// reading of each; readings while the interface did not answer are no reading.
func maxInto(out []*int, samples []DutySample, now int64, bm int) {
	n := int64(len(out))
	last := now / int64(bm)
	for _, s := range samples {
		if !s.Connected {
			continue
		}
		i := n - 1 - (last - minuteOf(s.Time)/int64(bm))
		if i < 0 || i >= n {
			continue
		}
		if out[i] == nil || s.DutyCycle > *out[i] {
			dc := s.DutyCycle
			out[i] = &dc
		}
	}
}

// hintOf looks at the last HoldMinutes of a client's minutes (the running one included): a
// minute above BurstPerMinute, or AvgMinutes in a row above AvgPerMinute on average.
func hintOf(m []int) *Hint {
	h := Hint{}
	for i := len(m) - HoldMinutes; i < len(m); i++ {
		if m[i] > h.PeakMinute {
			h.PeakMinute = m[i]
		}
		sum := 0
		for k := i - AvgMinutes + 1; k <= i; k++ {
			sum += m[k]
		}
		if avg := float64(sum) / AvgMinutes; avg > h.PeakAverage {
			h.PeakAverage = avg
		}
	}
	h.Burst = h.PeakMinute > BurstPerMinute
	h.Average = h.PeakAverage > AvgPerMinute
	if !h.Burst && !h.Average {
		return nil
	}
	h.PeakAverage = math.Round(h.PeakAverage*10) / 10
	return &h
}

// rise is the largest rise of a duty cycle within the last RiseMinutes - a later reading above an
// earlier one - when it is RisePoints or more. Readings taken while the interface did not answer
// are skipped: their 0 is no duty cycle.
func rise(samples []DutySample, now time.Time) (from, to int, ok bool) {
	since := now.Add(-RiseMinutes * time.Minute)
	low, have, best := 0, false, 0
	for _, s := range samples {
		if !s.Connected || s.Time.Before(since) || s.Time.After(now) {
			continue
		}
		if !have || s.DutyCycle < low {
			low, have = s.DutyCycle, true
		}
		if d := s.DutyCycle - low; d > best {
			best, from, to = d, low, s.DutyCycle
		}
	}
	return from, to, best >= RisePoints
}

// Noop says whether a write changes nothing: every value it writes is the one the datapoint
// already has. args are the call's parameters after the address, as JSON carries them - setValue's
// (key, value), putParamset's (paramset, {key: value}); last answers a datapoint's last known
// value. Only the VALUES paramset is judged, and never a key press or another command without a
// state: writing true to PRESS_SHORT again is a new press.
func Noop(method, address string, args []any, last func(address, datapoint string) (any, bool)) bool {
	if last == nil || len(args) < 2 {
		return false
	}
	same := func(dp string, v any) bool {
		if command(dp) {
			return false
		}
		have, ok := last(address, dp)
		return ok && sameValue(have, v)
	}
	switch method {
	case "setValue":
		dp, ok := args[0].(string)
		return ok && same(dp, args[1])
	case "putParamset":
		if ps, _ := args[0].(string); ps != "VALUES" {
			return false
		}
		set, _ := args[1].(map[string]any)
		if len(set) == 0 {
			return false
		}
		for dp, v := range set {
			if !same(dp, v) {
				return false
			}
		}
		return true
	}
	return false
}

// command says whether a datapoint is a command, not a state: the key presses, and what ends in
// _COMMAND or names a stop.
func command(dp string) bool {
	return strings.HasPrefix(dp, "PRESS_") || strings.HasSuffix(dp, "_COMMAND") || dp == "STOP" || dp == "INSTALL_TEST"
}

// sameValue compares a written value with a reported one across their encodings: an integer
// written to a float datapoint (LEVEL 1 for 1.0) is the same value.
func sameValue(a, b any) bool {
	if x, ok := number(a); ok {
		y, ok := number(b)
		return ok && math.Abs(x-y) < 1e-6
	}
	switch x := a.(type) {
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case string:
		y, ok := b.(string)
		return ok && x == y
	}
	return false
}

func number(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
