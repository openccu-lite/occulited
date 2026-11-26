package system

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// occulited task 35 (openccu-lite B-316): the check of a USB stick that was not unmounted cleanly.
// The fork's mount path (lite-usb-mount, occu-usb-mount@.service) reads the switch at each mount
// from USBStorageConfig - the line FSCK_AT_DIRTY_MOUNT=1, off when the file or the line is missing
// (the default, the maintainer's decision of 2026-11-11) - and runs fsck.fat -a / fsck.exfat -p
// before it mounts a FAT or exFAT stick whose dirty flag is set. What it found it writes to
// USBCheckDir/<kernel name>; the Storage page shows it, and Status warns of a stick that was not
// checked or whose check failed.

// USBStorageConfig is the fork's setting file (/etc/config/usbstorage on the userfs).
const USBStorageConfig = "/usr/local/etc/config/usbstorage"

// USBCheckDir holds one file per partition the mount found dirty (KEY=VALUE lines).
const USBCheckDir = "/run/lite-usb-check"

// The states a check record says.
const (
	USBCheckUnchecked = "unchecked" // dirty, and the check is off
	USBCheckClean     = "clean"     // checked, nothing to repair
	USBCheckRepaired  = "repaired"  // checked and repaired
	USBCheckFailed    = "failed"    // the check ended with an error; mounted anyway
)

// USBCheck is what the mount found on a partition that was not unmounted cleanly.
type USBCheck struct {
	State  string    `json:"state"`
	FSType string    `json:"fstype,omitempty"`
	Exit   *int      `json:"exit,omitempty"`
	At     time.Time `json:"at,omitzero"`
}

var usbFsckLineRe = regexp.MustCompile(`^[ \t]*FSCK_AT_DIRTY_MOUNT[ \t]*=[ \t]*(.*)$`)

// USBFsckAtDirtyMount says whether the check is switched on, read as the fork reads it: the last
// FSCK_AT_DIRTY_MOUNT line, quotes and blanks taken out, 1, yes, true or on.
func (r Root) USBFsckAtDirtyMount() bool {
	v := ""
	for _, line := range strings.Split(readFile(r.join(USBStorageConfig)), "\n") {
		if m := usbFsckLineRe.FindStringSubmatch(line); m != nil {
			v = strings.Map(func(c rune) rune {
				switch c {
				case '"', '\'', ' ', '\t', '\r':
					return -1
				}
				return c
			}, m[1])
		}
	}
	switch v {
	case "1", "yes", "true", "on":
		return true
	}
	return false
}

// SetUSBFsckAtDirtyMount writes the switch: every FSCK_AT_DIRTY_MOUNT line is replaced by one,
// other lines are kept. The file is root's (the helper writes it).
func (r Root) SetUSBFsckAtDirtyMount(on bool) error {
	var keep []string
	for _, line := range strings.Split(readFile(r.join(USBStorageConfig)), "\n") {
		if line == "" || usbFsckLineRe.MatchString(line) {
			continue
		}
		keep = append(keep, line)
	}
	if len(keep) == 0 {
		keep = append(keep, "# openccu-lite: USB sticks; written by occulited's Storage page, read by lite-usb-mount at each mount")
	}
	val := "0"
	if on {
		val = "1"
	}
	keep = append(keep, "FSCK_AT_DIRTY_MOUNT="+val)
	return Priv.WriteFile(r.join(USBStorageConfig), []byte(strings.Join(keep, "\n")+"\n"), 0o644)
}

var usbCheckNameRe = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// USBChecks reads the records, by kernel name (sda1); a record that is not one is left out.
func (r Root) USBChecks() map[string]USBCheck {
	out := map[string]USBCheck{}
	ents, err := os.ReadDir(r.join(USBCheckDir))
	if err != nil {
		return out
	}
	for _, e := range ents {
		if e.IsDir() || !usbCheckNameRe.MatchString(e.Name()) {
			continue
		}
		kv := readKV(r.join(USBCheckDir + "/" + e.Name()))
		c := USBCheck{State: kv["STATE"], FSType: kv["FSTYPE"]}
		switch c.State {
		case USBCheckUnchecked, USBCheckClean, USBCheckRepaired, USBCheckFailed:
		default:
			continue
		}
		if n, err := strconv.Atoi(kv["EXIT"]); err == nil && n >= 0 && n < 256 {
			c.Exit = &n
		}
		if s, err := strconv.ParseInt(kv["AT"], 10, 64); err == nil && s > 0 {
			c.At = time.Unix(s, 0).UTC()
		}
		out[e.Name()] = c
	}
	return out
}

// USBCheckNames is the records' names in order, for a stable warning.
func USBCheckNames(m map[string]USBCheck) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
