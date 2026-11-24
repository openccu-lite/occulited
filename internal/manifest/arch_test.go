package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// elf builds the head of an ELF file: class 64 or 32, little or big endian, e_type, e_machine,
// and one program header of type ptype right after the header.
func elf(class int, big bool, typ, machine uint16, ptype uint32) []byte {
	var bo binary.ByteOrder = binary.LittleEndian
	b := make([]byte, 256)
	copy(b, "\x7fELF")
	b[4], b[5] = byte(class/32), 1
	if big {
		bo, b[5] = binary.BigEndian, 2
	}
	bo.PutUint16(b[16:], typ)
	bo.PutUint16(b[18:], machine)
	if class == 64 {
		bo.PutUint64(b[32:], 64)
		bo.PutUint16(b[54:], 56)
		bo.PutUint16(b[56:], 1)
		bo.PutUint32(b[64:], ptype)
	} else {
		bo.PutUint32(b[28:], 52)
		bo.PutUint16(b[42:], 32)
		bo.PutUint16(b[44:], 1)
		bo.PutUint32(b[52:], ptype)
	}
	return b
}

const (
	etExec, etDyn    = 2, 3
	ptLoad, ptInterp = 1, 3
)

func TestElfProgram(t *testing.T) {
	for _, tc := range []struct {
		name string
		head []byte
		want string
	}{
		{"a static aarch64 program (Go)", elf(64, false, etExec, 183, ptLoad), "aarch64"},
		{"a static x86_64 program", elf(64, false, etExec, 62, ptLoad), "x86_64"},
		{"an armv7l program", elf(32, false, etExec, 40, ptLoad), "armv7l"},
		{"a position-independent aarch64 program", elf(64, false, etDyn, 183, ptInterp), "aarch64"},
		{"a 32-bit position-independent program", elf(32, false, etDyn, 3, ptInterp), "i386"},
		{"a shared library", elf(64, false, etDyn, 183, ptLoad), ""},
		{"a 32-bit shared library", elf(32, false, etDyn, 40, ptLoad), ""},
		{"an object file", elf(64, false, 1, 62, ptLoad), ""},
		{"a big-endian program of another machine", elf(32, true, etExec, 8, ptLoad), "elf-8"},
		{"a script", []byte("#!/bin/sh\necho " + strings.Repeat("x", 80)), ""},
		{"a short file", []byte("\x7fELF"), ""},
		{"a 64-bit header cut short", elf(64, false, etDyn, 183, ptInterp)[:60], ""},
		{"program headers beyond what was read", func() []byte {
			b := elf(64, false, etDyn, 183, ptInterp)
			binary.LittleEndian.PutUint64(b[32:], 1<<20)
			return b
		}(), ""},
		{"a program header size of nothing", func() []byte {
			b := elf(64, false, etDyn, 183, ptInterp)
			binary.LittleEndian.PutUint16(b[54:], 0)
			return b
		}(), ""},
	} {
		if got := elfProgram(tc.head); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

// pkg builds a package archive of the named files.
func pkg(t *testing.T, gz bool, files map[string][]byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	_ = tw.WriteHeader(&tar.Header{Name: "./bin/", Typeflag: tar.TypeDir, Mode: 0o755})
	_ = tw.WriteHeader(&tar.Header{Name: "./link", Typeflag: tar.TypeSymlink, Linkname: "bin/daemon"})
	for name, b := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(b))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if !gz {
		return raw.Bytes()
	}
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	_, _ = zw.Write(raw.Bytes())
	_ = zw.Close()
	return out.Bytes()
}

func TestProgramArchs(t *testing.T) {
	script := []byte("#!/bin/sh\n" + strings.Repeat("# update_script\n", 8))
	big := append(elf(64, false, etExec, 183, ptLoad), bytes.Repeat([]byte{0}, 3*elfHead)...)
	for _, tc := range []struct {
		name  string
		gz    bool
		files map[string][]byte
		want  string
	}{
		{"scripts only", true, map[string][]byte{"./update_script": script, "./rc.d/x": script}, ""},
		{"one aarch64 daemon", true, map[string][]byte{"./update_script": script, "./bin/daemon": big}, "aarch64"},
		{"a plain tar", false, map[string][]byte{"./bin/daemon": elf(64, false, etExec, 62, ptLoad)}, "x86_64"},
		{"every architecture in one package", true, map[string][]byte{"a": elf(64, false, etExec, 62, ptLoad), "b": elf(64, false, etExec, 183, ptLoad), "c": elf(32, false, etExec, 40, ptLoad)}, "aarch64,armv7l,x86_64"},
		{"libraries are not programs", true, map[string][]byte{"./lib/x.so": elf(64, false, etDyn, 183, ptLoad), "./x.node": elf(64, false, etDyn, 62, ptLoad)}, ""},
	} {
		got, err := ProgramArchs(bytes.NewReader(pkg(t, tc.gz, tc.files)))
		if err != nil || strings.Join(got, ",") != tc.want {
			t.Errorf("%s: %v %v, want %s", tc.name, got, err, tc.want)
		}
	}
	// not an archive, a broken gzip, an empty stream
	if _, err := ProgramArchs(strings.NewReader("this is no tar archive, and long enough to be read as a header" + strings.Repeat(".", 600))); err == nil {
		t.Error("not an archive: no error")
	}
	if _, err := ProgramArchs(bytes.NewReader([]byte{0x1f, 0x8b, 0, 0})); err == nil {
		t.Error("a broken gzip: no error")
	}
	if got, err := ProgramArchs(strings.NewReader("")); err != nil || len(got) != 0 {
		t.Errorf("an empty stream: %v %v", got, err)
	}
	truncated := pkg(t, false, map[string][]byte{"./bin/daemon": big})
	if _, err := ProgramArchs(bytes.NewReader(truncated[:1024+100])); err == nil {
		t.Error("a truncated archive: no error")
	}
	if _, err := ProgramArchs(errReader{}); err == nil {
		t.Error("a failing reader: no error")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "p.tar.gz")
	if err := os.WriteFile(path, pkg(t, true, map[string][]byte{"./bin/daemon": big}), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ProgramArchsOfFile(path); err != nil || strings.Join(got, ",") != "aarch64" {
		t.Errorf("from a file: %v %v", got, err)
	}
	if _, err := ProgramArchsOfFile(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing file: no error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("broken") }

func TestCheckArch(t *testing.T) {
	battery := &Manifest{ID: "battery", Version: "1.5.5", Requires: Requires{Architectures: []string{"armv7l", "aarch64", "x86_64"}},
		Release: &Release{GitHub: "openccu-lite/ccu-addon-battery", Asset: "battery-{arch}-{version}.tar.gz"}}
	for _, tc := range []struct {
		name     string
		m        *Manifest
		arch     string
		programs []string
		want     string // "" = accepted
	}{
		{"the right package", battery, "x86_64", []string{"x86_64"}, ""},
		{"the wrong package (openccu-lite-addons B-26)", battery, "x86_64", []string{"aarch64"}, "This package of battery is for aarch64; this system is x86_64. Take the package for x86_64: battery-x86_64-1.5.5.tar.gz. Nothing was installed."},
		{"an armv7l package runs on aarch64", battery, "aarch64", []string{"armv7l"}, ""},
		{"an i386 package runs on x86_64", battery, "x86_64", []string{"i386"}, ""},
		{"an aarch64 package does not run on armv7l", battery, "armv7l", []string{"aarch64"}, "This package of battery is for aarch64; this system is armv7l. Take the package for armv7l: battery-armv7l-1.5.5.tar.gz. Nothing was installed."},
		{"scripts only", battery, "x86_64", nil, ""},
		{"all architectures in one package", battery, "x86_64", []string{"aarch64", "armv7l", "x86_64"}, ""},
		{"no manifest, the right programs", nil, "aarch64", []string{"aarch64"}, ""},
		{"no manifest, the wrong programs", nil, "aarch64", []string{"x86_64"}, "This package of redmatic is for x86_64; this system is aarch64. Take the package for aarch64, where the addon has one. Nothing was installed."},
		{"a system this code does not know", battery, "riscv64", []string{"aarch64"}, "This package of battery is for armv7l, aarch64, x86_64; this system is riscv64. The addon has no package for riscv64. Nothing was installed."},
		{"an unknown system and no list", nil, "riscv64", []string{"aarch64"}, ""},
		{"the manifest's list lacks the system", &Manifest{ID: "x", Requires: Requires{Architectures: []string{"aarch64"}}}, "x86_64", nil, "This package of x is for aarch64; this system is x86_64. The addon has no package for x86_64. Nothing was installed."},
		{"any", &Manifest{ID: "x", Requires: Requires{Architectures: []string{"any"}}}, "x86_64", nil, ""},
		{"a pattern per architecture", &Manifest{ID: "x", Version: "2", Release: &Release{Assets: map[string]string{"x86_64": "x-amd64-{version}.tgz"}}}, "x86_64", []string{"aarch64"}, "This package of x is for aarch64; this system is x86_64. Take the package for x86_64: x-amd64-2.tgz. Nothing was installed."},
		{"one file for every architecture", &Manifest{ID: "x", Version: "2", Release: &Release{Asset: "x-{version}.tar.gz"}}, "x86_64", []string{"aarch64"}, "This package of x is for aarch64; this system is x86_64. Take the package for x86_64, where the addon has one. Nothing was installed."},
		{"a pattern with a version the manifest does not name", &Manifest{ID: "x", Release: &Release{Asset: "x-{arch}-{version}.tar.gz"}}, "x86_64", []string{"aarch64"}, "This package of x is for aarch64; this system is x86_64. Take the package for x86_64, where the addon has one. Nothing was installed."},
		{"a manifest without a release", &Manifest{ID: "x"}, "x86_64", []string{"aarch64"}, "This package of x is for aarch64; this system is x86_64. Take the package for x86_64, where the addon has one. Nothing was installed."},
	} {
		err := tc.m.CheckArch(tc.arch, tc.programs, "redmatic")
		got := ""
		if err != nil {
			got = err.Error()
			var we *WrongArchError
			if !errors.As(err, &we) || we.System != tc.arch {
				t.Errorf("%s: not a WrongArchError for %s: %v", tc.name, tc.arch, err)
			}
		}
		if got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.name, got, tc.want)
		}
	}
	// an addon nobody named
	if err := (*Manifest)(nil).CheckArch("x86_64", []string{"aarch64"}, ""); err == nil || !strings.HasPrefix(err.Error(), "This package is for aarch64;") {
		t.Errorf("without an id: %v", err)
	}
}
