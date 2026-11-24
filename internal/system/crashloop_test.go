package system

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// a fake systemd for the crash-loop sampler: per unit its properties, a clock and the uptime
type fakeUnits struct {
	props map[string]map[string]string
	now   time.Time
	boot  time.Time
	calls int
}

func (f *fakeUnits) show(_ context.Context, units []string, _ ...string) map[string]map[string]string {
	f.calls++
	out := map[string]map[string]string{}
	for _, u := range units {
		if p, ok := f.props[u]; ok {
			cp := map[string]string{}
			for k, v := range p {
				cp[k] = v
			}
			out[u] = cp
		}
	}
	return out
}

// set puts a unit's state: active since `since` (zero = not active), its restart counter and result
func (f *fakeUnits) set(unit, active, sub string, since time.Time, n int, result string) {
	if f.props == nil {
		f.props = map[string]map[string]string{}
	}
	p := map[string]string{"LoadState": "loaded", "ActiveState": active, "SubState": sub, "Result": result, "NRestarts": strconv.Itoa(n), "ControlGroup": "/system.slice/" + unit}
	if !since.IsZero() {
		p["ActiveEnterTimestampMonotonic"] = strconv.FormatInt(since.Sub(f.boot).Microseconds(), 10)
	}
	f.props[unit] = p
}

func (f *fakeUnits) watcher() *CrashLoops {
	return &CrashLoops{Show: f.show, Now: func() time.Time { return f.now }, Uptime: func() time.Duration { return f.now.Sub(f.boot) }}
}

// openccu-lite task 283: a core unit that restarts again and again is a crash loop once it
// restarted three times within ten minutes without staying up 120 s; the loop ends when it has.
func TestCrashLoopCoreUnit(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	c := f.watcher()

	// rfd up for an hour, never restarted; hmipserver restarted twice last week and up since
	f.set("rfd.service", "active", "running", f.boot.Add(time.Minute), 0, "success")
	f.set("hmipserver.service", "active", "running", f.boot.Add(2*time.Minute), 2, "success")
	c.Sample(context.Background())
	if l := c.Loops(); len(l) != 0 {
		t.Fatalf("no loop yet: %v", l)
	}

	// rfd fails at every start: it restarts, each time a moment after the last
	for i := 1; i <= 3; i++ {
		f.now = f.now.Add(40 * time.Second)
		f.set("rfd.service", "activating", "auto-restart", time.Time{}, i, "exit-code")
		c.Sample(context.Background())
		if l := c.Loops(); (i < 3) != (len(l) == 0) {
			t.Fatalf("after %d restarts: %v", i, l)
		}
	}
	l := c.Loops()
	if len(l) != 1 || l[0].Unit != "rfd" || l[0].Restarts != 3 || l[0].Total != 3 || l[0].Result != "exit-code" || l[0].Addon {
		t.Fatalf("the loop: %+v", l)
	}
	// briefly up again: still a loop
	f.now = f.now.Add(10 * time.Second)
	f.set("rfd.service", "active", "running", f.now.Add(-5*time.Second), 3, "success")
	c.Sample(context.Background())
	if len(c.Loops()) != 1 {
		t.Fatal("a unit up for 5 s is still in its loop")
	}
	// up for 120 s: the loop is over, though the restarts are still within the window
	f.now = f.now.Add(2 * time.Minute)
	c.Sample(context.Background())
	if l := c.Loops(); len(l) != 0 {
		t.Fatalf("stable again: %v", l)
	}
	// and the restarts fall out of the window
	f.now = f.now.Add(11 * time.Minute)
	f.set("rfd.service", "activating", "auto-restart", time.Time{}, 4, "signal")
	c.Sample(context.Background())
	if l := c.Loops(); len(l) != 0 {
		t.Fatalf("one restart after the window: %v", l)
	}

	// a unit the system does not have is skipped, and systemctl not answering changes nothing
	f.props["hs485d.service"] = map[string]string{"LoadState": "not-found"}
	saved := f.props
	f.props = nil
	c.Sample(context.Background())
	f.props = saved
	if _, ok := c.units["hs485d"]; ok {
		t.Error("a unit that is not there is watched")
	}
}

// the first sample after occulited's start: restarts that happened before count while the unit
// is not up, so a loop that began before occulited is seen at once
func TestCrashLoopFirstSample(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(20 * time.Minute)
	f.set("lighttpd.service", "activating", "auto-restart", time.Time{}, 7, "exit-code")
	f.set("sshd.service", "active", "running", f.boot.Add(5*time.Minute), 7, "success")
	c := f.watcher()
	c.Sample(context.Background())
	l := c.Loops()
	if len(l) != 1 || l[0].Unit != "lighttpd" || l[0].Total != 7 || l[0].Restarts != 3 {
		t.Fatalf("first sample: %+v", l)
	}
	if !strings.Contains(l[0].String(), "lighttpd: 3 restarts") {
		t.Errorf("line: %s", l[0])
	}
	// a start by hand resets NRestarts: not a restart
	f.now = f.now.Add(15 * time.Second)
	f.set("lighttpd.service", "active", "running", f.now, 0, "success")
	c.Sample(context.Background())
	if c.units["lighttpd"].last != 0 || len(c.units["lighttpd"].restarts) != 3 {
		t.Errorf("reset: %+v", c.units["lighttpd"])
	}
	if (*CrashLoops)(nil).Loops() != nil {
		t.Error("a nil watcher has loops")
	}
}

// the addon supervisor: an addon that declares a daemon and whose unit is active with an empty
// cgroup is restarted after the backoff, 2 s doubling; its restarts are a crash loop at three
// within fifteen minutes; a daemon that stays up 120 s starts the backoff again; a stopped unit is
// left alone
func TestCrashLoopAddonSupervisor(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 9, 27, 20, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	running := map[string]bool{}
	var restarted []string
	c := f.watcher()
	c.Supervised = func() []string { return []string{"mosquitto"} }
	c.Procs = func(cg string) bool { return running[cg] }
	c.Restart = func(_ context.Context, unit string) error {
		restarted = append(restarted, unit)
		if unit == "addon-broken" {
			return errors.New("no")
		}
		return nil
	}
	const unit = "addon-mosquitto.service"
	cg := "/system.slice/" + unit
	f.set(unit, "active", "exited", f.boot.Add(time.Minute), 0, "success")
	running[cg] = true
	c.Sample(context.Background())
	if len(restarted) != 0 || len(c.Loops()) != 0 {
		t.Fatal("a running daemon is restarted")
	}

	// the daemon ends: the first restart after 2 s (the next sample, which Run takes that soon)
	running[cg] = false
	if w := c.wait(); w != CrashLoopInterval {
		t.Errorf("nothing due: wait %v", w)
	}
	c.Sample(context.Background())
	if len(restarted) != 0 {
		t.Fatal("restarted before the backoff")
	}
	if w := c.wait(); w != 2*time.Second {
		t.Errorf("a restart due in 2 s: wait %v", w)
	}
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 1 || restarted[0] != "addon-mosquitto" {
		t.Fatalf("restart: %v", restarted)
	}
	// it ends again at once, twice more: 4 s, then 8 s
	waits := []time.Duration{4 * time.Second, 8 * time.Second}
	for i, w := range waits {
		c.Sample(context.Background()) // schedules
		f.now = f.now.Add(w - time.Second)
		c.Sample(context.Background())
		if len(restarted) != i+1 {
			t.Fatalf("restart %d came before its %v", i+2, w)
		}
		f.now = f.now.Add(time.Second)
		c.Sample(context.Background())
		if len(restarted) != i+2 {
			t.Fatalf("restart %d did not come after %v", i+2, w)
		}
	}
	l := c.Loops()
	if len(l) != 1 || l[0].Unit != "addon-mosquitto" || !l[0].Addon || l[0].Restarts != 3 {
		t.Fatalf("the addon's loop: %+v", l)
	}
	// the warnings are told when the set of loops changes, once
	changes := 0
	c.OnChange = func() { changes++ }
	c.Sample(context.Background())
	c.Sample(context.Background())
	if changes != 1 {
		t.Errorf("OnChange %d times", changes)
	}
	if c.attempts("mosquitto") != 3 || c.attempts("other") != 0 {
		t.Errorf("attempts %d", c.attempts("mosquitto"))
	}

	// now it keeps running: after 120 s the loop is over and the backoff starts again
	running[cg] = true
	c.Sample(context.Background())
	f.now = f.now.Add(121 * time.Second)
	c.Sample(context.Background())
	if len(c.Loops()) != 0 || c.attempts("mosquitto") != 0 {
		t.Fatalf("stable: %+v attempts %d", c.Loops(), c.attempts("mosquitto"))
	}
	if changes != 2 {
		t.Errorf("the loop's end: OnChange %d times", changes)
	}

	// stopped by the user: inactive, never restarted
	running[cg] = false
	f.set(unit, "inactive", "dead", time.Time{}, 0, "success")
	n := len(restarted)
	f.now = f.now.Add(time.Hour)
	c.Sample(context.Background())
	c.Sample(context.Background())
	if len(restarted) != n {
		t.Error("a stopped addon was restarted")
	}
	// a failed oneshot (the start failed) is systemd's failed state, not the supervisor's
	f.set(unit, "active", "exited", f.now, 0, "exit-code")
	c.Sample(context.Background())
	f.now = f.now.Add(time.Minute)
	c.Sample(context.Background())
	if len(restarted) != n {
		t.Error("a unit with a failed result was restarted")
	}

	// removed from the supervised list: forgotten
	c.Supervised = func() []string { return []string{"broken"} }
	f.set("addon-broken.service", "active", "exited", f.now, 0, "success")
	c.Sample(context.Background())
	if _, ok := c.addons["mosquitto"]; ok {
		t.Error("a removed addon is still supervised")
	}
	f.now = f.now.Add(3 * time.Second)
	c.Sample(context.Background()) // the restart fails: logged, counted
	if restarted[len(restarted)-1] != "addon-broken" {
		t.Errorf("restarts %v", restarted)
	}
}

// Run samples until its context ends
func TestCrashLoopRun(t *testing.T) {
	f := &fakeUnits{boot: time.Now().Add(-time.Hour), now: time.Now()}
	c := f.watcher()
	sampled := make(chan struct{}, 1)
	c.Show = func(ctx context.Context, units []string, props ...string) map[string]map[string]string {
		select {
		case sampled <- struct{}{}:
		default:
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	<-sampled
	cancel()
	<-done
}

func TestAddonBackoff(t *testing.T) {
	want := []time.Duration{2, 4, 8, 16, 32, 64, 128, 256, 300, 300}
	for i, w := range want {
		if got := addonBackoff(i); got != w*time.Second {
			t.Errorf("attempt %d: %v, want %v", i, got, w*time.Second)
		}
	}
}

// the state file for the waiting page: the unit's script writes it, the daemon reads it. The
// script runs here with a fake systemctl through every case.
func TestOcculitedUnitStateScript(t *testing.T) {
	script, err := filepath.Abs("../../deploy/systemd/occulited-unit-state")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	root := Root(dir)
	state := filepath.Join(dir, strings.TrimPrefix(OcculitedUnitStateFile, "/"))
	// the fake systemctl: NRestarts and RestartUSecNext from files, list-jobs from a file,
	// is-system-running from a file
	fake := filepath.Join(dir, "systemctl")
	body := `#!/bin/sh
d=` + dir + `
case "$1" in
show) p=$3; echo "$p=$(cat $d/$p 2>/dev/null)";;
list-jobs) cat $d/jobs 2>/dev/null;;
is-system-running) cat $d/running 2>/dev/null || echo running;;
esac
`
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	put := func(name, v string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("NRestarts", "0")
	put("RestartUSecNext", "2s")
	now := int64(1790000000)
	run := func(step string, env ...string) *OcculitedUnitState {
		t.Helper()
		cmd := exec.Command("sh", script, step)
		cmd.Env = append(os.Environ(), "SYSTEMCTL="+fake, "OCCULITE_UNIT_STATE="+state, "OCCULITE_NOW="+strconv.FormatInt(now, 10))
		cmd.Env = append(cmd.Env, env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", step, err, out)
		}
		s := root.ReadOcculitedUnitState()
		if s == nil {
			t.Fatalf("%s: no state file", step)
		}
		return s
	}

	// the boot: starting, then running
	if s := run("start"); s.State != "starting" || s.Since != now || s.Fails != 0 {
		t.Fatalf("start: %+v", s)
	}
	now += 1
	if s := run("up"); s.State != "running" || s.Started != now-1 {
		t.Fatalf("up: %+v", s)
	}
	if st, _ := os.Stat(state); st.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", st.Mode())
	}

	// a crash 30 s after the start: restarting, the next attempt from RestartUSecNext
	now += 30
	put("RestartUSecNext", "1min 4.500s")
	s := run("stop", "SERVICE_RESULT=exit-code")
	if s.State != "restarting" || s.Fails != 1 || s.FirstFail != now || s.LastFail != now || s.Result != "exit-code" || s.NextRetry != now+65 {
		t.Fatalf("first failure: %+v", s)
	}
	// the attempt keeps the case; two more quick failures are a crash loop
	for i := 2; i <= 3; i++ {
		now += 65
		put("NRestarts", strconv.Itoa(i-1))
		if s = run("start"); s.State != "restarting" && s.State != "crash-loop" {
			t.Fatalf("attempt %d: %+v", i, s)
		}
		if s.Restarts != i-1 || s.NextRetry != 0 {
			t.Errorf("attempt %d: %+v", i, s)
		}
		run("up")
		now += 10
		s = run("stop", "SERVICE_RESULT=signal")
	}
	if s.State != "crash-loop" || s.Fails != 3 || s.Result != "signal" || s.FirstFail == s.LastFail {
		t.Fatalf("the loop: %+v", s)
	}
	// the daemon reads it
	if s.Fails < OcculitedCrashLoopFails {
		t.Fatal("the daemon's threshold")
	}

	// back, up for longer than 120 s, then one failure: the first again
	now += 300
	run("start")
	run("up")
	now += 600
	if s = run("stop", "SERVICE_RESULT=core-dump"); s.State != "restarting" || s.Fails != 1 || s.FirstFail != now {
		t.Fatalf("after a long run: %+v", s)
	}

	// stopped on purpose: a stop job queued for the unit
	put("jobs", "123 occulited.service stop running\n")
	if s = run("stop", "SERVICE_RESULT=success"); s.State != "stopped" || s.Reason != "stop" || s.Fails != 0 {
		t.Fatalf("stop: %+v", s)
	}
	// a restart asked for
	put("jobs", "124 occulited.service restart running\n")
	if s = run("stop", "SERVICE_RESULT=success"); s.State != "starting" || s.Reason != "restart" {
		t.Fatalf("restart: %+v", s)
	}
	// the system going down
	put("jobs", "1 reboot.target start waiting\n125 occulited.service stop running\n")
	if s = run("stop"); s.State != "stopped" || s.Reason != "shutdown" {
		t.Fatalf("shutdown: %+v", s)
	}
	put("jobs", "")
	put("running", "stopping")
	if s = run("stop"); s.Reason != "shutdown" {
		t.Fatalf("stopping: %+v", s)
	}
	put("running", "running")
	// a clean exit nobody asked for: it comes back, not a failure
	if s = run("stop", "SERVICE_RESULT=success"); s.State != "starting" || s.Fails != 0 {
		t.Fatalf("clean exit: %+v", s)
	}
	// a start after a stop: starting, the counters cleared
	if s = run("start"); s.State != "starting" || s.Fails != 0 || s.Result != "" {
		t.Fatalf("start after stop: %+v", s)
	}
	// a wrong argument: usage, exit 2, the file unchanged
	cmd := exec.Command("sh", script, "nonsense")
	cmd.Env = append(os.Environ(), "SYSTEMCTL="+fake, "OCCULITE_UNIT_STATE="+state)
	if err := cmd.Run(); err == nil {
		t.Error("a wrong argument passes")
	}
	// no file: nil
	if Root(t.TempDir()).ReadOcculitedUnitState() != nil {
		t.Error("a state out of nowhere")
	}
	_ = os.WriteFile(state, []byte("{"), 0o644)
	if root.ReadOcculitedUnitState() != nil {
		t.Error("a broken file read")
	}
}

// occulited B-30: while an install or uninstall runs the supervisor restarts nothing - the job's
// update script stops the daemon and the job settles the unit itself - and the first sample after
// the job does not restart either: an empty unit starts its backoff from there.
func TestCrashLoopPausedDuringAnAddonJob(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	running := map[string]bool{}
	var restarted []string
	busy := false
	c := f.watcher()
	c.Supervised = func() []string { return []string{"hmm"} }
	c.Procs = func(cg string) bool { return running[cg] }
	c.Restart = func(_ context.Context, unit string) error { restarted = append(restarted, unit); return nil }
	c.Paused = func() bool { return busy }
	const unit = "addon-hmm.service"
	cg := "/system.slice/" + unit
	f.set(unit, "active", "exited", f.boot.Add(time.Minute), 0, "success")
	running[cg] = true
	c.Sample(context.Background())

	// the install starts; its update script stops the daemon, the unit stays active and empty
	busy = true
	running[cg] = false
	for i := 0; i < 5; i++ {
		c.Sample(context.Background())
		f.now = f.now.Add(15 * time.Second)
	}
	if len(restarted) != 0 {
		t.Fatalf("restarted during the job: %v", restarted)
	}
	if w := c.wait(); w != CrashLoopInterval {
		t.Errorf("a restart is due during the job: wait %v", w)
	}
	// the job ends with the unit still empty: the first sample only schedules
	busy = false
	c.Sample(context.Background())
	if len(restarted) != 0 {
		t.Fatalf("restarted in the first sample after the job: %v", restarted)
	}
	// the job's settle step started it meanwhile: nothing to do
	running[cg] = true
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 0 || c.attempts("hmm") != 0 {
		t.Fatalf("restarted a running daemon: %v", restarted)
	}
	// had it not, the supervisor takes over after the backoff, as for any ended daemon
	running[cg] = false
	c.Sample(context.Background())
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 1 {
		t.Fatalf("an ended daemon after the job: %v", restarted)
	}
}

// occulited B-30: SystemdAddons.Busy is true for the whole of an uninstall (the unit's stop at its
// start included) and false again afterwards - what main.go hands the supervisor as Paused.
func TestSystemdAddonsBusyDuringAJob(t *testing.T) {
	r := rootWith(t, map[string]string{"usr/local/etc/config/rc.d/x": "#!/bin/sh\nexit 0\n"})
	var a *SystemdAddons
	var during []bool
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		during = append(during, a.Busy())
		return nil, nil
	}
	a = NewSystemdAddons(r, SystemdServices{Root: r, Run: run})
	if a.Busy() {
		t.Fatal("busy before")
	}
	_, _ = a.Uninstall(context.Background(), "x")
	if len(during) == 0 || slices.Contains(during, false) {
		t.Errorf("busy during the uninstall: %v", during)
	}
	if a.Busy() {
		t.Error("busy after")
	}
}

// occulited B-166: an addon whose start script chose not to start anything (hm2mqtt without a
// broker) is not a crashed daemon. Never seen holding a process, its unit is started once more,
// quietly; nothing there right after that start - it is idle and left alone until its unit is
// started again. One that holds a process then keeps a daemon, and an empty unit later is a
// daemon that ended.
func TestCrashLoopAddonThatChoseNotToRun(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 11, 22, 14, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	running := map[string]bool{}
	var restarted []string
	startsDaemon := false
	learned := map[string]bool{}
	var logs bytes.Buffer
	c := f.watcher()
	c.Log = slog.New(slog.NewTextHandler(&logs, nil))
	c.Supervised = func() []string { return []string{"hm2mqtt"} }
	c.Procs = func(cg string) bool { return running[cg] }
	c.Learned = func(id string) bool { return learned[id] }
	c.Learn = func(id string) { learned[id] = true }
	const unit = "addon-hm2mqtt.service"
	cg := "/system.slice/" + unit
	started := f.boot.Add(time.Minute)
	c.Restart = func(_ context.Context, u string) error {
		restarted = append(restarted, u)
		started = f.now
		f.set(unit, "active", "exited", started, 0, "success")
		running[cg] = startsDaemon
		return nil
	}
	f.set(unit, "active", "exited", started, 0, "success")

	// the first sample: active, empty, never seen running - not running by its choice as far as
	// anybody knows, and one more start is due
	c.Sample(context.Background())
	if !c.Idle("hm2mqtt") || len(restarted) != 0 {
		t.Fatalf("before the probe: idle %v, restarted %v", c.Idle("hm2mqtt"), restarted)
	}
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 1 {
		t.Fatalf("the one more start: %v", restarted)
	}
	if !strings.Contains(logs.String(), "level=INFO") || strings.Contains(logs.String(), "level=WARN") || strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("the probe is said at info, nothing above:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "leaves nothing running") {
		t.Errorf("the verdict is not said:\n%s", logs.String())
	}
	// idle: never again, however long, and no crash loop, no attempt counted
	for i := 0; i < 40; i++ {
		f.now = f.now.Add(15 * time.Second)
		c.Sample(context.Background())
	}
	if len(restarted) != 1 || len(c.Loops()) != 0 || c.attempts("hm2mqtt") != 0 || !c.Idle("hm2mqtt") {
		t.Fatalf("an idle addon: restarted %v, loops %v, attempts %d, idle %v", restarted, c.Loops(), c.attempts("hm2mqtt"), c.Idle("hm2mqtt"))
	}
	if w := c.wait(); w != CrashLoopInterval {
		t.Errorf("nothing is due for an idle addon: wait %v", w)
	}
	if h := c.units["addon-hm2mqtt"]; h == nil || len(h.restarts) != 0 || h.last != 0 {
		t.Errorf("the probe was counted as a restart: %+v", h)
	}

	// the user starts the unit again and it still chooses not to run: looked at afresh, once
	started = f.now
	f.set(unit, "active", "exited", started, 0, "success")
	c.Sample(context.Background())
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 2 {
		t.Fatalf("a unit started again is probed again: %v", restarted)
	}
	f.now = f.now.Add(10 * time.Minute)
	c.Sample(context.Background())
	if len(restarted) != 2 || !c.Idle("hm2mqtt") {
		t.Fatalf("idle again: %v", restarted)
	}

	// the broker is configured and the unit started: the daemon runs, is learned, and is no
	// longer idle
	started = f.now
	f.set(unit, "active", "exited", started, 0, "success")
	running[cg] = true
	c.Sample(context.Background())
	if c.Idle("hm2mqtt") || !learned["hm2mqtt"] || len(restarted) != 2 {
		t.Fatalf("a running daemon: idle %v, learned %v, restarted %v", c.Idle("hm2mqtt"), learned, restarted)
	}
	// it ends: now that is a daemon that ended - a warning, a restart, counted
	logs.Reset()
	running[cg] = false
	startsDaemon = false
	c.Sample(context.Background())
	if c.Idle("hm2mqtt") {
		t.Fatal("an ended daemon is called idle")
	}
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 3 || !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "daemon ended") || c.attempts("hm2mqtt") != 1 {
		t.Fatalf("an ended daemon: restarted %v, attempts %d\n%s", restarted, c.attempts("hm2mqtt"), logs.String())
	}
	// and it stays supervised although the restart left nothing running: it was seen running
	c.Sample(context.Background())
	f.now = f.now.Add(4 * time.Second)
	c.Sample(context.Background())
	if len(restarted) != 4 {
		t.Fatalf("a learned daemon is restarted again: %v", restarted)
	}
}

// B-166: the probe finds a daemon - the addon keeps one, is learned, and is supervised from then on.
func TestCrashLoopProbeFindsADaemon(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 11, 22, 14, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	running := map[string]bool{}
	restarts := 0
	var learned []string
	c := f.watcher()
	c.Supervised = func() []string { return []string{"mosquitto"} }
	c.Procs = func(cg string) bool { return running[cg] }
	c.Learn = func(id string) { learned = append(learned, id) }
	const unit = "addon-mosquitto.service"
	cg := "/system.slice/" + unit
	c.Restart = func(context.Context, string) error { restarts++; running[cg] = true; return nil }
	// died between its start and the first sample: never seen, so the first restart is the probe
	f.set(unit, "active", "exited", f.boot.Add(time.Minute), 0, "success")
	c.Sample(context.Background())
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if restarts != 1 || c.Idle("mosquitto") || len(learned) != 1 {
		t.Fatalf("restarts %d, idle %v, learned %v", restarts, c.Idle("mosquitto"), learned)
	}
	// that restart counts: the daemon had ended
	if c.attempts("mosquitto") != 1 || len(c.units["addon-mosquitto"].restarts) != 1 {
		t.Errorf("attempts %d, restarts %v", c.attempts("mosquitto"), c.units["addon-mosquitto"].restarts)
	}
	running[cg] = false
	c.Sample(context.Background())
	f.now = f.now.Add(4 * time.Second)
	c.Sample(context.Background())
	if restarts != 2 {
		t.Fatalf("an ended daemon after the probe: %d restarts", restarts)
	}
	// nobody asked: an addon that is not supervised, and no supervisor at all
	if c.Idle("other") || (*CrashLoops)(nil).Idle("mosquitto") {
		t.Error("idle without supervision")
	}
}

// B-166: a restart that loses the race with an install or uninstall is not an error. A job that
// began after the sample: the restart is not made; one that begins while it runs and makes it
// fail: said at info. Neither counts.
func TestCrashLoopRestartRacingAJob(t *testing.T) {
	f := &fakeUnits{boot: time.Date(2026, 11, 22, 14, 0, 0, 0, time.UTC)}
	f.now = f.boot.Add(time.Hour)
	running := map[string]bool{}
	var restarted []string
	var logs bytes.Buffer
	pausedCalls, busyFrom := 0, 0
	var restartErr error
	jobDuringRestart := false
	busy := false
	c := f.watcher()
	c.Log = slog.New(slog.NewTextHandler(&logs, nil))
	c.Supervised = func() []string { return []string{"hm2mqtt"} }
	c.Procs = func(cg string) bool { return running[cg] }
	// the sample asks once; busyFrom is the call from which a job runs
	c.Paused = func() bool {
		pausedCalls++
		return busy || (busyFrom > 0 && pausedCalls >= busyFrom)
	}
	c.Restart = func(_ context.Context, unit string) error {
		restarted = append(restarted, unit)
		if jobDuringRestart {
			busy = true
		}
		return restartErr
	}
	const unit = "addon-hm2mqtt.service"
	cg := "/system.slice/" + unit
	f.set(unit, "active", "exited", f.boot.Add(time.Minute), 0, "success")
	running[cg] = true
	c.Sample(context.Background()) // seen running
	running[cg] = false
	c.Sample(context.Background()) // schedules

	// the job begins between the sample and the restart
	f.now = f.now.Add(2 * time.Second)
	pausedCalls, busyFrom = 0, 2
	c.Sample(context.Background())
	if len(restarted) != 0 {
		t.Fatalf("restarted into a job: %v", restarted)
	}
	if c.attempts("hm2mqtt") != 0 || len(c.units["addon-hm2mqtt"].restarts) != 0 {
		t.Errorf("a restart that was not made is counted: attempts %d", c.attempts("hm2mqtt"))
	}
	if logs.Len() != 0 {
		t.Errorf("said something: %s", logs.String())
	}

	// the job is over; the next restart meets another job's stop and fails
	busyFrom = 0
	c.Sample(context.Background()) // the first sample after a job schedules afresh
	f.now = f.now.Add(2 * time.Second)
	restartErr, jobDuringRestart = errors.New("systemctl restart addon-hm2mqtt.service: exit status 1"), true
	c.Sample(context.Background())
	if len(restarted) != 1 {
		t.Fatalf("the restart: %v", restarted)
	}
	if strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "met an addon install or uninstall") {
		t.Errorf("a restart lost to a job is an error:\n%s", logs.String())
	}
	if c.attempts("hm2mqtt") != 0 || len(c.units["addon-hm2mqtt"].restarts) != 0 {
		t.Errorf("a restart lost to a job is counted: attempts %d", c.attempts("hm2mqtt"))
	}

	// without a job a failing restart is an error, as before
	busy, jobDuringRestart = false, false
	logs.Reset()
	c.Sample(context.Background())
	f.now = f.now.Add(2 * time.Second)
	c.Sample(context.Background())
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "the restart failed") {
		t.Errorf("a failing restart without a job:\n%s", logs.String())
	}
}
