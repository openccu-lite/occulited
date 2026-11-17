package addonunit

import (
	"strings"
	"testing"
)

func confinedTimer() TimerService {
	return TimerService{
		DropIn:      DropIn{ID: "sync", Mode: "confined", UID: 30007, Groups: []string{"certs"}, Paths: []string{"/usr/local/syncdata"}},
		AddonName:   "Sync",
		Name:        "fotos",
		Bin:         "bin",
		Exec:        "lite-sync",
		Args:        []string{"run"},
		Description: "Fotos Klingel",
	}
}

// occulited task 154: the service of an addon's timer is the system's text - the addon's user, no
// capability, the confinement of its own unit, the program in its root-owned bin directory.
func TestTimerServiceRender(t *testing.T) {
	ts := confinedTimer()
	if err := ts.Validate(); err != nil {
		t.Fatal(err)
	}
	text := ts.Render()
	for _, want := range []string{
		TimerServiceMark + "\n",
		"\nDescription=Sync: Fotos Klingel\n",
		"\nType=oneshot\n",
		"\nExecStart=/usr/local/addons/sync/bin/lite-sync run fotos\n",
		"\nUser=addon-sync\nGroup=addon-sync\nSupplementaryGroups=certs\n",
		"\nCapabilityBoundingSet=\nNoNewPrivileges=yes\nProtectSystem=strict\n",
		"\nReadWritePaths=-/usr/local/addons/sync -/usr/local/etc/config/addons/sync -/run/addon-sync -/tmp -/var/tmp -/usr/local/syncdata\n",
		"\nRuntimeDirectory=addon-sync\nRuntimeDirectoryPreserve=yes\n",
		"\nMemoryHigh=200M\nMemoryMax=300M\nTasksMax=64\nTimeoutStartSec=21600\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the service lacks %q:\n%s", want, text)
		}
	}
	for _, never := range []string{"AmbientCapabilities", "ExecStartPre", "ExecStartPost", "+/", "User=root"} {
		if strings.Contains(text, never) {
			t.Errorf("the service has %q:\n%s", never, text)
		}
	}

	// the read grant: the named addons' own groups, after the addon's
	ts.Read = []string{"history", "hmm"}
	ts.TimeoutSec, ts.MemoryMB = 3600, 150
	if err := ts.Validate(); err != nil {
		t.Fatal(err)
	}
	text = ts.Render()
	if !strings.Contains(text, "\nSupplementaryGroups=certs addon-history addon-hmm\n") || !strings.Contains(text, "\nMemoryHigh=100M\nMemoryMax=150M\n") || !strings.Contains(text, "\nTimeoutStartSec=3600\n") {
		t.Errorf("read grant or limits:\n%s", text)
	}

	// a root addon's job is root's, as the addon is by the user's choice; without the right to mount
	root := TimerService{DropIn: DropIn{ID: "legacy", Mode: "root"}, Name: "job", Bin: "bin", Exec: "run"}
	if err := root.Validate(); err != nil {
		t.Fatal(err)
	}
	text = root.Render()
	if strings.Contains(text, "User=") || !strings.Contains(text, "\nCapabilityBoundingSet=~CAP_SYS_ADMIN\n") || !strings.Contains(text, "\nExecStart=/usr/local/addons/legacy/bin/run job\n") || !strings.Contains(text, "Description=legacy: job\n") {
		t.Errorf("a root addon's job:\n%s", text)
	}
}

func TestTimerServiceValidate(t *testing.T) {
	for name, change := range map[string]func(*TimerService){
		"a name with a space":       func(ts *TimerService) { ts.Name = "a b" },
		"a name with a dot":         func(ts *TimerService) { ts.Name = "a.service" },
		"an upper-case name":        func(ts *TimerService) { ts.Name = "Fotos" },
		"an empty name":             func(ts *TimerService) { ts.Name = "" },
		"a name that is an option":  func(ts *TimerService) { ts.Name = "-x" },
		"a program with a slash":    func(ts *TimerService) { ts.Exec = "../../bin/sh" },
		"an argument with a space":  func(ts *TimerService) { ts.Args = []string{"a b"} },
		"a specifier argument":      func(ts *TimerService) { ts.Args = []string{"%h"} },
		"an absolute bin":           func(ts *TimerService) { ts.Bin = "/bin" },
		"a two-line description":    func(ts *TimerService) { ts.Description = "a\n[Service]\nUser=root" },
		"a continued description":   func(ts *TimerService) { ts.Description = `a\` },
		"a long description":        func(ts *TimerService) { ts.Description = strings.Repeat("a", TimerDescriptionMax+1) },
		"reading itself":            func(ts *TimerService) { ts.Read = []string{"sync"} },
		"reading a system group":    func(ts *TimerService) { ts.Read = []string{"../occulite"} },
		"reading with a space":      func(ts *TimerService) { ts.Read = []string{"a root"} },
		"reading an upper-case id":  func(ts *TimerService) { ts.Read = []string{"Hmm"} },
		"reading twice":             func(ts *TimerService) { ts.Read = []string{"hmm", "hmm"} },
		"reading too many":          func(ts *TimerService) { ts.Read = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} },
		"a short timeout":           func(ts *TimerService) { ts.TimeoutSec = 5 },
		"a long timeout":            func(ts *TimerService) { ts.TimeoutSec = TimerTimeoutMax + 1 },
		"little memory":             func(ts *TimerService) { ts.MemoryMB = 8 },
		"much memory":               func(ts *TimerService) { ts.MemoryMB = 4096 },
		"a system uid":              func(ts *TimerService) { ts.DropIn.UID = 0 },
		"a root-equivalent group":   func(ts *TimerService) { ts.DropIn.Groups = []string{"root"} },
		"a path with a space":       func(ts *TimerService) { ts.DropIn.Paths = []string{"/a b"} },
		"an addon id with a slash":  func(ts *TimerService) { ts.DropIn.ID = "a/b" },
		"a two-line addon name":     func(ts *TimerService) { ts.AddonName = "a\nb" },
		"a root addon with a group": func(ts *TimerService) { ts.DropIn = DropIn{ID: "sync", Mode: "root", Groups: []string{"x"}} },
	} {
		ts := confinedTimer()
		change(&ts)
		if err := ts.Validate(); err == nil {
			t.Errorf("%s: accepted:\n%s", name, ts.Render())
		}
	}
}

func TestCleanTimerDescription(t *testing.T) {
	for in, want := range map[string]string{
		"Fotos Klingel":              "Fotos Klingel",
		"  a \t b\n[Service]\nx=y  ": "a b [Service] x=y",
		`ends in a backslash \`:      "ends in a backslash",
		`a\\`:                        "a",
		"":                           "",
		strings.Repeat("ä", 100):     strings.Repeat("ä", 60),
		"100 % sicher":               "100 % sicher",
	} {
		if got := CleanTimerDescription(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	ts := confinedTimer()
	ts.Description = CleanTimerDescription("100 %n sicher")
	if !strings.Contains(ts.Render(), "Description=Sync: 100 %%n sicher\n") {
		t.Errorf("a %% is not doubled:\n%s", ts.Render())
	}
}

func TestCheckTimerText(t *testing.T) {
	good := []string{
		"[Unit]\nDescription=Sync: Fotos\n\n[Timer]\nOnCalendar=*-*-* 03:00:00\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n",
		"[Timer]\nOnCalendar=hourly\nRandomizedDelaySec=5min\n",
		"# a comment\n[Timer]\n; another\nOnBootSec=5min\nAccuracySec=1min\n",
		"[Timer]\nOnCalendar=Mon *-*-* 03:00:00\nOnCalendar=Thu *-*-* 03:00:00\nPersistent=true\nFixedRandomDelay=yes\n",
	}
	for _, text := range good {
		if err := CheckTimerText(text); err != nil {
			t.Errorf("refused: %v\n%s", err, text)
		}
	}
	bad := map[string]string{
		"another unit":            "[Timer]\nOnCalendar=hourly\nUnit=poweroff.target\n",
		"waking the system":       "[Timer]\nOnCalendar=hourly\nWakeSystem=yes\n",
		"a service section":       "[Timer]\nOnCalendar=hourly\n[Service]\nExecStart=/bin/sh\n",
		"a unit dependency":       "[Unit]\nWants=reboot.target\n[Timer]\nOnCalendar=hourly\n",
		"an on-failure":           "[Unit]\nOnFailure=reboot.target\n[Timer]\nOnCalendar=hourly\n",
		"another install":         "[Timer]\nOnCalendar=hourly\n[Install]\nWantedBy=multi-user.target\n",
		"an alias":                "[Timer]\nOnCalendar=hourly\n[Install]\nAlias=sshd.timer\n",
		"a continuation":          "[Unit]\nDescription=a \\\n[Timer]\nOnCalendar=hourly\n",
		"no timer section":        "[Unit]\nDescription=a\n",
		"nothing that elapses":    "[Timer]\nPersistent=true\n",
		"an empty calendar":       "[Timer]\nOnCalendar=\n",
		"a line without a key":    "[Timer]\nOnCalendar=hourly\nrubbish\n",
		"before a section":        "OnCalendar=hourly\n[Timer]\nOnCalendar=hourly\n",
		"a NUL":                   "[Timer]\nOnCalendar=hourly\x00\n",
		"a carriage return":       "[Timer]\r\nOnCalendar=hourly\r\n",
		"too large":               "[Timer]\nOnCalendar=hourly\n#" + strings.Repeat("a", 5000) + "\n",
		"empty":                   "",
		"a section in lower case": "[timer]\nOnCalendar=hourly\n",
	}
	for name, text := range bad {
		if err := CheckTimerText(text); err == nil {
			t.Errorf("%s: accepted:\n%s", name, text)
		}
	}
}
