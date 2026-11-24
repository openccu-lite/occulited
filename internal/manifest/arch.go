package manifest

import (
	"archive/tar"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
)

// occulited B-167: a package for another architecture is refused before any of its code runs.
//
// Two things say what a package is for. Its manifest's requires.architectures - but a project
// ships the same manifest in every package, listing all the architectures it is built for, so the
// list only refuses a project that has no package for this system at all. And the programs in
// the archive themselves: an ELF executable names its machine in its header. A package that
// holds programs, none of which this system can run, is the wrong file - ccu-addon-battery's
// aarch64 package on an x86_64 system installed and then died with `syntax error: unexpected ")"`
// (openccu-lite-addons B-26), and the user saw "exit 13".

// The ELF machines an addon package is built for (e_machine), by their uname -m names.
var elfMachines = map[uint16]string{
	3:   "i386",
	40:  "armv7l",
	62:  "x86_64",
	183: "aarch64",
}

// runsOn are the machines a system runs: its own, and the 32-bit one of its family, which the
// image carries libraries for (/lib32).
var runsOn = map[string][]string{
	"x86_64":  {"x86_64", "i386"},
	"aarch64": {"aarch64", "armv7l"},
	"armv7l":  {"armv7l"},
}

// elfHead is how much of a file is read: the ELF header and, where the program headers follow
// it as they do in every linker's output, those.
const elfHead = 4096

// elfProgram reports the machine of an ELF program: an executable (ET_EXEC), or a
// position-independent one (ET_DYN with an interpreter - what a shared library or a Node.js
// native module does not have; those are data to the package's own programs and never refuse a
// package). "" for anything else, a machine this list does not know as "elf-<number>".
func elfProgram(head []byte) string {
	if len(head) < 52 || string(head[:4]) != "\x7fELF" {
		return ""
	}
	var bo binary.ByteOrder = binary.LittleEndian
	if head[5] == 2 {
		bo = binary.BigEndian
	}
	typ, machine := bo.Uint16(head[16:]), bo.Uint16(head[18:])
	name := elfMachines[machine]
	if name == "" {
		name = fmt.Sprintf("elf-%d", machine)
	}
	switch typ {
	case 2: // ET_EXEC
		return name
	case 3: // ET_DYN: a program when it asks for an interpreter (PT_INTERP)
	default:
		return ""
	}
	var phoff uint64
	var phentsize, phnum int
	if head[4] == 2 { // ELFCLASS64
		if len(head) < 64 {
			return ""
		}
		phoff, phentsize, phnum = bo.Uint64(head[32:]), int(bo.Uint16(head[54:])), int(bo.Uint16(head[56:]))
	} else {
		phoff, phentsize, phnum = uint64(bo.Uint32(head[28:])), int(bo.Uint16(head[42:])), int(bo.Uint16(head[44:]))
	}
	if phentsize < 4 {
		return ""
	}
	for i := 0; i < phnum; i++ {
		at := phoff + uint64(i)*uint64(phentsize)
		if at+4 > uint64(len(head)) {
			break
		}
		if bo.Uint32(head[at:]) == 3 { // PT_INTERP
			return name
		}
	}
	return ""
}

// ProgramArchs lists the machines of the ELF programs in a package archive (a gzipped or plain
// tar), sorted: empty for a package of scripts. The archive is read once, the first elfHead
// bytes of each regular file.
func ProgramArchs(r io.Reader) ([]string, error) {
	br := newPeekReader(r)
	head, err := br.Peek(2)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	var tr *tar.Reader
	if len(head) == 2 && head[0] == 0x1f && head[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr = tar.NewReader(gz)
	} else {
		tr = tar.NewReader(br)
	}
	seen := map[string]bool{}
	buf := make([]byte, elfHead)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || h.Size < 52 {
			continue
		}
		n, err := io.ReadFull(tr, buf)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if m := elfProgram(buf[:n]); m != "" {
			seen[m] = true
		}
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

// ProgramArchsOfFile is ProgramArchs on a file.
func ProgramArchsOfFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ProgramArchs(f)
}

// WrongArchError refuses a package that is not for this system's architecture: nothing was
// unpacked. Package is what the package is for - its programs' machines, or its manifest's list.
type WrongArchError struct {
	ID      string
	Package []string
	System  string
	// Asset is the file to take instead, from the manifest's release pattern; "" when unknown.
	Asset string
	// NoPackage: the manifest's own list lacks the system - the addon has no package for it.
	NoPackage bool
}

func (e *WrongArchError) Error() string {
	what := "This package"
	if e.ID != "" {
		what = "This package of " + e.ID
	}
	s := fmt.Sprintf("%s is for %s; this system is %s.", what, strings.Join(e.Package, ", "), e.System)
	switch {
	case e.NoPackage:
		s += fmt.Sprintf(" The addon has no package for %s.", e.System)
	case e.Asset != "":
		s += fmt.Sprintf(" Take the package for %s: %s.", e.System, e.Asset)
	default:
		s += fmt.Sprintf(" Take the package for %s, where the addon has one.", e.System)
	}
	return s + " Nothing was installed."
}

// assetFor is the release asset's name for arch, from the manifest's pattern; "" without one.
func (m *Manifest) assetFor(arch string) string {
	if m == nil || m.Release == nil {
		return ""
	}
	pattern := m.Release.Assets[arch]
	if pattern == "" {
		// one pattern for every architecture names it, or it is one file for all of them
		if pattern = m.Release.Asset; !strings.Contains(pattern, "{arch}") {
			return ""
		}
	}
	if strings.Contains(pattern, "{version}") && m.Version == "" {
		return ""
	}
	return strings.NewReplacer("{arch}", arch, "{version}", m.Version).Replace(pattern)
}

// CheckArch refuses a package that is not for a system of arch (uname -m). programs are the
// machines of the package's ELF programs (ProgramArchs); id names the addon where the manifest
// does not. A manifest without requires.architectures and a package without programs are for
// every system, and so is everything on an architecture this code does not know.
func (m *Manifest) CheckArch(arch string, programs []string, id string) error {
	if m != nil && m.ID != "" {
		id = m.ID
	}
	if !m.SupportsArch(arch) {
		return &WrongArchError{ID: id, Package: slices.Clone(m.Requires.Architectures), System: arch, NoPackage: true}
	}
	runs, known := runsOn[arch]
	if !known || len(programs) == 0 {
		return nil
	}
	for _, p := range programs {
		if slices.Contains(runs, p) {
			return nil
		}
	}
	return &WrongArchError{ID: id, Package: slices.Clone(programs), System: arch, Asset: m.assetFor(arch)}
}
