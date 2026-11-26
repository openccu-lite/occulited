package priv

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// occulited B-186: rpi_volt's lines of the ring buffer - exactly `dmesg -r`, nothing in the
// request, nothing of the kernel log but rpi_volt's lines in the answer, a missing dmesg "not available"
func TestVoltageLogOperation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "dmesg")
	body := "#!/bin/sh\necho \"<6>[    0.000000] Booting Linux on physical CPU 0x0 args:$*\"\n" +
		"echo '<2>[    9.535987] hwmon hwmon1: Undervoltage detected!'\n" +
		"echo '<6>[   10.100000] usb 1-1.3: new full-speed USB device'\n" +
		"echo '<6>[   11.552027] hwmon hwmon1: Voltage normalised'\n" +
		"echo '<4>[   12.000000] hwmon hwmon1: Under-voltage detected! (0x00050005)'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := dmesgPaths
	dmesgPaths = []string{filepath.Join(dir, "missing"), script}
	t.Cleanup(func() { dmesgPaths = old })
	sock := filepath.Join(t.TempDir(), "h.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var logged []string
	srv := &Server{Policy: DefaultPolicy("/", "/usr/local/etc/occulite"), Ops: Local{}, Log: func(f string, a ...any) { logged = append(logged, f) }}
	go func() { _ = srv.Serve(ctx, l) }()
	c := Client{Socket: sock}
	r, err := c.VoltageLog(ctx)
	want := "<2>[    9.535987] hwmon hwmon1: Undervoltage detected!\n<6>[   11.552027] hwmon hwmon1: Voltage normalised\n<4>[   12.000000] hwmon hwmon1: Under-voltage detected! (0x00050005)\n"
	if err != nil || string(r.Stdout) != want {
		t.Fatalf("%q %v", r.Stdout, err)
	}
	for _, req := range []request{{Op: opVoltageLog, Args: []string{"-c"}}, {Op: opVoltageLog, Name: "/bin/sh"}, {Op: opVoltageLog, Path: "/dev/kmsg"}} {
		if _, err := c.call(ctx, req); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("%+v: %v", req, err)
		}
	}
	if !strings.Contains(strings.Join(logged, "\n"), "voltage log") {
		t.Errorf("the refusals were not logged: %v", logged)
	}
	dmesgPaths = []string{filepath.Join(dir, "missing")}
	if _, err := c.VoltageLog(ctx); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("missing: %v", err)
	}
	if got := VoltageLogArgs(); len(got) != 1 || got[0] != "-r" {
		t.Errorf("%v", got)
	}
}
