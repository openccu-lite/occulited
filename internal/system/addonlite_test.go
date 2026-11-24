package system

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/hobbyquaker/occulited/internal/manifest"
)

func litePackage(t *testing.T, min string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	body := `{"format":1,"id":"hmm","name":"HMM","requires":{"lite":"` + min + `"}}`
	if err := tw.WriteHeader(&tar.Header{Name: manifest.FileName, Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// occulited task 134: a package for a newer system is refused before the installer runs.
func TestInstallRefusedBelowRequiresLite(t *testing.T) {
	rig := newSettleRig(t, failedStartUpdate)
	if err := os.WriteFile(rig.root.join("/VERSION"), []byte("VERSION=3.89.11\nLITE=1.1.0-dev.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := rig.a.Install(context.Background(), bytes.NewReader(litePackage(t, "1.1.0-dev.6")))
	var e *manifest.LiteTooOldError
	if !errors.As(err, &e) || res != nil {
		t.Fatalf("res %v, err %v", res, err)
	}
	if _, serr := os.Stat(rig.root.join("/tmp/installer-ran")); serr == nil {
		t.Fatal("the installer ran")
	}
}

// archPackage is a package with a manifest for battery and one file, a program's ELF head for
// machine (62 x86_64, 183 aarch64) or, for 0, a script.
func archPackage(t *testing.T, machine uint16) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	prog := []byte("#!/bin/sh\n# a daemon that is a script, long enough to be looked at as a program's head\n")
	if machine != 0 {
		prog = make([]byte, 128)
		copy(prog, "\x7fELF\x02\x01")
		prog[16], prog[18], prog[19] = 2, byte(machine), byte(machine>>8)
	}
	for name, body := range map[string][]byte{
		manifest.FileName:   []byte(`{"format":1,"id":"battery","version":"1.5.5","name":"Battery","requires":{"architectures":["armv7l","aarch64","x86_64"]},"release":{"github":"openccu-lite/ccu-addon-battery","asset":"battery-{arch}-{version}.tar.gz"}}`),
		"./bin/battery":     prog,
		"./update_script":   []byte("#!/bin/sh\nexit 0\n"),
		"./rc.d/battery.sh": []byte("#!/bin/sh\n"),
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// occulited B-167: a package whose programs are for another architecture is refused before the
// installer runs, with both architectures and the file to take; the right one, a 32-bit one of
// the same family and a package of scripts go to the installer.
func TestInstallRefusedForAnotherArchitecture(t *testing.T) {
	ran := func(rig *settleRig) bool {
		_, err := os.Stat(rig.root.join("/tmp/installer-ran"))
		return err == nil
	}
	rig := newSettleRig(t, failedStartUpdate)
	rig.a.Arch = "x86_64"
	res, err := rig.a.Install(context.Background(), bytes.NewReader(archPackage(t, 183)))
	var e *manifest.WrongArchError
	if !errors.As(err, &e) || res != nil {
		t.Fatalf("res %v, err %v", res, err)
	}
	if want := "This package of battery is for aarch64; this system is x86_64. Take the package for x86_64: battery-x86_64-1.5.5.tar.gz. Nothing was installed."; err.Error() != want {
		t.Errorf("message:\n got %s\nwant %s", err, want)
	}
	if ran(rig) {
		t.Fatal("the installer ran")
	}
	// the manifest's list lacks the system
	rig.a.Arch = "riscv64"
	if _, err := rig.a.Install(context.Background(), bytes.NewReader(archPackage(t, 0))); !errors.As(err, &e) || !e.NoPackage || ran(rig) {
		t.Fatalf("a system the manifest does not list: %v", err)
	}
	for _, tc := range []struct {
		name    string
		arch    string
		machine uint16
	}{
		{"the right package", "x86_64", 62},
		{"a 32-bit package of the family", "aarch64", 40},
		{"a package of scripts", "aarch64", 0},
	} {
		rig := newSettleRig(t, failedStartUpdate)
		rig.a.Arch = tc.arch
		if _, err := rig.a.Install(context.Background(), bytes.NewReader(archPackage(t, tc.machine))); errors.As(err, &e) {
			t.Errorf("%s: refused: %v", tc.name, err)
		}
		if !ran(rig) {
			t.Errorf("%s: the installer did not run", tc.name)
		}
	}
	// without a set architecture it is this binary's own
	if (&SystemdAddons{}).arch() != MachineName() || MachineName() == "" {
		t.Error("the default architecture")
	}
}
