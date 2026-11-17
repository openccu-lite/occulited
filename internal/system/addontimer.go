package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/hobbyquaker/occulited/internal/addonunit"
	"github.com/hobbyquaker/occulited/internal/atomicfile"
)

// ---- an addon's own timers (occulited task 154) -------------------------------------------------
//
// An addon that runs jobs on a schedule needed system:write for it - and an own timer is a root
// unit, so that scope made a bug in the addon root, with the firewall and SSH thrown in. The scope
// timers:addon is the narrow way: with it an addon's token keeps own timers named
// local-<id>-<name>, and of the two files of such a timer the addon writes only the schedule.
//
//   - The .service is rendered by the system (addonunit.TimerService): the addon's user, groups
//     and confinement as its own unit has them, no capability, limits, ExecStart the program the
//     manifest names in runtime.timers inside the root-owned runtime.bin directory, with the
//     timer's name as its last argument. It is rendered again at every start and whenever the
//     addon is installed, updated or its policy changes, so it cannot drift.
//   - The .timer is the addon's text, held to a closed list of directives (addonunit.CheckTimerText).
//     On the Services page a person edits it like any own timer's; the service there is read-only.
//   - Whose a timer is says a small file beside the two (local-<id>-<name>.addon): the name alone
//     is no proof, since a person may have made local-sync-x by hand before the addon came - that
//     one stays the person's, and the addon is refused the name.
//   - A timer may name other addons whose data its run reads, when the manifest says
//     runtime.read_addon_data: the service then carries those addons' own groups (addon-<x>) -
//     never a system group - so plain file permissions decide, read-only, what the run sees.
//   - The timers go with the addon (uninstall), and a timer whose addon is gone or declares no
//     timers any more is removed at the next start.

// AddonTimersMax is how many timers one addon keeps.
const AddonTimersMax = 32

// AddonTimer is one timer of an addon as /addon-timers carries it.
type AddonTimer struct {
	Addon string `json:"addon"`
	// Name is the addon's name for it; the units are local-<addon>-<name>.
	Name        string `json:"name"`
	Unit        string `json:"unit"`
	Service     string `json:"service"`
	TimerFile   string `json:"timer_file"`
	ServiceFile string `json:"service_file"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
	// Read are the addons whose data the run reads, as the addon asked; one that is not installed
	// any more is left out of the service until it is back.
	Read       []string `json:"read"`
	TimeoutSec int      `json:"timeout"`
	MemoryMB   int      `json:"memory_mb"`
}

// AddonTimerInput is what an addon sends to make or change a timer.
type AddonTimerInput struct {
	// Timer is the .timer text; empty on a change keeps the stored text (the schedule a person
	// may have edited on the Services page stays untouched by a change of the description).
	Timer string `json:"timer"`
	// Description is one line about the timer, shown as the unit's description after the addon's
	// name; nil on a change keeps it.
	Description *string `json:"description"`
	// Read are the addons whose data the run reads; nil on a change keeps the list.
	Read *[]string `json:"read"`
	// Enabled switches the timer; nil makes a new timer enabled and leaves an existing one as it is.
	Enabled *bool `json:"enabled"`
	// TimeoutSec and MemoryMB are the run's limits; nil keeps them, 0 is the default.
	TimeoutSec *int `json:"timeout"`
	MemoryMB   *int `json:"memory_mb"`
}

// The errors the routes turn into answers.
var (
	// ErrAddonTimersUndeclared: the addon's manifest names no runtime.timers (or no runtime.bin).
	ErrAddonTimersUndeclared = errors.New("the addon declares no timers")
	// ErrAddonTimerInvalid: a name, a text or a value the rules refuse.
	ErrAddonTimerInvalid = errors.New("invalid addon timer")
	// ErrAddonTimerLimit: the addon has AddonTimersMax timers.
	ErrAddonTimerLimit = errors.New("too many timers")
)

type addonTimerErr struct {
	kind error
	msg  string
}

func (e addonTimerErr) Error() string { return e.msg }
func (e addonTimerErr) Unwrap() error { return e.kind }

func invalidAddonTimer(format string, args ...any) error {
	return addonTimerErr{ErrAddonTimerInvalid, fmt.Sprintf(format, args...)}
}

// addonTimerMeta is the file beside an addon's timer: whose it is and what its service is
// rendered from besides the addon's policy.
type addonTimerMeta struct {
	Addon       string   `json:"addon"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Read        []string `json:"read,omitempty"`
	TimeoutSec  int      `json:"timeout,omitempty"`
	MemoryMB    int      `json:"memory_mb,omitempty"`
}

// addonTimerMetaPath is the file's path for an own timer's short name (local-<name>.addon).
func (s SystemdServices) addonTimerMetaPath(name string) string {
	return filepath.Join(s.unitsDir(), LocalTimerPrefix+name+".addon")
}

// addonTimerMeta reads the file of the own timer name; false for a timer a person made, and for
// a file that does not say what its own name says (it is then nobody's proof).
func (s SystemdServices) addonTimerMeta(name string) (addonTimerMeta, bool) {
	if s.unitsDir() == "" {
		return addonTimerMeta{}, false
	}
	b, err := os.ReadFile(s.addonTimerMetaPath(name))
	if err != nil {
		return addonTimerMeta{}, false
	}
	var m addonTimerMeta
	if json.Unmarshal(b, &m) != nil || m.Addon == "" || addonTimerFull(m.Addon, m.Name) != name {
		return addonTimerMeta{}, false
	}
	return m, true
}

func (s SystemdServices) writeAddonTimerMeta(m addonTimerMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.addonTimerMetaPath(addonTimerFull(m.Addon, m.Name)), append(b, '\n'), 0o640)
}

// addonTimerFull is the own timer's short name of an addon's timer: <addon>-<name>.
func addonTimerFull(addon, name string) string { return addon + "-" + name }

// addonTimerPlan is what an addon's timers are rendered from: its policy's drop-in and its
// manifest's runtime.timers.
type addonTimerPlan struct {
	dropIn    addonunit.DropIn
	addonName string
	bin, exec string
	args      []string
	readData  bool
}

// addonTimerPlan reads the addon's stored manifest and policy; ErrAddonTimersUndeclared when the
// addon is not installed with a manifest that declares runtime.timers and runtime.bin.
func (s SystemdServices) addonTimerPlan(addon string) (addonTimerPlan, error) {
	m := s.Root.ReadAddonManifest(addon)
	if m == nil || m.ID != addon || m.Runtime == nil || m.Runtime.Timers == nil || m.Runtime.Bin == "" {
		return addonTimerPlan{}, addonTimerErr{ErrAddonTimersUndeclared, "the addon " + addon + " is not installed with a manifest that declares runtime.timers and runtime.bin"}
	}
	p := s.Root.ReadAddonPolicy(addon)
	if p == nil {
		return addonTimerPlan{}, addonTimerErr{ErrAddonTimersUndeclared, "the addon " + addon + " has no policy"}
	}
	d := dropInOf(p, s.Root.HasGroup, "")
	d.Capabilities = nil // a job gets none, whatever the addon's daemon holds
	return addonTimerPlan{dropIn: d, addonName: addonunit.CleanTimerDescription(m.Name.In("en")), bin: m.Runtime.Bin, exec: m.Runtime.Timers.Exec, args: m.Runtime.Timers.Args, readData: m.Runtime.ReadAddonData}, nil
}

// AddonTimerReadable lists the addons whose data a timer of addon may read: every other installed
// addon that is confined and whose own group the system knows. Empty when the addon's manifest
// does not say read_addon_data.
func (s SystemdServices) AddonTimerReadable(addon string) []string {
	plan, err := s.addonTimerPlan(addon)
	if err != nil || !plan.readData || plan.dropIn.Mode != "confined" {
		return []string{}
	}
	out := []string{}
	for id := range s.Root.AddonPolicies() {
		if s.addonReadable(addon, id) == nil {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// addonReadable says why a timer of addon may not read the data of other, nil when it may: the
// other is an installed, confined addon whose group addon-<other> exists - an addon's own group
// and no other, whatever the name looks like.
func (s SystemdServices) addonReadable(addon, other string) error {
	if !addonIDRe.MatchString(other) || other == addon {
		return fmt.Errorf("%q is not another addon", other)
	}
	p := s.Root.ReadAddonPolicy(other)
	if p == nil || p.ID != other {
		return fmt.Errorf("the addon %s is not installed", other)
	}
	if p.Mode != "confined" || p.UID < addonunit.MinUID {
		return fmt.Errorf("the addon %s runs as root: its data has no group of its own", other)
	}
	if !groupRe.MatchString("addon-"+other) || !s.Root.HasGroup("addon-"+other) {
		return fmt.Errorf("the addon %s has no group on this system", other)
	}
	return nil
}

// renderAddonTimer is the service text of an addon's timer; the read list is cut down to the
// addons that are readable now when lenient (the replay: an addon that was removed since must not
// leave a unit systemd refuses for an unknown group), else such an entry is an error.
func (s SystemdServices) renderAddonTimer(plan addonTimerPlan, meta addonTimerMeta, lenient bool) (string, addonTimerMeta, error) {
	var read []string
	for _, other := range meta.Read {
		err := s.addonReadable(meta.Addon, other)
		switch {
		case err == nil:
			read = append(read, other)
		case lenient:
			slog.Warn("addon timers: an addon whose data a timer reads is not readable any more and left out", "addon", meta.Addon, "timer", meta.Name, "read", other, "why", err.Error())
		default:
			return "", meta, invalidAddonTimer("read: %v", err)
		}
	}
	if len(read) > 0 {
		if !plan.readData {
			if !lenient {
				return "", meta, invalidAddonTimer("read: the addon's manifest does not declare runtime.read_addon_data")
			}
			read = nil
		} else if plan.dropIn.Mode != "confined" {
			read = nil // a root addon reads everything already
		}
	}
	meta.Read = read
	ts := addonunit.TimerService{DropIn: plan.dropIn, AddonName: plan.addonName, Name: meta.Name, Bin: plan.bin, Exec: plan.exec, Args: plan.args,
		Description: meta.Description, Read: read, TimeoutSec: meta.TimeoutSec, MemoryMB: meta.MemoryMB}
	if err := ts.Validate(); err != nil {
		return "", meta, invalidAddonTimer("%v", err)
	}
	return ts.Render(), meta, nil
}

func addonTimerOf(lt LocalTimer, meta addonTimerMeta) AddonTimer {
	read := meta.Read
	if read == nil {
		read = []string{}
	}
	timeout, mem := meta.TimeoutSec, meta.MemoryMB
	if timeout == 0 {
		timeout = addonunit.TimerTimeoutDefault
	}
	if mem == 0 {
		mem = addonunit.TimerMemoryDefault
	}
	return AddonTimer{Addon: meta.Addon, Name: meta.Name, Unit: lt.Unit, Service: lt.Service, TimerFile: lt.TimerFile, ServiceFile: lt.ServiceFile,
		Enabled: lt.Enabled, Description: meta.Description, Read: read, TimeoutSec: timeout, MemoryMB: mem}
}

// AddonTimers lists the addon's timers, never nil.
func (s SystemdServices) AddonTimers(addon string) []AddonTimer {
	out := []AddonTimer{}
	for _, name := range s.localTimerNames() {
		meta, ok := s.addonTimerMeta(name)
		if !ok || meta.Addon != addon {
			continue
		}
		if lt, err := s.ReadLocalTimer(name); err == nil {
			out = append(out, addonTimerOf(lt, meta))
		}
	}
	return out
}

// addonTimer reads one timer of the addon; ErrLocalTimerNotFound when there is none of that name
// - also when an own timer of that full name exists and is not the addon's.
func (s SystemdServices) addonTimer(addon, name string) (LocalTimer, addonTimerMeta, error) {
	if !addonunit.ValidTimerName(name) || !ValidLocalTimerName(addonTimerFull(addon, name)) {
		return LocalTimer{}, addonTimerMeta{}, invalidAddonTimer("a timer's name is lower-case letters, digits and dashes, and with the addon's id in front at most 32 characters")
	}
	full := addonTimerFull(addon, name)
	meta, ok := s.addonTimerMeta(full)
	if !ok || meta.Addon != addon {
		return LocalTimer{}, addonTimerMeta{}, localTimerErr{ErrLocalTimerNotFound, "the addon " + addon + " has no timer named " + name}
	}
	lt, err := s.ReadLocalTimer(full)
	return lt, meta, err
}

// ReadAddonTimer returns one timer of the addon.
func (s SystemdServices) ReadAddonTimer(addon, name string) (AddonTimer, error) {
	lt, meta, err := s.addonTimer(addon, name)
	if err != nil {
		return AddonTimer{}, err
	}
	return addonTimerOf(lt, meta), nil
}

// PutAddonTimer makes or changes a timer of the addon; created says which. The service is
// rendered here; of the addon's input only the timer text reaches a unit file as it came, after
// addonunit.CheckTimerText.
func (s SystemdServices) PutAddonTimer(ctx context.Context, addon, name string, in AddonTimerInput) (at AddonTimer, created bool, err error) {
	if !addonunit.ValidTimerName(name) || !ValidLocalTimerName(addonTimerFull(addon, name)) {
		return AddonTimer{}, false, invalidAddonTimer("a timer's name is lower-case letters, digits and dashes, and with the addon's id in front at most 32 characters")
	}
	if s.unitsDir() == "" {
		return AddonTimer{}, false, fmt.Errorf("no state directory: timers cannot be kept")
	}
	plan, err := s.addonTimerPlan(addon)
	if err != nil {
		return AddonTimer{}, false, err
	}
	full := addonTimerFull(addon, name)
	meta, mine := s.addonTimerMeta(full)
	cur, rerr := s.ReadLocalTimer(full)
	exists := rerr == nil
	switch {
	case exists && (!mine || meta.Addon != addon):
		return AddonTimer{}, false, localTimerErr{ErrLocalTimerExists, "an own timer named " + full + " exists already and is not this addon's"}
	case !exists:
		// a leftover file without its timer proves nothing: start clean
		meta = addonTimerMeta{Addon: addon, Name: name}
		if len(s.AddonTimers(addon)) >= AddonTimersMax {
			return AddonTimer{}, false, addonTimerErr{ErrAddonTimerLimit, fmt.Sprintf("an addon keeps at most %d timers", AddonTimersMax)}
		}
		if strings.TrimSpace(in.Timer) == "" {
			return AddonTimer{}, false, invalidAddonTimer("a new timer needs its timer text")
		}
	}
	if in.Description != nil {
		meta.Description = addonunit.CleanTimerDescription(*in.Description)
	}
	if in.Read != nil {
		meta.Read = slices.Clone(*in.Read)
	}
	if in.TimeoutSec != nil {
		meta.TimeoutSec = *in.TimeoutSec
	}
	if in.MemoryMB != nil {
		meta.MemoryMB = *in.MemoryMB
	}
	timer := in.Timer
	if strings.TrimSpace(timer) == "" {
		timer = cur.TimerFile
	} else if err := addonunit.CheckTimerText(timer); err != nil {
		return AddonTimer{}, false, invalidAddonTimer("timer: %v", err)
	}
	service, meta, err := s.renderAddonTimer(plan, meta, false)
	if err != nil {
		return AddonTimer{}, false, err
	}
	var lt LocalTimer
	if !exists {
		enable := in.Enabled == nil || *in.Enabled
		if lt, err = s.createLocalTimer(ctx, full, timer, service, enable); err != nil {
			return AddonTimer{}, false, err
		}
		if err := s.writeAddonTimerMeta(meta); err != nil {
			s.dropLocalTimer(ctx, full)
			return AddonTimer{}, false, err
		}
		return addonTimerOf(lt, meta), true, nil
	}
	lt = cur
	if timer != cur.TimerFile || service != cur.ServiceFile {
		if lt, err = s.updateLocalTimer(ctx, cur, timer, service); err != nil {
			return AddonTimer{}, false, err
		}
	}
	if err := s.writeAddonTimerMeta(meta); err != nil {
		return AddonTimer{}, false, err
	}
	if in.Enabled != nil && *in.Enabled != lt.Enabled {
		if _, err := s.SetLocalTimerEnabled(ctx, full, *in.Enabled); err != nil {
			return AddonTimer{}, false, err
		}
		lt.Enabled = *in.Enabled
	}
	return addonTimerOf(lt, meta), false, nil
}

// DeleteAddonTimer removes one timer of the addon.
func (s SystemdServices) DeleteAddonTimer(ctx context.Context, addon, name string) error {
	lt, _, err := s.addonTimer(addon, name)
	if err != nil {
		return err
	}
	return s.DeleteLocalTimer(ctx, lt.Name)
}

// RunAddonTimer starts the timer's service once, now, without waiting for it.
func (s SystemdServices) RunAddonTimer(ctx context.Context, addon, name string) (string, error) {
	lt, _, err := s.addonTimer(addon, name)
	if err != nil {
		return "", err
	}
	return s.RunLocalTimer(ctx, lt.Name)
}

// StopAddonTimer stops the timer's service when it runs; the timer stays as it is.
func (s SystemdServices) StopAddonTimer(ctx context.Context, addon, name string) (string, error) {
	lt, _, err := s.addonTimer(addon, name)
	if err != nil {
		return "", err
	}
	out, err := s.run(ctx, "stop", "--no-block", "--no-pager", "--", lt.Service)
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("systemctl stop %s: %w", lt.Service, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// SetAddonTimerEnabled switches the timer on or off; off keeps both files, so the job can still
// be run by hand.
func (s SystemdServices) SetAddonTimerEnabled(ctx context.Context, addon, name string, on bool) (string, error) {
	lt, _, err := s.addonTimer(addon, name)
	if err != nil {
		return "", err
	}
	return s.SetLocalTimerEnabled(ctx, lt.Name, on)
}

// RemoveAddonTimers removes every timer of the addon (its uninstall) and names the units.
func (s SystemdServices) RemoveAddonTimers(ctx context.Context, addon string) (removed []string) {
	for _, name := range s.localTimerNames() {
		meta, ok := s.addonTimerMeta(name)
		if !ok || meta.Addon != addon {
			continue
		}
		if err := s.DeleteLocalTimer(ctx, name); err != nil {
			slog.Warn("addon timers: a timer of a removed addon could not be removed", "addon", addon, "timer", localTimerUnit(name), "err", err)
			continue
		}
		removed = append(removed, localTimerUnit(name))
	}
	return removed
}

// RefreshAddonTimers renders the services of the addons' timers again from what the addons are
// now - every addon's, or one's when only is not empty - and stores a text that changed. With
// orphans a timer whose addon is gone or declares no timers any more is removed (at start and
// after an install; not in the middle of a policy change, when the manifest may not be stored
// yet). With apply the changed units are also written into /run and systemd reloads (after an
// install, an update, a policy change); at start the replay that follows does that for all own
// timers at once.
func (s SystemdServices) RefreshAddonTimers(ctx context.Context, only string, apply, orphans bool) (changed, removed []string) {
	reload := false
	for _, full := range s.localTimerNames() {
		meta, ok := s.addonTimerMeta(full)
		if !ok || (only != "" && meta.Addon != only) {
			continue
		}
		plan, err := s.addonTimerPlan(meta.Addon)
		if err != nil && !orphans {
			continue
		}
		if err != nil {
			if derr := s.DeleteLocalTimer(ctx, full); derr != nil {
				slog.Warn("addon timers: a timer without its addon could not be removed", "timer", localTimerUnit(full), "err", derr)
				continue
			}
			removed = append(removed, localTimerUnit(full))
			continue
		}
		lt, err := s.ReadLocalTimer(full)
		if err != nil {
			continue
		}
		// the record keeps what the addon asked to read: an addon that is missing now (removed, or
		// its group not made yet) is left out of this rendering only, and is back with the next
		service, _, err := s.renderAddonTimer(plan, meta, true)
		if err != nil {
			slog.Warn("addon timers: a timer's service could not be rendered and stays as it was", "timer", lt.Unit, "err", err)
			continue
		}
		if service == lt.ServiceFile {
			continue
		}
		if err := s.storeLocalTimer(full, lt.TimerFile, service); err != nil {
			slog.Warn("addon timers: a timer's service could not be stored", "timer", lt.Unit, "err", err)
			continue
		}
		changed = append(changed, lt.Service)
		if apply {
			if err := s.applyLocalTimer(full, lt.TimerFile, service); err != nil {
				slog.Warn("addon timers: a timer's service could not be written", "timer", lt.Unit, "err", err)
				continue
			}
			reload = true
		}
	}
	if reload {
		if out, err := s.run(ctx, "daemon-reload"); err != nil {
			slog.Warn("addon timers: daemon-reload failed", "err", err, "output", strings.TrimSpace(string(out)))
		}
	}
	return changed, removed
}
