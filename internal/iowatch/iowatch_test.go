package iowatch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// box is a fake /proc and /sys under a temporary root.
type box struct {
	t    *testing.T
	root string
	now  time.Time
}

func newBox(t *testing.T) *box {
	b := &box{t: t, root: t.TempDir(), now: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	b.write("/proc/sys/kernel/random/boot_id", "boot-1\n")
	b.write("/sys/fs/cgroup/cgroup.controllers", "cpuset cpu io memory pids\n")
	b.write("/sys/fs/cgroup/cgroup.subtree_control", "cpu memory pids\n")
	for _, d := range []string{"/sys/block/mmcblk0/mmcblk0p1", "/sys/block/mmcblk0/mmcblk0p3", "/sys/block/sda/sda1"} {
		if err := os.MkdirAll(filepath.Join(b.root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func (b *box) write(path, text string) {
	b.t.Helper()
	full := filepath.Join(b.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		b.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *box) remove(path string) {
	b.t.Helper()
	if err := os.RemoveAll(filepath.Join(b.root, path)); err != nil {
		b.t.Fatal(err)
	}
}

// diskstats writes the card (179:0, partitions 1 and 3) and a stick (8:0, partition 1) with the
// sectors written on the card's third partition and on the stick.
func (b *box) diskstats(cardSectors, stickSectors uint64) {
	line := func(maj, min int, name string, rd, wr uint64) string {
		return " " + itoa(uint64(maj)) + " " + itoa(uint64(min)) + " " + name + " 10 0 " + itoa(rd) + " 5 20 0 " + itoa(wr) + " 9 0 1 2 0 0 0 0 0 0\n"
	}
	b.write("/proc/diskstats",
		line(7, 0, "loop0", 800, 0)+
			line(179, 0, "mmcblk0", 2000, cardSectors)+
			line(179, 1, "mmcblk0p1", 100, 0)+
			line(179, 3, "mmcblk0p3", 1900, cardSectors)+
			line(8, 0, "sda", 50, stickSectors)+
			line(8, 1, "sda1", 50, stickSectors))
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	return string(d)
}

func (b *box) enable() {
	b.write("/sys/fs/cgroup/cgroup.subtree_control", "cpu io memory pids\n")
}

func (b *box) iostat(cgroup, text string) { b.write("/sys/fs/cgroup/"+cgroup+"/io.stat", text) }

func (b *box) sampler() *Sampler {
	return &Sampler{Root: b.root, Now: func() time.Time { return b.now }}
}

func unit(v View, name string) *Unit {
	for i := range v.Units {
		if v.Units[i].Unit == name {
			return &v.Units[i]
		}
	}
	return nil
}

func TestDisksWithoutRecording(t *testing.T) {
	b := newBox(t)
	b.diskstats(1000, 10)
	s := b.sampler()
	s.Poll()
	b.now = b.now.Add(30 * time.Minute)
	b.diskstats(1000+2048, 10)
	s.Poll()
	v := s.View()
	if !v.Supported || v.Active || v.Since != nil || len(v.Units) != 0 {
		t.Fatalf("view = %+v", v)
	}
	if len(v.Disks) != 2 || v.Disks[0].Name != "mmcblk0" || v.Disks[1].Name != "sda" {
		t.Fatalf("disks = %+v", v.Disks)
	}
	card := v.Disks[0]
	if card.Dev != "179:0" || card.WrittenBytes != 3048*512 || card.ReadBytes != 2000*512 {
		t.Errorf("card = %+v", card)
	}
	want := []Partition{{Name: "mmcblk0p1", ReadBytes: 100 * 512}, {Name: "mmcblk0p3", ReadBytes: 1900 * 512, WrittenBytes: 3048 * 512}}
	if !reflect.DeepEqual(card.Partitions, want) {
		t.Errorf("partitions = %+v", card.Partitions)
	}
	if card.Hour != (Window{Seconds: 1800, WrittenBytes: 2048 * 512}) || card.Day != card.Hour {
		t.Errorf("windows = %+v %+v", card.Hour, card.Day)
	}
}

func TestUnitsKernelAndEnded(t *testing.T) {
	b := newBox(t)
	b.diskstats(1000, 0)
	b.enable()
	// the root counts from the boot: 400 000 before the recording began
	b.iostat("", "179:0 rbytes=10 wbytes=400000 rios=1 wios=1\n7:0 rbytes=5 wbytes=0\n8:0 \n")
	b.iostat("init.scope", "")
	b.iostat("system.slice", "")
	b.iostat("system.slice/rfd.service", "")
	b.iostat("system.slice/addon-redmatic.service", "")
	b.iostat("system.slice/system-getty.slice", "")
	b.iostat("system.slice/system-getty.slice/getty@tty1.service", "")
	s := b.sampler()
	s.Poll()
	v := s.View()
	if !v.Active || v.Since == nil || !v.Since.Equal(b.now) {
		t.Fatalf("view = %+v", v)
	}
	// units that wrote nothing are listed (they are counted), the two other rows are not
	if len(v.Units) != 4 || unit(v, RowKernel) != nil || unit(v, RowEnded) != nil {
		t.Fatalf("units = %+v", v.Units)
	}

	b.now = b.now.Add(10 * time.Minute)
	b.iostat("", "179:0 rbytes=10 wbytes=1000000\n8:0 rbytes=0 wbytes=7000\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=500000\n8:0 rbytes=0 wbytes=7000\n")
	b.iostat("system.slice/rfd.service", "179:0 rbytes=0 wbytes=100000\n")
	b.iostat("system.slice/addon-redmatic.service", "179:0 rbytes=4096 wbytes=300000\n8:0 rbytes=0 wbytes=7000\n")
	s.Poll()
	v = s.View()
	red := unit(v, "addon-redmatic.service")
	if red == nil || red.WrittenBytes != 307000 || red.ReadBytes != 4096 || !red.Running || red.Kind != "unit" {
		t.Fatalf("redmatic = %+v", red)
	}
	wantDisks := []UnitDisk{
		{Disk: "mmcblk0", ReadBytes: 4096, WrittenBytes: 300000, Hour: Window{600, 300000}, Day: Window{600, 300000}},
		{Disk: "sda", WrittenBytes: 7000, Hour: Window{600, 7000}, Day: Window{600, 7000}},
	}
	if !reflect.DeepEqual(red.Disks, wantDisks) {
		t.Errorf("redmatic disks = %+v", red.Disks)
	}
	if red.Hour != (Window{600, 307000}) {
		t.Errorf("redmatic hour = %+v", red.Hour)
	}
	// the kernel: the root's 1 000 000 less the slice's 500 000 less the 400 000 from before
	if k := unit(v, RowKernel); k == nil || k.WrittenBytes != 100000 || k.Kind != "kernel" {
		t.Errorf("kernel = %+v", k)
	}
	// ended: the slice's 500 000 less its two units' 400 000 - a run nobody saw
	if e := unit(v, RowEnded); e == nil || e.WrittenBytes != 100000 || e.Kind != "ended" || e.Running {
		t.Errorf("ended = %+v", e)
	}
	if v.Units[0].Unit != "addon-redmatic.service" {
		t.Errorf("order: %s first", v.Units[0].Unit)
	}
}

func TestRestartAndEndKeepTheSum(t *testing.T) {
	b := newBox(t)
	b.diskstats(0, 0)
	b.enable()
	b.iostat("", "179:0 rbytes=0 wbytes=0\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=50000\n")
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=50000\n")
	s := b.sampler()
	s.Poll()

	// a restart: the directory is another one and the counter starts again. The first run wrote
	// 10 000 more before it ended, which only the slice knows.
	b.now = b.now.Add(time.Minute)
	if err := os.Rename(filepath.Join(b.root, "/sys/fs/cgroup/system.slice/a.service"), filepath.Join(b.root, "/sys/fs/cgroup/old")); err != nil {
		t.Fatal(err)
	}
	b.iostat("old", "")
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=2000\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=62000\n")
	s.Poll()
	v := s.View()
	if a := unit(v, "a.service"); a == nil || a.WrittenBytes != 52000 || !a.Running {
		t.Fatalf("after the restart: %+v", a)
	}
	if e := unit(v, RowEnded); e == nil || e.WrittenBytes != 10000 {
		t.Fatalf("ended after the restart: %+v", e)
	}

	// the same directory with a counter that went backwards is another run as well
	b.now = b.now.Add(time.Minute)
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=500\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=62500\n")
	s.Poll()
	if a := unit(s.View(), "a.service"); a == nil || a.WrittenBytes != 52500 {
		t.Fatalf("after the counter went back: %+v", a)
	}

	// the unit ends: its sum stays, not running
	b.now = b.now.Add(time.Minute)
	b.remove("/sys/fs/cgroup/system.slice/a.service")
	s.Poll()
	v = s.View()
	if a := unit(v, "a.service"); a == nil || a.WrittenBytes != 52500 || a.Running {
		t.Fatalf("after the end: %+v", a)
	}
	if e := unit(v, RowEnded); e == nil || e.WrittenBytes != 10000 {
		t.Fatalf("ended after the end: %+v", e)
	}
	// and it comes back
	b.now = b.now.Add(time.Minute)
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=100\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=62600\n")
	s.Poll()
	if a := unit(s.View(), "a.service"); a == nil || a.WrittenBytes != 52600 || !a.Running {
		t.Fatalf("after it came back: %+v", a)
	}
}

func TestStateFileSurvivesARestartNotABoot(t *testing.T) {
	b := newBox(t)
	b.diskstats(0, 0)
	b.enable()
	b.iostat("", "179:0 rbytes=0 wbytes=900\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=0\n")
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=0\n")
	file := filepath.Join(t.TempDir(), "io.json")
	s := b.sampler()
	s.StateFile = file
	s.Poll()
	b.now = b.now.Add(2 * time.Minute)
	b.iostat("", "179:0 rbytes=0 wbytes=2000\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=600\n")
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=600\n")
	s.Poll()
	b.now = b.now.Add(2 * time.Minute)
	b.remove("/sys/fs/cgroup/system.slice/a.service")
	s.Poll()
	since := *s.View().Since

	// occulited restarts: the unit's sum, the kernel's base and the start are kept
	s2 := b.sampler()
	s2.StateFile = file
	b.now = b.now.Add(time.Minute)
	s2.Poll()
	v := s2.View()
	if a := unit(v, "a.service"); a == nil || a.WrittenBytes != 600 || a.Running {
		t.Fatalf("after occulited's restart: %+v", v.Units)
	}
	if k := unit(v, RowKernel); k == nil || k.WrittenBytes != 500 {
		t.Fatalf("kernel after occulited's restart: %+v", k)
	}
	if !v.Since.Equal(since) {
		t.Errorf("since = %v, want %v", v.Since, since)
	}
	if a := unit(v, "a.service"); a.Day.Seconds != 300 || a.Day.WrittenBytes != 600 {
		t.Errorf("the day's window after the restart: %+v", a.Day)
	}

	// another boot: nothing of the file is taken
	b.write("/proc/sys/kernel/random/boot_id", "boot-2\n")
	s3 := b.sampler()
	s3.StateFile = file
	s3.Poll()
	if v := s3.View(); unit(v, "a.service") != nil || !v.Since.Equal(b.now) {
		t.Fatalf("after a boot: %+v", v)
	}
}

func TestSwitchedOffForgets(t *testing.T) {
	b := newBox(t)
	b.diskstats(100, 0)
	b.enable()
	b.iostat("", "179:0 rbytes=0 wbytes=900\n")
	b.iostat("system.slice", "179:0 rbytes=0 wbytes=300\n")
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=300\n")
	s := b.sampler()
	s.Poll()
	if v := s.View(); !v.Active || unit(v, "a.service") == nil {
		t.Fatalf("on: %+v", v)
	}
	b.write("/sys/fs/cgroup/cgroup.subtree_control", "cpu memory pids\n")
	b.now = b.now.Add(time.Minute)
	s.Poll()
	v := s.View()
	if v.Active || v.Since != nil || len(v.Units) != 0 || len(v.Disks) != 2 {
		t.Fatalf("off: %+v", v)
	}
	// on again: a new start, nothing of before
	b.enable()
	b.now = b.now.Add(time.Minute)
	b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes=20\n")
	s.Poll()
	v = s.View()
	if a := unit(v, "a.service"); a == nil || a.WrittenBytes != 20 || !v.Since.Equal(b.now) {
		t.Fatalf("on again: %+v", v)
	}
}

func TestWindowsFollowTheRings(t *testing.T) {
	b := newBox(t)
	b.enable()
	b.iostat("", "179:0 rbytes=0 wbytes=0\n")
	s := b.sampler()
	// three hours, a megabyte's sectors a minute
	for i := 0; i <= 180; i++ {
		b.diskstats(uint64(i)*2048, 0)
		b.iostat("system.slice/a.service", "179:0 rbytes=0 wbytes="+itoa(uint64(i)*1000)+"\n")
		b.iostat("system.slice", "179:0 rbytes=0 wbytes="+itoa(uint64(i)*1000)+"\n")
		s.Poll()
		if i < 180 {
			b.now = b.now.Add(time.Minute)
		}
	}
	v := s.View()
	card := v.Disks[0]
	if card.Hour != (Window{Seconds: 3600, WrittenBytes: 60 * 2048 * 512}) {
		t.Errorf("hour = %+v", card.Hour)
	}
	if card.Day != (Window{Seconds: 3 * 3600, WrittenBytes: 180 * 2048 * 512}) {
		t.Errorf("day = %+v", card.Day)
	}
	a := unit(v, "a.service")
	if a.Hour != (Window{3600, 60000}) || a.Day != (Window{3 * 3600, 180000}) {
		t.Errorf("unit windows = %+v %+v", a.Hour, a.Day)
	}
	if len(s.disksMin) != 61 || len(s.st.DisksHour) != 4 {
		t.Errorf("rings: %d minute, %d hour snapshots", len(s.disksMin), len(s.st.DisksHour))
	}
}

func TestParseIOStat(t *testing.T) {
	known := map[string]bool{"8:0": true, "179:0": true}
	got := parseIOStat("8:0 rbytes=94208 wbytes=31776768 rios=2 wios=77 dbytes=0 dios=0\n252:1 rbytes=1 wbytes=2\n179:0 \n7:7 \nbroken\n", known)
	if !reflect.DeepEqual(got, map[string]Counter{"8:0": {Read: 94208, Written: 31776768}}) {
		t.Errorf("got %+v", got)
	}
}

func TestSkippedDisk(t *testing.T) {
	for name, want := range map[string]bool{"loop0": true, "ram1": true, "zram0": true, "mtdblock2": true, "sda": false, "sdb1": false, "mmcblk0p3": false, "nvme0n1": false, "vda": false, "sr0": true} {
		if skippedDisk(name) != want {
			t.Errorf("skippedDisk(%q) = %v", name, !want)
		}
	}
}
