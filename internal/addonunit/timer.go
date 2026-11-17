package addonunit

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/hobbyquaker/occulited/internal/manifest"
)

// An addon's own timers (occulited task 154). An addon whose token holds the scope timers:addon
// keeps timers local-<id>-<name>. Such a timer is two units like every own timer, and they are
// root's files in /run/systemd/system - so the addon decides as little of them as the job needs:
//
//   - the .service is rendered here, from the addon's policy and its manifest. The addon's user
//     and groups, the confinement of its own unit without any capability, limits, and ExecStart a
//     program in the addon's root-owned runtime.bin directory with the timer's name as its last
//     argument. Nothing of it is the addon's text but the description, which is one cleaned line.
//   - the .timer is the addon's text, held to a closed list of directives (CheckTimerText): a
//     schedule, and nothing that names another unit to start.
//
// The package stays pure: the daemon reads the policy and the manifest and builds a TimerService,
// Validate refuses what must never reach a unit, Render writes the text.

// The limits a timer's service runs under, and what an addon may ask for inside them.
const (
	TimerTimeoutDefault = 6 * 3600 // seconds: TimeoutStartSec of a job
	TimerTimeoutMin     = 60
	TimerTimeoutMax     = 24 * 3600
	TimerMemoryDefault  = 300 // MiB: MemoryMax; MemoryHigh is two thirds of it
	TimerMemoryMin      = 64
	TimerMemoryMax      = 1024
	// TimerDescriptionMax is the longest description, in bytes.
	TimerDescriptionMax = 120
	// TimerReadMax is how many other addons one timer may name.
	TimerReadMax = 8
)

// timerNameRe is a timer's name as the addon gives it: the part after local-<id>-.
var timerNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// ValidTimerName is the rule for the name an addon gives one of its timers: lower-case letters,
// digits and dashes, starting with a letter or a digit. With the addon's id in front it must still
// be an own unit's name (priv.ValidLocalUnitName, 32 characters) - the caller checks that.
func ValidTimerName(name string) bool { return timerNameRe.MatchString(name) }

// TimerService is what the service of an addon's timer says.
type TimerService struct {
	// DropIn is the addon's policy as its own unit has it (the daemon's dropInOf): the mode, the
	// uid, the groups, the paths. Its capabilities and its restore hook are not used.
	DropIn DropIn
	// AddonName is the manifest's name, for the description.
	AddonName string
	// Name is the timer's name, the program's last argument.
	Name string
	// Bin is the manifest's runtime.bin, Exec and Args its runtime.timers.
	Bin  string
	Exec string
	Args []string
	// Description is the addon's line about the timer, cleaned by CleanTimerDescription.
	Description string
	// Read are the other addons whose data the run reads (the manifest's read_addon_data): each
	// becomes the supplementary group addon-<id>. The daemon has checked that each is installed,
	// confined and not this addon.
	Read []string
	// TimeoutSec and MemoryMB are the limits; 0 is the default.
	TimeoutSec int
	MemoryMB   int
}

// CleanTimerDescription makes one harmless line of an addon's description: control characters
// become spaces, runs of white space one space, the ends are trimmed, a trailing backslash (a unit
// file's line continuation) goes, and it is cut at TimerDescriptionMax bytes on a rune boundary.
func CleanTimerDescription(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	for len(s) > TimerDescriptionMax {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return strings.TrimRight(s, `\ `)
}

// Validate refuses what must not reach the unit: a name, a program or an argument of another
// shape than the manifest's rules allow, a description that is more than one cleaned line, a
// group that is not another addon's own, limits outside the bounds, and a confined policy the
// drop-in's own check refuses.
func (t TimerService) Validate() error {
	if !IDRe.MatchString(t.DropIn.ID) {
		return fmt.Errorf("addon id %q", t.DropIn.ID)
	}
	if !ValidTimerName(t.Name) {
		return fmt.Errorf("timer name %q", t.Name)
	}
	if err := manifest.ValidBin(t.Bin); err != nil {
		return err
	}
	if err := (&manifest.Timers{Exec: t.Exec, Args: t.Args}).Validate(); err != nil {
		return err
	}
	if t.Description != CleanTimerDescription(t.Description) {
		return errors.New("the description is not one clean line")
	}
	if strings.ContainsAny(t.AddonName, "\x00\r\n") {
		return errors.New("the addon's name is not one line")
	}
	if len(t.Read) > TimerReadMax {
		return fmt.Errorf("more than %d addons to read", TimerReadMax)
	}
	for i, id := range t.Read {
		if !IDRe.MatchString(id) || id == t.DropIn.ID || !groupRe.MatchString("addon-"+id) {
			return fmt.Errorf("addon to read %q", id)
		}
		for _, other := range t.Read[:i] {
			if other == id {
				return fmt.Errorf("addon to read %s twice", id)
			}
		}
	}
	if t.TimeoutSec != 0 && (t.TimeoutSec < TimerTimeoutMin || t.TimeoutSec > TimerTimeoutMax) {
		return fmt.Errorf("a timeout is %d to %d seconds", TimerTimeoutMin, TimerTimeoutMax)
	}
	if t.MemoryMB != 0 && (t.MemoryMB < TimerMemoryMin || t.MemoryMB > TimerMemoryMax) {
		return fmt.Errorf("a memory limit is %d to %d MiB", TimerMemoryMin, TimerMemoryMax)
	}
	d := t.DropIn
	d.RestoreHook = ""
	return d.Validate()
}

// TimerServiceMark is the first line of a rendered service: how the pages and the replay tell an
// addon's timer service from a person's.
const TimerServiceMark = "# openccu-lite: the service of an addon's timer, rendered by the system."

// unitText makes a value safe after "Key=" in a unit file: "%" is systemd's specifier character.
func unitText(s string) string { return strings.ReplaceAll(s, "%", "%%") }

// Render is the service's text. A confined addon's job runs as the addon's user with the
// confinement of the addon's own unit and no capability at all; a root addon's job runs as root,
// as that addon does by the user's choice.
func (t TimerService) Render() string {
	id := t.DropIn.ID
	var b strings.Builder
	b.WriteString(TimerServiceMark + "\n")
	fmt.Fprintf(&b, "# Addon %s, timer %s. Not a file to edit: it is rendered again at every start and whenever the\n# addon changes. The schedule is the timer's.\n", id, t.Name)
	desc := t.AddonName
	if desc == "" {
		desc = id
	}
	if t.Description != "" {
		desc += ": " + t.Description
	} else {
		desc += ": " + t.Name
	}
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\n\n[Service]\nType=oneshot\n", unitText(desc))
	home := addonsDir + "/" + id
	cmd := []string{home + "/" + t.Bin + "/" + t.Exec}
	cmd = append(cmd, t.Args...)
	cmd = append(cmd, t.Name)
	fmt.Fprintf(&b, "ExecStart=%s\nWorkingDirectory=-%s\n", strings.Join(cmd, " "), home)
	if t.DropIn.Mode == "confined" {
		user := "addon-" + id
		fmt.Fprintf(&b, "User=%s\nGroup=%s\n", user, user)
		groups := append([]string(nil), t.DropIn.Groups...)
		for _, r := range t.Read {
			groups = append(groups, "addon-"+r)
		}
		if len(groups) > 0 {
			fmt.Fprintf(&b, "SupplementaryGroups=%s\n", strings.Join(groups, " "))
		}
		paths := []string{home, "/usr/local/etc/config/addons/" + id, "/run/addon-" + id, "/tmp", "/var/tmp"}
		paths = append(paths, t.DropIn.Paths...)
		for i := range paths {
			paths[i] = "-" + paths[i]
		}
		b.WriteString("CapabilityBoundingSet=\nNoNewPrivileges=yes\nProtectSystem=strict\nProtectKernelTunables=yes\nProtectControlGroups=yes\nRestrictSUIDSGID=yes\n")
		fmt.Fprintf(&b, "RuntimeDirectory=addon-%s\nRuntimeDirectoryPreserve=yes\n", id)
		fmt.Fprintf(&b, "ReadWritePaths=%s\n", strings.Join(paths, " "))
	} else {
		// as the root addon's own unit: root, without the right to mount unless it declares it
		if !t.DropIn.MayMount {
			b.WriteString("CapabilityBoundingSet=~" + CapSysAdmin + "\n")
		}
	}
	timeout, mem := t.TimeoutSec, t.MemoryMB
	if timeout == 0 {
		timeout = TimerTimeoutDefault
	}
	if mem == 0 {
		mem = TimerMemoryDefault
	}
	fmt.Fprintf(&b, "Nice=10\nIOSchedulingClass=idle\nMemoryHigh=%dM\nMemoryMax=%dM\nTasksMax=64\nTimeoutStartSec=%d\n", mem*2/3, mem, timeout)
	return b.String()
}

// timerDirectives are the [Timer] directives an addon's timer text may carry: when it elapses and
// how exactly. Not Unit= (a timer that starts another unit starts anything, the power-off target
// included) and not WakeSystem=.
var timerDirectives = map[string]bool{
	"OnCalendar": true, "OnBootSec": true, "OnStartupSec": true, "OnActiveSec": true,
	"OnUnitActiveSec": true, "OnUnitInactiveSec": true, "Persistent": true,
	"RandomizedDelaySec": true, "FixedRandomDelay": true, "AccuracySec": true,
}

// CheckTimerText is the rule for a timer text an addon hands over: the sections [Unit] (a
// Description= alone), [Timer] (the directives above, at least one that elapses) and [Install]
// (WantedBy=timers.target alone), comments and empty lines; no line continuation, no NUL, at most
// 4 KiB. The same shape the Services page's form writes, so a timer an addon wrote opens there as
// the schedule it is. What a person later adds on the Services page is not held to this.
func CheckTimerText(text string) error {
	if len(text) > 4096 {
		return errors.New("the timer text is larger than 4 KiB")
	}
	if strings.ContainsRune(text, 0) || strings.ContainsRune(text, '\r') {
		return errors.New("the timer text contains a NUL or a carriage return")
	}
	section, elapses, sawTimer := "", false, false
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasSuffix(line, `\`) {
			return fmt.Errorf("line %d: a line continuation is not allowed", n+1)
		}
		if strings.HasPrefix(line, "[") {
			switch line {
			case "[Unit]", "[Timer]", "[Install]":
				section = line
				sawTimer = sawTimer || line == "[Timer]"
			default:
				return fmt.Errorf("line %d: the section %s is not allowed in an addon's timer", n+1, line)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return fmt.Errorf("line %d: not a Key=value line", n+1)
		}
		switch section {
		case "[Unit]":
			if key != "Description" {
				return fmt.Errorf("line %d: %s= is not allowed in an addon's timer ([Unit] takes Description= alone)", n+1, key)
			}
		case "[Timer]":
			if !timerDirectives[key] {
				return fmt.Errorf("line %d: %s= is not allowed in an addon's timer", n+1, key)
			}
			if strings.HasPrefix(key, "On") && value != "" {
				elapses = true
			}
		case "[Install]":
			if key != "WantedBy" || value != "timers.target" {
				return fmt.Errorf("line %d: [Install] takes WantedBy=timers.target alone", n+1)
			}
		default:
			return fmt.Errorf("line %d: a directive before the first section", n+1)
		}
	}
	if !sawTimer {
		return errors.New("the timer text has no [Timer] section")
	}
	if !elapses {
		return errors.New("the timer never elapses: [Timer] names no OnCalendar= or other On…= directive")
	}
	return nil
}
