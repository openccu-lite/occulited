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

// occulited task 148: the firmware's throttling word - exactly `vcgencmd get_throttled`, nothing
// in the request, a missing vcgencmd "not available"
func TestThrottledOperation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "vcgencmd")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = measure_clock ]; then [ -e \"$0.noclock\" ] && exit 1; echo \"frequency(48)=1200000000\"; exit 0; fi\necho \"args:$*\"\necho throttled=0x50005\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := vcgencmdPath
	vcgencmdPath = script
	t.Cleanup(func() { vcgencmdPath = old })
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
	r, err := c.Throttled(ctx)
	// task 177: the measured clock after the word
	if err != nil || string(r.Stdout) != "args:get_throttled\nthrottled=0x50005\nfrequency(48)=1200000000\n" {
		t.Fatalf("%q %v", r.Stdout, err)
	}
	// a clock that cannot be read leaves the word alone
	if err := os.WriteFile(script+".noclock", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := c.Throttled(ctx); err != nil || string(r.Stdout) != "args:get_throttled\nthrottled=0x50005\n" {
		t.Fatalf("without the clock: %q %v", r.Stdout, err)
	}
	for _, req := range []request{{Op: opThrottled, Args: []string{"otp_dump"}}, {Op: opThrottled, Name: "/bin/sh"}, {Op: opThrottled, Path: "/dev/vcio"}} {
		if _, err := c.call(ctx, req); err == nil || !errors.Is(err, ErrRefused) {
			t.Errorf("%+v: %v", req, err)
		}
	}
	if !strings.Contains(strings.Join(logged, "\n"), "throttling") {
		t.Errorf("the refusals were not logged: %v", logged)
	}
	vcgencmdPath = filepath.Join(dir, "missing")
	if _, err := c.Throttled(ctx); !errors.Is(err, ErrNotAvailable) {
		t.Errorf("missing: %v", err)
	}
	if got := ThrottledArgs(); len(got) != 1 || got[0] != "get_throttled" {
		t.Errorf("%v", got)
	}
}
