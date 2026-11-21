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
	"sort"
	"strings"
	"sync"
	"time"
)

// The thresholds (occulited task 163).
const (
	// Minutes is the ring's length.
	Minutes = 60
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

// ring is 60 one-minute counts; slot m%Minutes holds minute m while last-Minutes < m <= last.
type ring struct {
	last  int64
	slots [Minutes]slot
}

func (r *ring) advance(min int64) {
	if min <= r.last {
		return
	}
	if min-r.last >= Minutes {
		r.slots = [Minutes]slot{}
	} else {
		for m := r.last + 1; m <= min; m++ {
			r.slots[m%Minutes] = slot{}
		}
	}
	r.last = min
}

func (r *ring) add(min int64, noop bool) {
	r.advance(min)
	if min <= r.last-Minutes {
		return // older than the ring (the clock went back)
	}
	s := &r.slots[min%Minutes]
	s.w++
	if noop {
		s.noop++
	}
}

// series is the ring up to minute now, oldest first; the last entry is the running minute.
func (r *ring) series(now int64) (w, noop [Minutes]int) {
	for i := 0; i < Minutes; i++ {
		m := now - int64(Minutes-1-i)
		if m > r.last || m <= r.last-Minutes || m < 0 {
			continue
		}
		s := r.slots[m%Minutes]
		w[i], noop[i] = int(s.w), int(s.noop)
	}
	return w, noop
}

// Tracker keeps the counts.
type Tracker struct {
	// Now is the clock; nil = time.Now.
	Now func() time.Time

	mu       sync.Mutex
	rings    map[Client]map[string]*ring
	excluded map[string]map[string]bool
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
	if t.rings == nil {
		t.rings = map[Client]map[string]*ring{}
	}
	byIface := t.rings[c]
	if byIface == nil {
		if len(t.rings) >= maxClients {
			t.pruneLocked(min)
			if len(t.rings) >= maxClients {
				return
			}
		}
		byIface = map[string]*ring{}
		t.rings[c] = byIface
	}
	r := byIface[iface]
	if r == nil {
		r = &ring{last: min}
		byIface[iface] = r
	}
	r.add(min, noop)
}

// pruneLocked forgets the clients that sent nothing within the ring.
func (t *Tracker) pruneLocked(now int64) {
	for c, byIface := range t.rings {
		quiet := true
		for _, r := range byIface {
			if now-r.last < Minutes {
				quiet = false
			}
		}
		if quiet {
			delete(t.rings, c)
		}
	}
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

// InterfaceCount is a client's hour on one interface.
type InterfaceCount struct {
	Name string `json:"name"`
	Hour int    `json:"hour"`
	Noop int    `json:"noop"`
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
	Minutes    []int            `json:"minutes"`
	Interfaces []InterfaceCount `json:"interfaces"`
	// Level is empty, hint or warning.
	Level string `json:"level,omitempty"`
	Hint  *Hint  `json:"hint,omitempty"`
	Cause *Cause `json:"cause,omitempty"`
}

// DutyView is one duty-cycle series on the ring's time axis: per minute the highest reading, nil
// where there was none. A series without a reading in the hour is left out.
type DutyView struct {
	Interface string `json:"interface"`
	Address   string `json:"address"`
	Minutes   []*int `json:"minutes"`
}

// View is the whole answer.
type View struct {
	At         time.Time    `json:"at"`
	Thresholds Thresholds   `json:"thresholds"`
	Clients    []ClientView `json:"clients"`
	DutyCycle  []DutyView   `json:"duty_cycle"`
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
	now := t.now()
	min := minuteOf(now)
	v := View{At: now, Thresholds: TheThresholds(), Clients: []ClientView{}, DutyCycle: []DutyView{}}

	type counted struct {
		view    ClientView
		byIface map[string][Minutes]int
	}
	var list []counted
	t.mu.Lock()
	t.pruneLocked(min)
	for c, byIface := range t.rings {
		cv := counted{view: ClientView{Client: c, Minutes: make([]int, Minutes), Interfaces: []InterfaceCount{}}, byIface: map[string][Minutes]int{}}
		for name, r := range byIface {
			w, noop := r.series(min)
			ic := InterfaceCount{Name: name}
			for i := range w {
				cv.view.Minutes[i] += w[i]
				ic.Hour += w[i]
				ic.Noop += noop[i]
			}
			if ic.Hour == 0 {
				continue
			}
			cv.byIface[name] = w
			cv.view.Hour += ic.Hour
			cv.view.Noop += ic.Noop
			cv.view.Interfaces = append(cv.view.Interfaces, ic)
		}
		if cv.view.Hour == 0 {
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
		// a series without a reading in the hour (a gateway that is configured and not there) is no row
		if m, any := dutyMinutes(ds.Samples, min); any {
			v.DutyCycle = append(v.DutyCycle, DutyView{Interface: ds.Interface, Address: ds.Address, Minutes: m})
		}
		from, to, ok := rise(ds.Samples, now)
		if !ok || transfer[ds.Interface] {
			continue
		}
		total, best, bestN := 0, -1, 0
		for i := range list {
			w, n := list[i].byIface[ds.Interface], 0
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

	rank := map[string]int{LevelWarning: 0, LevelHint: 1, "": 2}
	sort.Slice(list, func(a, b int) bool {
		x, y := list[a].view, list[b].view
		if rank[x.Level] != rank[y.Level] {
			return rank[x.Level] < rank[y.Level]
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

// dutyMinutes puts a series onto the ring's minutes: the highest reading of each minute; any is
// false when the hour holds none.
func dutyMinutes(samples []DutySample, now int64) (out []*int, any bool) {
	out = make([]*int, Minutes)
	for _, s := range samples {
		if !s.Connected {
			continue
		}
		i := Minutes - 1 - int(now-minuteOf(s.Time))
		if i < 0 || i >= Minutes {
			continue
		}
		if out[i] == nil || s.DutyCycle > *out[i] {
			dc := s.DutyCycle
			out[i], any = &dc, true
		}
	}
	return out, any
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
