package backuptarget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// The run's and the daemon's side of addon-provided targets (occulited task 150): the delivery in
// the occulite half (the put, its retries, the confirmation by the provider's list, the catch-up
// from a local target, retention), the progress the card shows, and the targets' states as the
// provider answers them.

// ProgressFile is a running upload's progress, backup-targets/<id>/progress.json: written by the
// deliver process about once a second, read by the daemon for the card's percentage, removed at
// the end.
const ProgressFile = "progress.json"

// Progress is ProgressFile.
type Progress struct {
	Name string    `json:"name"`
	Size int64     `json:"size"`
	Sent int64     `json:"sent"`
	At   time.Time `json:"at"`
}

// ReadProgress reads a target's running upload; ok false when there is none (or it is older than
// two stall timeouts: a process that died left it).
func ReadProgress(dir string, now time.Time) (Progress, bool) {
	b, err := os.ReadFile(filepath.Join(dir, ProgressFile))
	if err != nil {
		return Progress{}, false
	}
	var p Progress
	if json.Unmarshal(b, &p) != nil || p.Size <= 0 || now.Sub(p.At) > 2*providerStall {
		return Progress{}, false
	}
	return p, true
}

// CatchUpMax bounds how many older backups one run sends after the night's own.
const CatchUpMax = 10

// providerRetries are the pauses before the second and third attempt of a put that failed for a
// reason that may pass (unreachable, error); the pipeline's Retry replaces them.
var providerRetries = []time.Duration{15 * time.Second, time.Minute}

// retryable says whether a failed put is worth another attempt in the same run.
func retryable(state string) bool { return state == StateUnreachable || state == StateError }

// nameTimeRe finds the minute in a backup's name: <hostname>-<version>-2026-11-17-0007.sbk.age.
var nameTimeRe = regexp.MustCompile(`-(\d{4}-\d{2}-\d{2}-\d{4})\.sbk(\.age)?$`)

// NameTime is the time a backup's name carries; ok false when it carries none.
func NameTime(name string) (time.Time, bool) {
	m := nameTimeRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02-1504", m[1], time.Local)
	return t, err == nil
}

// byNameTime gives each file the time its name carries: on a provider's destination a file's own
// time is when it was uploaded, so a backup caught up later would count as the newest.
func byNameTime(files []BackupFile) []BackupFile {
	for i := range files {
		if t, ok := NameTime(files[i].Name); ok {
			files[i].Time = t
		}
	}
	SortBackups(files)
	return files
}

// uploadSource answers the local target an addon target's uploads read from: the one it names, or
// - automatic - the first enabled directory or share target that keeps encrypted backups; nil =
// the working copy.
func uploadSource(t Target, all []Target) *Target {
	if t.Addon == nil || t.Addon.Source == SourceStaging {
		return nil
	}
	for i := range all {
		o := &all[i]
		if !o.IsLocal() || !o.Encrypt {
			continue
		}
		if t.Addon.Source == o.ID || t.Addon.Source == "" && o.Enabled {
			return o
		}
	}
	return nil
}

// UploadSource is uploadSource for the API: the id and name of the target an addon target would
// upload from now, "" = the working copy.
func UploadSource(t Target, all []Target) (id, name string) {
	if s := uploadSource(t, all); s != nil {
		return s.ID, s.Name
	}
	return "", ""
}

func (p *Pipeline) listLocal(ctx context.Context, t Target) ([]BackupFile, error) {
	if p.ListLocal != nil {
		return p.ListLocal(ctx, t)
	}
	return ListDir(t.Dir())
}

func (p *Pipeline) openLocal(ctx context.Context, t Target, name string) (io.ReadCloser, error) {
	if p.OpenLocal != nil {
		return p.OpenLocal(ctx, t, name)
	}
	return os.Open(filepath.Join(t.Dir(), name))
}

// progressWriter writes ProgressFile at most once a second.
type progressWriter struct {
	p    *Pipeline
	dir  string
	name string
	size int64
	mu   sync.Mutex
	last time.Time
}

func (w *progressWriter) on(sent int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	if sent < w.size && now.Sub(w.last) < time.Second {
		return
	}
	w.last = now
	b, _ := json.Marshal(Progress{Name: w.name, Size: w.size, Sent: sent, At: now})
	_ = writeAtomic(filepath.Join(w.dir, ProgressFile), b, 0o600)
}

func (w *progressWriter) done() { _ = os.Remove(filepath.Join(w.dir, ProgressFile)) }

// putFile sends one file with the run's retries. open is called once per attempt.
func (p *Pipeline) putFile(ctx context.Context, c *ProviderClient, t Target, name string, size int64, sum string, open func() (io.ReadCloser, error)) (ProviderPut, error) {
	retries := providerRetries
	if p.Retry != nil {
		retries = p.Retry
	}
	dir := p.Store.SecretDir(t.ID)
	_ = os.MkdirAll(dir, 0o700)
	pw := &progressWriter{p: p, dir: dir, name: name, size: size}
	defer pw.done()
	var last error
	for attempt := 0; ; attempt++ {
		in, err := open()
		if err != nil {
			return ProviderPut{}, err
		}
		pw.on(0)
		res, err := c.Put(ctx, t.Addon.Destination, t.Addon.Folder, name, in, size, sum, pw.on)
		in.Close()
		if err == nil {
			return res, nil
		}
		last = err
		st, _ := providerState(err)
		if attempt >= len(retries) || !retryable(st) || ctx.Err() != nil {
			return ProviderPut{}, last
		}
		p.log().Warn("backup: upload failed, trying again", "target", t.ID, "file", name, "attempt", attempt+1, "in", retries[attempt].String(), "err", err)
		select {
		case <-ctx.Done():
			return ProviderPut{}, last
		case <-time.After(retries[attempt]):
		}
	}
}

// deliverAddon hands the run's encrypted backup to the target's provider and applies retention:
// the put (from the local target that already holds the file, else from the working copy), the
// provider's list as the confirmation, the catch-up of older backups the destination lacks, then
// this system's files beyond the number to keep.
func (p *Pipeline) deliverAddon(ctx context.Context, t Target, run Run, host string, all []Target) Result {
	start := time.Now()
	r := Result{At: start, Encrypted: true}
	fail := func(step string, err error) Result {
		r.OK, r.Step, r.Error, r.State = false, step, err.Error(), Classify(err)
		var pe *ProviderError
		if errors.As(err, &pe) {
			r.Message, r.Action = pe.Message, pe.Action
		}
		r.DurationMS = time.Since(start).Milliseconds()
		return r
	}
	f := run.Age
	if f == nil {
		r.State, r.Step, r.Error = StateNotEncrypted, "encrypt", "encryption is switched off: a target an addon provides gets encrypted backups only"
		return r
	}
	r.Name = f.Name
	c := NewProviderClient(t.Addon.ID)
	src := uploadSource(t, all)
	// the local target holds tonight's file when its own delivery of this run verified it
	fromLocal := false
	if src != nil {
		if lr, ok := ReadResult(p.Store.SecretDir(src.ID)); ok && lr.OK && lr.SHA256 == f.SHA256 && lr.Name == f.Name {
			fromLocal = true
		}
	}
	open := func() (io.ReadCloser, error) {
		if fromLocal {
			if rc, err := p.openLocal(ctx, *src, f.Name); err == nil {
				return rc, nil
			} else {
				p.log().Warn("backup: the local target's copy could not be opened, uploading the working copy", "target", t.ID, "from", src.ID, "err", err)
				fromLocal = false
			}
		}
		return os.Open(f.Path)
	}
	put, err := p.putFile(ctx, c, t, f.Name, f.Size, f.SHA256, open)
	if err != nil {
		return fail("upload", err)
	}
	if fromLocal {
		r.From = src.Name
	}
	files, err := c.List(ctx, t.Addon.Destination, t.Addon.Folder)
	if err != nil {
		return fail("list", err)
	}
	found := false
	for _, x := range files {
		if x.Name == f.Name && x.Size == f.Size {
			found = true
		}
	}
	if !found {
		return fail("list", providerErr(StateError, "the addon says the upload is done, but its list does not show %s with %d bytes", f.Name, f.Size))
	}
	r.OK, r.State, r.SHA256, r.Size, r.Verified = true, StateWritable, f.SHA256, f.Size, put.Verified
	if src != nil {
		sent := p.catchUp(ctx, c, t, *src, host, f.Name, files)
		r.CaughtUp = sent
		if len(sent) > 0 {
			if again, err := c.List(ctx, t.Addon.Destination, t.Addon.Folder); err == nil {
				files = again
			}
		}
	}
	if !t.AppendOnly {
		for _, n := range Expired(byNameTime(files), host, t.MaxBackups) {
			if n == f.Name {
				continue
			}
			if err := c.Delete(ctx, t.Addon.Destination, t.Addon.Folder, n); err == nil {
				r.Removed = append(r.Removed, n)
			}
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	return r
}

// catchUp sends the newest of this system's encrypted backups on the local target that the
// destination lacks - the nights the upload failed or the network was away - newest first, at most
// the number the target keeps (and CatchUpMax), stopping at the first failure. It answers what it
// sent.
func (p *Pipeline) catchUp(ctx context.Context, c *ProviderClient, t, src Target, host, tonight string, remote []BackupFile) []string {
	local, err := p.listLocal(ctx, src)
	if err != nil {
		p.log().Warn("backup: no catch-up, the local target could not be listed", "target", t.ID, "from", src.ID, "err", err)
		return nil
	}
	have := map[string]bool{tonight: true}
	for _, x := range remote {
		have[x.Name] = true
	}
	var own []BackupFile
	for _, x := range local {
		if IsProviderName(x.Name) && OwnBackup(x.Name, host) {
			own = append(own, x)
		}
	}
	own = byNameTime(own)
	keep := t.MaxBackups
	if keep <= 0 || keep > CatchUpMax {
		keep = CatchUpMax
	}
	if len(own) > keep {
		own = own[:keep]
	}
	var sent []string
	for _, x := range own {
		if have[x.Name] || x.Size <= 0 || ctx.Err() != nil {
			continue
		}
		rc, err := p.openLocal(ctx, src, x.Name)
		if err != nil {
			p.log().Warn("backup: catch-up stopped, a file on the local target could not be opened", "target", t.ID, "file", x.Name, "err", err)
			break
		}
		sum, size, err := hashReader(rc)
		rc.Close()
		if err != nil || size != x.Size {
			p.log().Warn("backup: catch-up stopped, a file on the local target could not be read", "target", t.ID, "file", x.Name, "err", err)
			break
		}
		name := x.Name
		if _, err := p.putFile(ctx, c, t, name, size, sum, func() (io.ReadCloser, error) { return p.openLocal(ctx, src, name) }); err != nil {
			p.log().Warn("backup: catch-up stopped, an upload failed", "target", t.ID, "file", name, "err", err)
			break
		}
		p.log().Info("backup: an older backup caught up", "target", t.ID, "file", name, "from", src.ID)
		sent = append(sent, name)
	}
	sort.Strings(sent)
	return sent
}

// --- the daemon's side ---

// Provider is an installed addon that declares backup.targets, from its stored manifest.
type Provider struct {
	// Addon is its id, Name its name.
	Addon string `json:"addon"`
	Name  string `json:"name"`
	// Label is the kind's word ("Cloud storage"), Setup the in-app path where destinations are made.
	Label ProviderText `json:"label"`
	Setup string       `json:"setup"`
}

// ProviderView is a provider for GET /backup/targets: the declaration, whether it answers, and its
// destinations.
type ProviderView struct {
	Provider
	Available    bool          `json:"available"`
	Destinations []Destination `json:"destinations"`
}

// TargetProvider is an addon target's provider as its card shows it.
type TargetProvider struct {
	Addon string       `json:"addon"`
	Name  string       `json:"name,omitempty"`
	Label ProviderText `json:"label,omitempty"`
	// Installed: the addon is installed and declares the extension.
	Installed bool `json:"installed"`
	// Destination is the destination as the provider answers it now (nil: not answered, or gone).
	Destination *Destination `json:"destination,omitempty"`
	// UploadFrom is the name of the local target the next upload reads from, "" = the working copy.
	UploadFrom string `json:"upload_from,omitempty"`
}

type infoEntry struct {
	at   time.Time
	info ProviderInfo
	err  error
}

// providerInfoTTL is how long an answer of GET /v1/info serves the views.
const providerInfoTTL = 5 * time.Second

// providerInfo asks a provider for its destinations, at most once per providerInfoTTL.
func (m *Manager) providerInfo(ctx context.Context, addon string, fresh bool) (ProviderInfo, error) {
	m.mu.Lock()
	if e, ok := m.infos[addon]; ok && !fresh && m.now().Sub(e.at) < providerInfoTTL {
		m.mu.Unlock()
		return e.info, e.err
	}
	m.mu.Unlock()
	// not the request's own context: a page that reloads cancels its request, and the answer of a
	// cancelled question, kept for the next seconds, would show every target of the addon as failed
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancel()
	info, err := NewProviderClient(addon).Info(ctx)
	m.mu.Lock()
	if m.infos == nil {
		m.infos = map[string]infoEntry{}
	}
	m.infos[addon] = infoEntry{at: m.now(), info: info, err: err}
	m.mu.Unlock()
	return info, err
}

func (m *Manager) providers() []Provider {
	if m.Providers == nil {
		return nil
	}
	return m.Providers()
}

func (m *Manager) provider(addon string) (Provider, bool) {
	for _, p := range m.providers() {
		if p.Addon == addon {
			return p, true
		}
	}
	return Provider{}, false
}

// ProviderViews answers the installed providers with their destinations. fresh asks each provider
// now instead of taking an answer of the last seconds: the addon's page has just handed over an
// account it made a moment ago.
func (m *Manager) ProviderViews(ctx context.Context, fresh bool) []ProviderView {
	out := []ProviderView{}
	for _, p := range m.providers() {
		v := ProviderView{Provider: p, Destinations: []Destination{}}
		if info, err := m.providerInfo(ctx, p.Addon, fresh); err == nil {
			v.Available, v.Destinations = true, info.Destinations
		}
		out = append(out, v)
	}
	return out
}

// addonView fills an addon target's state in: addon-missing when the provider is not installed or
// does not answer, the destination's own state when it is a failure (a lapsed login), an error
// when the destination is gone; and the running upload's progress.
func (m *Manager) addonView(ctx context.Context, t Target, v *View, all []Target) {
	if t.Addon == nil {
		return
	}
	tp := &TargetProvider{Addon: t.Addon.ID}
	v.Provider = tp
	_, tp.UploadFrom = UploadSource(t, all)
	if pr, ok := m.Store.progress(t.ID, m.now()); ok {
		v.State.Progress = &pr
	}
	p, installed := m.provider(t.Addon.ID)
	tp.Installed, tp.Name, tp.Label = installed, p.Name, p.Label
	running := v.State.State == StateRunning
	set := func(state, detail string) {
		if !running {
			v.State.State, v.State.Detail = state, detail
		}
	}
	if !installed {
		set(StateAddonMissing, "the addon "+t.Addon.ID+" is not installed")
		return
	}
	info, err := m.providerInfo(ctx, t.Addon.ID, false)
	if err != nil {
		st, _ := providerState(err)
		if st == "" {
			st = StateError
		}
		set(st, err.Error())
		return
	}
	for i := range info.Destinations {
		if d := info.Destinations[i]; d.ID == t.Addon.Destination {
			tp.Destination = &d
			switch {
			case d.State != StateWritable:
				set(d.State, d.Message["en"])
				v.State.Message, v.State.Action = d.Message, d.Action
			case v.State.State == StateAddonMissing, v.State.State == StateAuthFailed,
				v.State.State == StateNotEncrypted && m.Crypt != nil && m.Crypt.Enabled():
				// a state only the provider's presence, the login or the recovery key decided,
				// and that is in order again: the next run tells the rest
				set(StateIdle, "")
			case Failed(v.State.State) && v.LastBackup != nil && !v.LastBackup.OK:
				// the last run's failure stands until a run, a test or a recheck succeeds; its
				// sentence and action come from the result
				v.State.Message, v.State.Action = v.LastBackup.Message, v.LastBackup.Action
			}
			return
		}
	}
	set(StateError, "the account this target uses no longer exists in the addon "+t.Addon.ID)
	v.State.Gone = true
}

func (s *Store) progress(id string, now time.Time) (Progress, bool) {
	return ReadProgress(s.SecretDir(id), now)
}

// testAddon is the write test through the provider.
func (m *Manager) testAddon(ctx context.Context, t Target) TestResult {
	if m.Crypt == nil || !m.Crypt.Enabled() {
		return TestResult{Step: "encrypt", State: StateNotEncrypted, Error: "encryption is switched off: a target an addon provides gets encrypted backups only"}
	}
	res, err := NewProviderClient(t.Addon.ID).Test(ctx, t.Addon.Destination, t.Addon.Folder)
	if err != nil {
		tr := TestResult{Step: "test", State: Classify(err), Error: err.Error()}
		var pe *ProviderError
		if errors.As(err, &pe) {
			tr.Message, tr.Action = pe.Message, pe.Action
		}
		return tr
	}
	return TestResult{OK: true, State: StateWritable, FreeBytes: res.FreeBytes, TotalBytes: res.TotalBytes}
}

// recheckAddon is the retry's light check of an addon target: the provider answers and the
// destination is there and fine.
func (m *Manager) recheckAddon(ctx context.Context, t Target) Check {
	c := Check{At: m.now(), State: StateIdle, Step: "retry"}
	if _, ok := m.provider(t.Addon.ID); !ok {
		c.State, c.Detail = StateAddonMissing, "the addon "+t.Addon.ID+" is not installed"
		return c
	}
	info, err := m.providerInfo(ctx, t.Addon.ID, true)
	if err != nil {
		c.State, c.Detail = Classify(err), err.Error()
		return c
	}
	for _, d := range info.Destinations {
		if d.ID == t.Addon.Destination {
			if d.State != StateWritable {
				c.State, c.Detail = d.State, d.Message["en"]
			}
			return c
		}
	}
	c.State, c.Detail = StateError, "the account this target uses no longer exists in the addon "+t.Addon.ID
	return c
}

// UsedBy answers the names of the targets an addon provides: the Addons page names them before an
// uninstall.
func (m *Manager) UsedBy(addon string) []string {
	list, err := m.Store.List()
	if err != nil {
		return nil
	}
	out := []string{}
	for _, t := range list {
		if t.Kind == KindAddon && t.Addon != nil && t.Addon.ID == addon {
			out = append(out, t.Name)
		}
	}
	return out
}

// checkAddonTarget is what saving an addon target needs beyond its shape: a recovery key, and an
// installed provider.
func (m *Manager) CheckAddonTarget(t Target) error {
	if t.Kind != KindAddon || t.Addon == nil {
		return nil
	}
	if m.Crypt == nil || !m.Crypt.Enabled() {
		return ErrNeedsEncryption
	}
	if _, ok := m.provider(t.Addon.ID); !ok {
		return fmt.Errorf("%w: addon.id: no installed addon %q provides backup targets", ErrInvalid, t.Addon.ID)
	}
	return nil
}

// ErrNeedsEncryption: an addon target cannot be saved without a recovery key.
var ErrNeedsEncryption = errors.New("a target an addon provides takes encrypted backups only: set up backup encryption first")
