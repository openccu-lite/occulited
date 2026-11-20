// Package iowatch counts what is written to the disks, and by which service (occulited task 159).
//
// Two sources, both read from files the kernel keeps in memory, so sampling writes nothing itself:
//
//   - /proc/diskstats: every disk's and partition's sectors read and written since the boot. Always
//     there; it is the total, and the rate of each disk.
//   - the unified cgroup tree's io.stat: bytes read and written per cgroup and disk, there only
//     while the io controller is enabled for the services (systemd's DefaultIOAccounting=, the
//     switch of the Services page). A unit's cgroup counts from its start, or from the moment the
//     controller was enabled.
//
// A cgroup's counters go with the cgroup: a service that is restarted starts at zero, one that
// ended is gone. The sampler therefore reads every Interval and keeps, per unit, what its former
// runs had written when they were last seen (the cgroup directory's inode tells one run from the
// next). What a run wrote between its last sample and its end, and a unit that came and went
// between two samples, stays in the parent slice's counter - the kernel moves a removed cgroup's
// figures there - and is reported as the row "ended": counted, but not attributable to a unit.
// What the root cgroup counts beyond its children is the kernel's own: the file systems' journals
// (jbd2), other kernel threads - the row "kernel".
//
// All of it lives in memory. The per-unit sums and the hourly snapshots are also written to a
// file in a RAM file system (StateFile, under /run), so a restart of occulited keeps them; a
// reboot starts from zero like the kernel's counters do.
package iowatch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Counter is bytes read and written.
type Counter struct {
	Read    uint64 `json:"r"`
	Written uint64 `json:"w"`
}

func (c Counter) add(o Counter) Counter {
	return Counter{Read: c.Read + o.Read, Written: c.Written + o.Written}
}

// sub is c - o, never below zero.
func (c Counter) sub(o Counter) Counter {
	var out Counter
	if c.Read > o.Read {
		out.Read = c.Read - o.Read
	}
	if c.Written > o.Written {
		out.Written = c.Written - o.Written
	}
	return out
}

// The rows that are not a unit.
const (
	// RowKernel is what the root cgroup counts beyond its children: the file systems' journals
	// and other kernel threads, since the recording began.
	RowKernel = "kernel"
	// RowEnded is what the slices count beyond their running units and the sums kept for the
	// units seen: runs that ended between two samples.
	RowEnded = "ended"
)

// The windows a rate is given over.
const (
	Hour = time.Hour
	Day  = 24 * time.Hour
)

// Sampler reads the counters every Interval and keeps their history.
type Sampler struct {
	// Root is the file system root /proc and /sys are read under; "" = "/".
	Root string
	// Interval between two samples; 0 = 10 s. Short, because a unit's last figures are the ones
	// of its last sample.
	Interval time.Duration
	Now      func() time.Time
	// Enabled says whether the per-unit recording is switched on; nil = whenever the kernel
	// counts. Off, the units are not read even where the io controller is enabled for another
	// reason (an administrator's own IOAccounting=yes covers some units, not all).
	Enabled func() bool
	// StateFile keeps the sums across a restart of occulited; it must lie in a RAM file system.
	// "" = nothing is kept.
	StateFile string

	mu     sync.Mutex
	loaded bool
	st     state
	// minute snapshots (the last hour); the hourly ones are in st
	disksMin, unitsMin []snapshot
	lastSave           time.Time
}

// state is what StateFile holds.
type state struct {
	Boot string `json:"boot"`
	// Since is when the per-unit recording was first seen active; zero while it is not.
	Since time.Time `json:"since"`
	// Units by unit name.
	Units map[string]*unitState `json:"units,omitempty"`
	// CarriedRoot and CarriedSlice are the sums moved into Carried from cgroups directly below
	// the root and below a slice: they are in the parents' counters too, and are taken off the
	// kernel and the ended row.
	CarriedRoot  map[string]Counter `json:"carried_root,omitempty"`
	CarriedSlice map[string]Counter `json:"carried_slice,omitempty"`
	// KernelBase is the root's remainder when the recording began: the root counts from the
	// boot, its children from the moment the controller was enabled.
	KernelBase map[string]Counter `json:"kernel_base,omitempty"`
	HaveBase   bool               `json:"have_base,omitempty"`
	// Kernel and Ended are the two rows as last computed, per device.
	Kernel map[string]Counter `json:"kernel,omitempty"`
	Ended  map[string]Counter `json:"ended,omitempty"`
	// Disks is the last reading of /proc/diskstats by device name.
	Disks map[string]diskStat `json:"-"`
	// hourly snapshots (the last day)
	DisksHour []snapshot `json:"disks_hour,omitempty"`
	UnitsHour []snapshot `json:"units_hour,omitempty"`
}

type unitState struct {
	// Ino is the cgroup directory's inode: another one is another run.
	Ino uint64 `json:"ino"`
	// Live is the running cgroup's counters per device, Carried the former runs'.
	Live    map[string]Counter `json:"live,omitempty"`
	Carried map[string]Counter `json:"carried,omitempty"`
	Running bool               `json:"running"`
	// TopLevel: the cgroup lies directly below the root, not in a slice.
	TopLevel bool `json:"top,omitempty"`
}

func (u *unitState) total(dev string) Counter { return u.Live[dev].add(u.Carried[dev]) }

type diskStat struct {
	Dev string // "8:0"
	Counter
}

// snapshot is the bytes written per key at one time: a disk's name, or unit + "\x00" + device.
type snapshot struct {
	At time.Time         `json:"at"`
	W  map[string]uint64 `json:"w"`
}

func (s *Sampler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Sampler) interval() time.Duration {
	if s.Interval > 0 {
		return s.Interval
	}
	return 10 * time.Second
}

func (s *Sampler) path(p string) string { return filepath.Join(s.Root, p) }

// Run samples until ctx ends; the first sample is taken at once.
func (s *Sampler) Run(ctx context.Context) {
	s.Poll()
	t := time.NewTicker(s.interval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.saveLocked(true)
			s.mu.Unlock()
			return
		case <-t.C:
			s.Poll()
		}
	}
}

// Active reports whether the io controller is enabled for the root's children, which is what the
// per-unit figures need.
func (s *Sampler) Active() bool {
	return hasWord(readString(s.path("/sys/fs/cgroup/cgroup.subtree_control")), "io")
}

// Supported reports whether this kernel (or, in a container, the host) offers the io controller.
func (s *Sampler) Supported() bool {
	return hasWord(readString(s.path("/sys/fs/cgroup/cgroup.controllers")), "io")
}

func hasWord(s, w string) bool {
	for _, f := range strings.Fields(s) {
		if f == w {
			return true
		}
	}
	return false
}

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// Reset forgets the per-unit figures: the recording was switched off, and the kernel's counters
// start again with the next switch on.
func (s *Sampler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetUnitsLocked()
	s.saveLocked(true)
}

func (s *Sampler) resetUnitsLocked() {
	s.st.Since = time.Time{}
	s.st.Units, s.st.CarriedRoot, s.st.CarriedSlice = nil, nil, nil
	s.st.KernelBase, s.st.HaveBase, s.st.Kernel, s.st.Ended = nil, false, nil, nil
	s.st.UnitsHour, s.unitsMin = nil, nil
}

// Poll takes one sample.
func (s *Sampler) Poll() {
	now := s.now()
	disks := s.readDiskstats()
	boot := strings.TrimSpace(readString(s.path("/proc/sys/kernel/random/boot_id")))
	active := s.Active() && (s.Enabled == nil || s.Enabled())
	var tree *cgTree
	if active {
		tree = s.readTree(disks)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.loaded = true
		s.loadLocked(boot)
	}
	s.st.Boot = boot
	s.st.Disks = disks
	dw := map[string]uint64{}
	for name, d := range disks {
		dw[name] = d.Written
	}
	s.disksMin, s.st.DisksHour = push(s.disksMin, s.st.DisksHour, snapshot{At: now, W: dw})

	if tree == nil {
		if !s.st.Since.IsZero() {
			s.resetUnitsLocked()
			s.saveLocked(true)
		}
		return
	}
	if s.st.Since.IsZero() {
		s.st.Since = now
	}
	s.applyTreeLocked(tree)
	uw := map[string]uint64{}
	for name, u := range s.st.Units {
		for _, dev := range devsOf(u.Live, u.Carried) {
			if w := u.total(dev).Written; w > 0 {
				uw[name+"\x00"+dev] = w
			}
		}
	}
	for dev, c := range s.st.Kernel {
		uw[RowKernel+"\x00"+dev] = c.Written
	}
	for dev, c := range s.st.Ended {
		uw[RowEnded+"\x00"+dev] = c.Written
	}
	s.unitsMin, s.st.UnitsHour = push(s.unitsMin, s.st.UnitsHour, snapshot{At: now, W: uw})
	s.saveLocked(false)
}

// push appends a snapshot to the minute ring when a minute has passed since its newest (61 kept)
// and to the hour ring when an hour has (25 kept).
func push(minute, hour []snapshot, sn snapshot) ([]snapshot, []snapshot) {
	if n := len(minute); n == 0 || sn.At.Sub(minute[n-1].At) >= time.Minute-time.Second || sn.At.Before(minute[n-1].At) {
		minute = append(minute, sn)
		if len(minute) > 61 {
			minute = append([]snapshot(nil), minute[len(minute)-61:]...)
		}
	}
	if n := len(hour); n == 0 || sn.At.Sub(hour[n-1].At) >= time.Hour-time.Second || sn.At.Before(hour[n-1].At) {
		hour = append(hour, sn)
		if len(hour) > 25 {
			hour = append([]snapshot(nil), hour[len(hour)-25:]...)
		}
	}
	return minute, hour
}

// cgTree is one reading of the cgroup tree.
type cgTree struct {
	units map[string]cgUnit
	// rootRest and sliceRest are what the root and the slices count beyond their children
	rootRest, sliceRest map[string]Counter
}

type cgUnit struct {
	ino      uint64
	io       map[string]Counter
	topLevel bool
}

// readTree walks the cgroup tree: a directory named *.slice is walked into, every other one is a
// unit (io.stat is recursive, so what a service's own sub-cgroups did is in its figures).
func (s *Sampler) readTree(disks map[string]diskStat) *cgTree {
	known := map[string]bool{}
	for _, d := range disks {
		known[d.Dev] = true
	}
	t := &cgTree{units: map[string]cgUnit{}, rootRest: map[string]Counter{}, sliceRest: map[string]Counter{}}
	var walk func(dir string, depth int) map[string]Counter
	walk = func(dir string, depth int) map[string]Counter {
		own := parseIOStat(readString(filepath.Join(dir, "io.stat")), known)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return own
		}
		children := map[string]Counter{}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(dir, e.Name())
			var io map[string]Counter
			if strings.HasSuffix(e.Name(), ".slice") && depth < 8 {
				io = walk(sub, depth+1)
			} else {
				io = parseIOStat(readString(filepath.Join(sub, "io.stat")), known)
				u := cgUnit{io: io, topLevel: depth == 0}
				if fi, err := os.Stat(sub); err == nil {
					if st, ok := fi.Sys().(*syscall.Stat_t); ok {
						u.ino = st.Ino
					}
				}
				// a unit name is unique in the tree; should two cgroups carry it, they add up
				if prev, ok := t.units[e.Name()]; ok {
					for dev, c := range prev.io {
						u.io[dev] = u.io[dev].add(c)
					}
				}
				t.units[e.Name()] = u
			}
			for dev, c := range io {
				children[dev] = children[dev].add(c)
			}
		}
		rest := t.sliceRest
		if depth == 0 {
			rest = t.rootRest
		}
		for dev, c := range own {
			rest[dev] = rest[dev].add(c.sub(children[dev]))
		}
		return own
	}
	walk(s.path("/sys/fs/cgroup"), 0)
	return t
}

// parseIOStat reads io.stat: "8:0 rbytes=1 wbytes=2 rios=…" per device; a device without figures
// is a bare "7:0". Only the devices in known count (a loop device's writes are its backing
// disk's as well).
func parseIOStat(text string, known map[string]bool) map[string]Counter {
	out := map[string]Counter{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !known[f[0]] {
			continue
		}
		var c Counter
		for _, kv := range f[1:] {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				continue
			}
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				continue
			}
			switch k {
			case "rbytes":
				c.Read = n
			case "wbytes":
				c.Written = n
			}
		}
		if c != (Counter{}) {
			out[f[0]] = c
		}
	}
	return out
}

func addInto(dst *map[string]Counter, src map[string]Counter) {
	if len(src) == 0 {
		return
	}
	if *dst == nil {
		*dst = map[string]Counter{}
	}
	for dev, c := range src {
		(*dst)[dev] = (*dst)[dev].add(c)
	}
}

func devsOf(maps ...map[string]Counter) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range maps {
		for dev := range m {
			if !seen[dev] {
				seen[dev] = true
				out = append(out, dev)
			}
		}
	}
	sort.Strings(out)
	return out
}

// applyTreeLocked takes a reading into the per-unit sums.
func (s *Sampler) applyTreeLocked(t *cgTree) {
	if s.st.Units == nil {
		s.st.Units = map[string]*unitState{}
	}
	carry := func(u *unitState) {
		if len(u.Live) == 0 {
			return
		}
		addInto(&u.Carried, u.Live)
		if u.TopLevel {
			addInto(&s.st.CarriedRoot, u.Live)
		} else {
			addInto(&s.st.CarriedSlice, u.Live)
		}
		u.Live = nil
	}
	for name, cu := range t.units {
		u := s.st.Units[name]
		if u == nil {
			u = &unitState{}
			s.st.Units[name] = u
		}
		// another run: another directory, or - should the inode have been reused - counters
		// that went backwards
		again := u.Ino != 0 && cu.ino != u.Ino
		for dev, old := range u.Live {
			if n := cu.io[dev]; n.Written < old.Written || n.Read < old.Read {
				again = true
			}
		}
		if again {
			carry(u)
		}
		u.Ino, u.Live, u.Running, u.TopLevel = cu.ino, cu.io, true, cu.topLevel
		if len(u.Live) == 0 {
			u.Live = nil
		}
	}
	for name, u := range s.st.Units {
		if _, ok := t.units[name]; ok {
			continue
		}
		carry(u)
		u.Running, u.Ino = false, 0
		if len(u.Carried) == 0 {
			delete(s.st.Units, name) // nothing to remember of it
		}
	}
	if !s.st.HaveBase {
		s.st.HaveBase = true
		s.st.KernelBase = map[string]Counter{}
		for dev, c := range t.rootRest {
			s.st.KernelBase[dev] = c.sub(s.st.CarriedRoot[dev])
		}
	}
	s.st.Kernel, s.st.Ended = map[string]Counter{}, map[string]Counter{}
	for dev, c := range t.rootRest {
		if k := c.sub(s.st.CarriedRoot[dev]).sub(s.st.KernelBase[dev]); k != (Counter{}) {
			s.st.Kernel[dev] = k
		}
	}
	for dev, c := range t.sliceRest {
		if e := c.sub(s.st.CarriedSlice[dev]); e != (Counter{}) {
			s.st.Ended[dev] = e
		}
	}
}

// readDiskstats reads /proc/diskstats: the disks and partitions by name. RAM-backed and stacked
// devices (loop, ram, zram, mtdblock) are left out.
func (s *Sampler) readDiskstats() map[string]diskStat {
	out := map[string]diskStat{}
	sc := bufio.NewScanner(bytes.NewReader([]byte(readString(s.path("/proc/diskstats")))))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || skippedDisk(f[2]) {
			continue
		}
		rd, err1 := strconv.ParseUint(f[5], 10, 64)
		wr, err2 := strconv.ParseUint(f[9], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		// diskstats counts 512-byte sectors, whatever the device's block size
		out[f[2]] = diskStat{Dev: f[0] + ":" + f[1], Counter: Counter{Read: rd * 512, Written: wr * 512}}
	}
	return out
}

func skippedDisk(name string) bool {
	for _, p := range []string{"loop", "ram", "zram", "mtdblock", "nbd", "sr", "fd"} {
		if rest, ok := strings.CutPrefix(name, p); ok && (rest == "" || (rest[0] >= '0' && rest[0] <= '9')) {
			return true
		}
	}
	return false
}

// partitionOf names the disk a diskstats name is a partition of ("" for a disk): the sysfs
// directory /sys/block/<disk>/<name> exists.
func (s *Sampler) partitionOf(name string, names []string) string {
	for _, d := range names {
		if d != name && strings.HasPrefix(name, d) {
			if fi, err := os.Stat(s.path("/sys/block/" + d + "/" + name)); err == nil && fi.IsDir() {
				return d
			}
		}
	}
	return ""
}

// Window is what was written in a span of time; Seconds is the span the figures cover, shorter
// than the window asked for while the history is.
type Window struct {
	Seconds      int64  `json:"seconds"`
	WrittenBytes uint64 `json:"written_bytes"`
}

// Partition is one partition of a disk, from /proc/diskstats: since the boot.
type Partition struct {
	Name         string `json:"name"`
	ReadBytes    uint64 `json:"read_bytes"`
	WrittenBytes uint64 `json:"written_bytes"`
}

// Disk is one disk: its totals since the boot and what was written in the last hour and day.
type Disk struct {
	Name         string      `json:"name"`
	Dev          string      `json:"dev"` // major:minor, the key of a unit's devices
	ReadBytes    uint64      `json:"read_bytes"`
	WrittenBytes uint64      `json:"written_bytes"`
	Hour         Window      `json:"hour"`
	Day          Window      `json:"day"`
	Partitions   []Partition `json:"partitions"`
}

// UnitDisk is a unit's figures on one disk.
type UnitDisk struct {
	Disk         string `json:"disk"` // the disk's name
	ReadBytes    uint64 `json:"read_bytes"`
	WrittenBytes uint64 `json:"written_bytes"`
	Hour         Window `json:"hour"`
	Day          Window `json:"day"`
}

// Unit is one row: a unit, or one of RowKernel and RowEnded.
type Unit struct {
	Unit string `json:"unit"`
	// Kind is "unit", "kernel" or "ended".
	Kind string `json:"kind"`
	// Running: the unit has a cgroup now.
	Running bool `json:"running"`
	// ReadBytes and WrittenBytes are since the unit's start or the start of the recording,
	// whichever was later, over all its runs that were seen.
	ReadBytes    uint64     `json:"read_bytes"`
	WrittenBytes uint64     `json:"written_bytes"`
	Hour         Window     `json:"hour"`
	Day          Window     `json:"day"`
	Disks        []UnitDisk `json:"disks"`
}

// View is what the API returns.
type View struct {
	At time.Time `json:"at"`
	// Supported: the kernel offers the io controller. Active: it is enabled, the units are
	// counted. Since is when the sampler first saw it active (absent while it is not).
	Supported bool       `json:"supported"`
	Active    bool       `json:"active"`
	Since     *time.Time `json:"since,omitempty"`
	IntervalS int        `json:"interval_s"`
	Disks     []Disk     `json:"disks"`
	// Units, the most written in the last day first; empty while the recording is not active.
	Units []Unit `json:"units"`
}

// window is the growth of key since the oldest snapshot that is not older than span.
func window(now time.Time, cur uint64, key string, span time.Duration, rings ...[]snapshot) Window {
	var best *snapshot
	for _, ring := range rings {
		for i := range ring {
			sn := &ring[i]
			// half a minute of slack: the snapshot taken an hour ago is a few seconds older
			if sn.At.After(now) || now.Sub(sn.At) > span+30*time.Second {
				continue
			}
			if best == nil || sn.At.Before(best.At) {
				best = sn
			}
		}
	}
	if best == nil {
		return Window{}
	}
	w := Window{Seconds: int64(now.Sub(best.At) / time.Second)}
	if old := best.W[key]; cur > old {
		w.WrittenBytes = cur - old
	}
	return w
}

// View returns the figures as of the last sample.
func (s *Sampler) View() View {
	supported, active := s.Supported(), s.Active() && (s.Enabled == nil || s.Enabled())
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	v := View{At: now, Supported: supported, Active: active && !s.st.Since.IsZero(), IntervalS: int(s.interval() / time.Second), Disks: []Disk{}, Units: []Unit{}}

	names := make([]string, 0, len(s.st.Disks))
	for name := range s.st.Disks {
		names = append(names, name)
	}
	sort.Strings(names)
	byDev := map[string]string{}
	index := map[string]int{}
	var parts []string
	for _, name := range names {
		if s.partitionOf(name, names) != "" {
			parts = append(parts, name)
			continue
		}
		d := s.st.Disks[name]
		byDev[d.Dev] = name
		index[name] = len(v.Disks)
		v.Disks = append(v.Disks, Disk{Name: name, Dev: d.Dev, ReadBytes: d.Read, WrittenBytes: d.Written,
			Hour: window(now, d.Written, name, Hour, s.disksMin), Day: window(now, d.Written, name, Day, s.disksMin, s.st.DisksHour), Partitions: []Partition{}})
	}
	for _, name := range parts {
		p := s.st.Disks[name]
		i := index[s.partitionOf(name, names)]
		v.Disks[i].Partitions = append(v.Disks[i].Partitions, Partition{Name: name, ReadBytes: p.Read, WrittenBytes: p.Written})
	}
	if !v.Active {
		return v
	}
	since := s.st.Since
	v.Since = &since
	row := func(name, kind string, running bool, devs []string, total func(dev string) Counter) {
		u := Unit{Unit: name, Kind: kind, Running: running, Disks: []UnitDisk{}}
		for _, dev := range devs {
			c := total(dev)
			disk, ok := byDev[dev]
			if !ok || c == (Counter{}) {
				continue
			}
			key := name + "\x00" + dev
			ud := UnitDisk{Disk: disk, ReadBytes: c.Read, WrittenBytes: c.Written,
				Hour: window(now, c.Written, key, Hour, s.unitsMin), Day: window(now, c.Written, key, Day, s.unitsMin, s.st.UnitsHour)}
			u.Disks = append(u.Disks, ud)
			u.ReadBytes += c.Read
			u.WrittenBytes += c.Written
			u.Hour.WrittenBytes += ud.Hour.WrittenBytes
			u.Day.WrittenBytes += ud.Day.WrittenBytes
			u.Hour.Seconds = max(u.Hour.Seconds, ud.Hour.Seconds)
			u.Day.Seconds = max(u.Day.Seconds, ud.Day.Seconds)
		}
		if len(u.Disks) == 0 && kind != "unit" {
			return
		}
		sort.Slice(u.Disks, func(i, j int) bool { return u.Disks[i].Disk < u.Disks[j].Disk })
		v.Units = append(v.Units, u)
	}
	for name, u := range s.st.Units {
		row(name, "unit", u.Running, devsOf(u.Live, u.Carried), u.total)
	}
	row(RowKernel, "kernel", true, devsOf(s.st.Kernel), func(dev string) Counter { return s.st.Kernel[dev] })
	row(RowEnded, "ended", false, devsOf(s.st.Ended), func(dev string) Counter { return s.st.Ended[dev] })
	sort.Slice(v.Units, func(i, j int) bool {
		a, b := v.Units[i], v.Units[j]
		if a.Day.WrittenBytes != b.Day.WrittenBytes {
			return a.Day.WrittenBytes > b.Day.WrittenBytes
		}
		if a.WrittenBytes != b.WrittenBytes {
			return a.WrittenBytes > b.WrittenBytes
		}
		return a.Unit < b.Unit
	})
	return v
}

// loadLocked takes the state file of this boot; another boot's is ignored.
func (s *Sampler) loadLocked(boot string) {
	if s.StateFile == "" {
		return
	}
	b, err := os.ReadFile(s.StateFile)
	if err != nil {
		return
	}
	var st state
	if json.Unmarshal(b, &st) != nil || st.Boot == "" || st.Boot != boot {
		return
	}
	s.st = st
}

// saveLocked writes the state file, at most once a minute unless now is set. The file is in a
// RAM file system: the write reaches no disk.
func (s *Sampler) saveLocked(now bool) {
	if s.StateFile == "" || !s.loaded {
		return
	}
	t := s.now()
	if !now && !s.lastSave.IsZero() && t.Sub(s.lastSave) < time.Minute && !t.Before(s.lastSave) {
		return
	}
	s.lastSave = t
	b, err := json.Marshal(s.st)
	if err != nil {
		return
	}
	tmp := s.StateFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, s.StateFile); err != nil {
		_ = os.Remove(tmp)
	}
}
