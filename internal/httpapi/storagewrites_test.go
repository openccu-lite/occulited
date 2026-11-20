package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/iowatch"
	"github.com/hobbyquaker/occulited/internal/system"
)

// occulited task 159: the disks' totals without the recording, the switch through the stored
// file and the drop-in, the units once the kernel counts them, and the hint on a busy card.
func TestStorageWritesRoutes(t *testing.T) {
	r := fakeRoot(t)
	root := string(r)
	put := func(path, text string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	card := filepath.Join(root, "sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0")
	_ = os.MkdirAll(filepath.Join(card, "mmcblk0p3"), 0o755)
	put("sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0/size", "62521344\n")
	put("sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0/dev", "179:0\n")
	put("sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3/size", "62000000\n")
	put("sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3/dev", "179:3\n")
	put("sys/devices/platform/mmc0/mmc0:0001/type", "SD\n")
	put("sys/devices/platform/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3/partition", "3\n")
	_ = os.MkdirAll(filepath.Join(root, "sys/block"), 0o755)
	_ = os.Symlink(card, filepath.Join(root, "sys/block/mmcblk0"))
	_ = os.Symlink(filepath.Dir(filepath.Dir(card)), filepath.Join(card, "device"))
	put("proc/self/mountinfo", "30 1 179:3 / /usr/local rw,noatime shared:1 - ext4 /dev/mmcblk0p3 rw\n")
	put("proc/uptime", "86400.00 1.00\n")
	put("proc/sys/kernel/random/boot_id", "b\n")
	put("sys/fs/cgroup/cgroup.controllers", "cpu io memory pids\n")
	put("sys/fs/cgroup/cgroup.subtree_control", "cpu memory pids\n")
	diskstats := func(sectors int) {
		put("proc/diskstats", fmt.Sprintf(" 179 0 mmcblk0 1 0 8 0 1 0 %d 0 0 0 0\n 179 3 mmcblk0p3 1 0 8 0 1 0 %d 0 0 0 0\n", sectors, sectors))
	}
	diskstats(0)

	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	dropIn := filepath.Join(dir, "50.conf")
	acct := &system.IOAccounting{File: filepath.Join(dir, "io-accounting"), DropIn: dropIn,
		Set: func(_ context.Context, on bool) error {
			if on {
				// what systemd does at the reload
				put("sys/fs/cgroup/cgroup.subtree_control", "cpu io memory pids\n")
				return os.WriteFile(dropIn, nil, 0o644)
			}
			put("sys/fs/cgroup/cgroup.subtree_control", "cpu memory pids\n")
			return os.Remove(dropIn)
		}}
	watch := &iowatch.Sampler{Root: root, Now: func() time.Time { return now }, Enabled: acct.Enabled}
	mux := http.NewServeMux()
	(&SystemAPI{Root: r, IOWatch: watch, IOAccounting: acct}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	watch.Poll()
	st, out, _ := do(t, srv, "GET", "/api/system/v1/storage/writes", "", nil)
	rec, _ := out["recording"].(map[string]any)
	if st != 200 || rec["supported"] != true || rec["enabled"] != false || rec["active"] != false || out["uptime_s"] != float64(86400) {
		t.Fatalf("off: %d %v", st, out)
	}
	if units := out["units"].([]any); len(units) != 0 {
		t.Errorf("units while off: %v", units)
	}

	if st, _, _ := do(t, srv, "PUT", "/api/system/v1/storage/writes/recording", `{}`, nil); st != 422 {
		t.Errorf("without enabled: %d", st)
	}
	put("sys/fs/cgroup/io.stat", "179:0 rbytes=0 wbytes=1000\n")
	put("sys/fs/cgroup/system.slice/io.stat", "")
	put("sys/fs/cgroup/system.slice/addon-redmatic.service/io.stat", "")
	st, out, _ = do(t, srv, "PUT", "/api/system/v1/storage/writes/recording", `{"enabled":true}`, nil)
	if st != 200 || out["enabled"] != true || out["active"] != true || out["since"] == nil {
		t.Fatalf("on: %d %v", st, out)
	}
	if st, out, _ = do(t, srv, "GET", "/api/system/v1/storage/writes/recording", "", nil); st != 200 || out["enabled"] != true {
		t.Errorf("the switch read back: %d %v", st, out)
	}

	// an hour later: 64 MiB on the card, 48 of them the addon's
	now = now.Add(time.Hour)
	diskstats(64 << 11)
	put("sys/fs/cgroup/io.stat", fmt.Sprintf("179:0 rbytes=0 wbytes=%d\n", 1000+64<<20))
	put("sys/fs/cgroup/system.slice/io.stat", fmt.Sprintf("179:0 rbytes=0 wbytes=%d\n", 48<<20))
	put("sys/fs/cgroup/system.slice/addon-redmatic.service/io.stat", fmt.Sprintf("179:0 rbytes=0 wbytes=%d\n", 48<<20))
	watch.Poll()
	_, out, _ = do(t, srv, "GET", "/api/system/v1/storage/writes", "", nil)
	disks := out["disks"].([]any)
	if len(disks) != 1 {
		t.Fatalf("disks: %v", disks)
	}
	d := disks[0].(map[string]any)
	if d["name"] != "mmcblk0" || d["kind"] != "sd" || d["written_bytes"] != float64(64<<20) {
		t.Errorf("disk: %v", d)
	}
	if p := d["partitions"].([]any)[0].(map[string]any); p["name"] != "mmcblk0p3" || p["mount"] != "/usr/local" || p["role"] != "userfs" {
		t.Errorf("partition: %v", p)
	}
	hint, _ := d["hint"].(map[string]any)
	if hint == nil || hint["bytes_per_hour"] != float64(64<<20) || hint["limit_bytes_per_hour"] != float64(WriteHintBytesPerHour) {
		t.Fatalf("hint: %v", d["hint"])
	}
	if top := hint["top_units"].([]any); len(top) != 2 || top[0] != "addon-redmatic.service" || top[1] != iowatch.RowKernel {
		t.Errorf("top units: %v", top)
	}
	units := out["units"].([]any)
	if u := units[0].(map[string]any); u["unit"] != "addon-redmatic.service" || u["written_bytes"] != float64(48<<20) || u["running"] != true {
		t.Errorf("units: %v", units)
	}

	st, out, _ = do(t, srv, "PUT", "/api/system/v1/storage/writes/recording", `{"enabled":false}`, nil)
	if st != 200 || out["enabled"] != false || out["active"] != false {
		t.Fatalf("off again: %d %v", st, out)
	}
	if _, out, _ = do(t, srv, "GET", "/api/system/v1/storage/writes", "", nil); len(out["units"].([]any)) != 0 || len(out["disks"].([]any)) != 1 {
		t.Errorf("after off: %v", out)
	}

	// a kernel without the io controller: the switch is refused
	put("sys/fs/cgroup/cgroup.controllers", "cpu memory pids\n")
	if st, out, _ = do(t, srv, "PUT", "/api/system/v1/storage/writes/recording", `{"enabled":true}`, nil); st != 409 || out["error"] != "unsupported" {
		t.Errorf("unsupported: %d %v", st, out)
	}
}

// Without systemd there is no switch: the reads answer, the write is 501.
func TestStorageWritesWithoutSystemd(t *testing.T) {
	mux := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t)}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	st, out, _ := do(t, srv, "GET", "/api/system/v1/storage/writes", "", nil)
	if rec, _ := out["recording"].(map[string]any); st != 200 || rec["supported"] != false || rec["enabled"] != false {
		t.Errorf("read: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "PUT", "/api/system/v1/storage/writes/recording", `{"enabled":true}`, nil); st != 501 {
		t.Errorf("write: %d", st)
	}
}
