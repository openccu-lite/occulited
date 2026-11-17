package backuptarget

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProvider is a provider on a Unix socket: files in memory per destination and folder, and
// knobs for every way a provider can answer wrongly.
type fakeProvider struct {
	t      *testing.T
	socket string
	srv    *http.Server

	mu    sync.Mutex
	dests []Destination
	files map[string]map[string][]byte // dest|folder -> name -> content
	times map[string]time.Time
	puts  []string
	dels  []string
	// knobs
	putState  string        // answer this state instead of storing
	putFails  int           // fail the first n puts with unreachable
	wrongSize bool          // answer a size one byte short
	hang      time.Duration // read nothing of a put for this long
	huge      bool          // answer more than the limit to info
	protocol  int
	testState string
	noList    bool // puts are not shown by list (a lying provider)
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	dir, err := os.MkdirTemp("", "bp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeProvider{t: t, socket: filepath.Join(dir, ProviderSocketName), files: map[string]map[string][]byte{}, times: map[string]time.Time{}, protocol: ProviderProtocol,
		dests: []Destination{{ID: "acc1", Name: "OneDrive", Detail: "OneDrive · max@example.org", State: StateWritable}}}
	l, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/info", f.info)
	mux.HandleFunc("POST /v1/destinations/{id}/test", f.test)
	mux.HandleFunc("PUT /v1/destinations/{id}/files/{name}", f.put)
	mux.HandleFunc("GET /v1/destinations/{id}/files", f.list)
	mux.HandleFunc("GET /v1/destinations/{id}/files/{name}", f.get)
	mux.HandleFunc("DELETE /v1/destinations/{id}/files/{name}", f.del)
	f.srv = &http.Server{Handler: mux}
	go func() { _ = f.srv.Serve(l) }()
	t.Cleanup(func() { f.srv.Close() })
	old := NewProviderClient
	NewProviderClient = func(addon string) *ProviderClient {
		return &ProviderClient{Addon: addon, Socket: f.socket, OwnerOK: func(uint32) bool { return true }, Stall: 400 * time.Millisecond}
	}
	t.Cleanup(func() { NewProviderClient = old })
	return f
}

func (f *fakeProvider) client() *ProviderClient { return NewProviderClient("sync") }

func (f *fakeProvider) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeProvider) key(r *http.Request) string {
	return r.PathValue("id") + "|" + r.URL.Query().Get("folder")
}

func (f *fakeProvider) info(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.huge {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"protocol":1,"destinations":[],"pad":"` + strings.Repeat("x", providerMaxAnswer+10) + `"}`))
		return
	}
	f.json(w, 200, map[string]any{"protocol": f.protocol, "destinations": f.dests})
}

func (f *fakeProvider) test(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.testState != "" {
		f.json(w, 200, map[string]any{"state": f.testState, "message": map[string]string{"en": "OneDrive no longer accepts this system's login.", "de": "OneDrive nimmt die Anmeldung dieses Systems nicht mehr an."},
			"action": map[string]any{"label": map[string]string{"en": "Log in again", "de": "Erneut anmelden"}, "href": "/addon-settings/sync#accounts"}})
		return
	}
	f.json(w, 200, map[string]any{"state": StateWritable, "free_bytes": 4_100_000_000, "total_bytes": 5_000_000_000})
}

func (f *fakeProvider) put(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hang, state, fails := f.hang, f.putState, f.putFails
	if fails > 0 {
		f.putFails--
	}
	f.mu.Unlock()
	if hang > 0 {
		time.Sleep(hang)
	}
	if fails > 0 {
		_, _ = io.Copy(io.Discard, r.Body)
		f.json(w, 200, map[string]any{"state": StateUnreachable, "message": map[string]string{"en": "The cloud did not answer."}})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != r.Header.Get("X-SHA256") || int64(len(body)) != r.ContentLength {
		f.json(w, 200, map[string]any{"state": StateError, "message": map[string]string{"en": "The file arrived damaged."}})
		return
	}
	if state != "" {
		f.json(w, 200, map[string]any{"state": state})
		return
	}
	f.mu.Lock()
	k := f.key(r)
	if f.files[k] == nil {
		f.files[k] = map[string][]byte{}
	}
	f.files[k][r.PathValue("name")] = body
	f.times[k+"|"+r.PathValue("name")] = time.Now()
	f.puts = append(f.puts, r.PathValue("name"))
	size := int64(len(body))
	if f.wrongSize {
		size--
	}
	f.mu.Unlock()
	f.json(w, 200, map[string]any{"state": StateWritable, "size": size, "verified": "hash", "duration_ms": 5})
}

func (f *fakeProvider) list(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type file struct {
		Name string    `json:"name"`
		Size int64     `json:"size"`
		Time time.Time `json:"time"`
	}
	out := []file{{Name: "../../etc/passwd", Size: 1}, {Name: "notes.txt", Size: 5}}
	if !f.noList {
		for n, b := range f.files[f.key(r)] {
			out = append(out, file{Name: n, Size: int64(len(b)), Time: f.times[f.key(r)+"|"+n]})
		}
	}
	f.json(w, 200, map[string]any{"files": out})
}

func (f *fakeProvider) get(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	b, ok := f.files[f.key(r)][r.PathValue("name")]
	f.mu.Unlock()
	if !ok {
		f.json(w, 404, map[string]any{"state": StateError, "message": map[string]string{"en": "No such file."}})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(b)
}

func (f *fakeProvider) del(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	delete(f.files[f.key(r)], r.PathValue("name"))
	f.dels = append(f.dels, r.PathValue("name"))
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeProvider) names(dest, folder string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.files[dest+"|"+folder] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (f *fakeProvider) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func sumOf(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestProviderConformanceAgainstTheFake(t *testing.T) {
	f := newFakeProvider(t)
	var steps []string
	if err := ProviderConformance(context.Background(), f.client(), "acc1", "openccu-lite/box", func(s string) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if len(steps) < 6 || len(f.names("acc1", "openccu-lite/box")) != 0 {
		t.Fatalf("steps %v, left %v", steps, f.names("acc1", "openccu-lite/box"))
	}
	if err := ProviderConformance(context.Background(), f.client(), "nope", "x", nil); err == nil {
		t.Fatal("an unknown destination passed")
	}
}

func TestProviderClientLimits(t *testing.T) {
	f := newFakeProvider(t)
	ctx := context.Background()
	c := f.client()
	body := bytes.Repeat([]byte("backup"), 5000)
	name := "box-1.0.0-2026-11-17-0007.sbk.age"
	put := func() error {
		_, err := c.Put(ctx, "acc1", "f", name, bytes.NewReader(body), int64(len(body)), sumOf(body), nil)
		return err
	}
	// only an encrypted backup's name ever leaves the system
	for _, bad := range []string{"box-1.0.0-2026-11-17-0007.sbk", "../x.sbk.age", ".hidden.sbk.age", "a/b.sbk.age", "notes.txt"} {
		if _, err := c.Put(ctx, "acc1", "f", bad, bytes.NewReader(body), int64(len(body)), sumOf(body), nil); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	var sent int64
	if _, err := c.Put(ctx, "acc1", "f", name, bytes.NewReader(body), int64(len(body)), sumOf(body), func(n int64) { sent = n }); err != nil || sent != int64(len(body)) {
		t.Fatalf("put: %v, progress %d", err, sent)
	}
	// the list drops what is not a backup's name
	files, err := c.List(ctx, "acc1", "f")
	if err != nil || len(files) != 1 || files[0].Name != name || !files[0].Encrypted {
		t.Fatalf("%v %+v", err, files)
	}
	if when, _ := NameTime(name); !files[0].Time.Equal(when) {
		t.Fatalf("the file's time is not its name's: %v", files[0].Time)
	}
	rc, _, err := c.Get(ctx, "acc1", "f", name)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(back, body) {
		t.Fatal("get differs")
	}
	if _, _, err := c.Get(ctx, "acc1", "f", "box-1.0.0-2026-11-16-0007.sbk.age"); Classify(err) != StateError {
		t.Fatalf("a missing file: %v", err)
	}
	// a wrong size, an unknown state, a state that is a failure
	f.set(func() { f.wrongSize = true })
	if err := put(); Classify(err) != StateError || !strings.Contains(err.Error(), "bytes of") {
		t.Fatalf("wrong size: %v", err)
	}
	f.set(func() { f.wrongSize, f.putState = false, "splendid" })
	if err := put(); Classify(err) != StateError || !strings.Contains(err.Error(), "does not know") {
		t.Fatalf("unknown state: %v", err)
	}
	f.set(func() { f.putState = StateFull })
	if err := put(); Classify(err) != StateFull {
		t.Fatalf("full: %v", err)
	}
	// a provider that takes nothing is given up after the stall limit
	f.set(func() { f.putState, f.hang = "", 3*time.Second })
	big := bytes.Repeat([]byte("x"), 8<<20)
	start := time.Now()
	_, err = c.Put(ctx, "acc1", "f", name, bytes.NewReader(big), int64(len(big)), sumOf(big), nil)
	if Classify(err) != StateError || time.Since(start) > 2500*time.Millisecond {
		t.Fatalf("stall: %v after %s", err, time.Since(start))
	}
	f.set(func() { f.hang = 0 })
	// an answer over the limit, another protocol
	f.set(func() { f.huge = true })
	if _, err := c.Info(ctx); Classify(err) != StateError {
		t.Fatalf("huge: %v", err)
	}
	f.set(func() { f.huge, f.protocol = false, 2 })
	if _, err := c.Info(ctx); Classify(err) != StateError || !strings.Contains(err.Error(), "protocol 2") {
		t.Fatalf("protocol: %v", err)
	}
	// info: a destination with a bad id or an unknown state, an action that leads off the system
	f.set(func() {
		f.protocol = 1
		f.dests = []Destination{{ID: "ok_1", Name: " Privat\n ", State: StateAuthFailed, Message: ProviderText{"en": "Login lapsed.", "fr": "x"},
			Action: &ProviderAction{Label: ProviderText{"en": "Log in again"}, Href: "/addon-settings/sync#accounts"}},
			{ID: "../x", State: StateWritable}, {ID: "odd", State: "great", Action: &ProviderAction{Label: ProviderText{"en": "x"}, Href: "https://evil.example/"}},
			{ID: "ext", State: StateFull, Action: &ProviderAction{Label: ProviderText{"en": "x"}, Href: "//evil.example/"}}}
	})
	info, err := c.Info(ctx)
	if err != nil || len(info.Destinations) != 3 {
		t.Fatalf("%v %+v", err, info)
	}
	if d := info.Destinations[0]; d.Name != "Privat" || d.State != StateAuthFailed || d.Message["fr"] != "" || d.Action == nil {
		t.Fatalf("%+v", d)
	}
	if d := info.Destinations[1]; d.State != StateError || d.Action != nil {
		t.Fatalf("%+v", d)
	}
	if d := info.Destinations[2]; d.Action != nil {
		t.Fatalf("an action off the system kept: %+v", d)
	}
	// the test's refusal carries the provider's sentence and action
	f.set(func() { f.testState = StateAuthFailed })
	_, err = c.Test(ctx, "acc1", "f")
	var pe *ProviderError
	if !errors.As(err, &pe) || pe.State != StateAuthFailed || pe.Message["de"] == "" || pe.Action == nil || pe.Action.Href != "/addon-settings/sync#accounts" {
		t.Fatalf("%v %+v", err, pe)
	}
	// no socket, a file that is no socket, a socket that is not the addon's, nobody listening
	gone := &ProviderClient{Addon: "sync", Socket: filepath.Join(t.TempDir(), "none.sock")}
	if _, err := gone.Info(ctx); !errors.Is(err, ErrAddonMissing) || Classify(err) != StateAddonMissing {
		t.Fatalf("missing: %v", err)
	}
	plain := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(plain, nil, 0o600)
	if err := (&ProviderClient{Addon: "sync", Socket: plain}).Reachable(ctx); !errors.Is(err, ErrAddonMissing) {
		t.Fatalf("no socket: %v", err)
	}
	foreign := &ProviderClient{Addon: "sync", Socket: f.socket, OwnerOK: func(uint32) bool { return false }}
	if _, err := foreign.Info(ctx); !errors.Is(err, ErrAddonMissing) {
		t.Fatalf("foreign: %v", err)
	}
	f.srv.Close()
	if err := c.Reachable(ctx); !errors.Is(err, ErrAddonMissing) {
		t.Fatalf("closed: %v", err)
	}
}

func TestProviderDirAndFolder(t *testing.T) {
	base := filepath.Join(t.TempDir(), "backup-targets")
	if err := ProviderDir(base, "sync"); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(base, "sync", ProviderSocketName)
	_ = os.WriteFile(stale, nil, 0o600)
	if err := ProviderDir(base, "sync"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a socket a previous run left stays")
	}
	if ProviderDir(base, "../x") == nil {
		t.Fatal("a path as an id")
	}
	for f, want := range map[string]bool{"openccu-lite/ccu-keller": true, "Backups 2026/ccu": true, "": false, "../x": false, "a//b": false, ".hidden/x": false, "a/b/c/d/e/f/g": false, "x ": false, "a\\b": false} {
		if ValidProviderFolder(f) != want {
			t.Errorf("%q: want %v", f, want)
		}
	}
}

func TestAddonTargetValidation(t *testing.T) {
	st := &Store{Dir: t.TempDir(), Root: t.TempDir()}
	ok := Target{Kind: KindAddon, Name: "OneDrive", Enabled: true, MaxBackups: 30, Addon: &AddonRef{ID: "sync", Destination: "acc1", Folder: "openccu-lite/box"}}
	saved, err := st.Put(ok)
	if err != nil || !saved.Encrypt || saved.ID == "" {
		t.Fatalf("%v %+v", err, saved)
	}
	for name, bad := range map[string]Target{
		"folder":      {Kind: KindAddon, Name: "x", Addon: &AddonRef{ID: "sync", Destination: "acc1", Folder: "../up"}},
		"destination": {Kind: KindAddon, Name: "x", Addon: &AddonRef{ID: "sync", Destination: "a/b", Folder: "f"}},
		"addon":       {Kind: KindAddon, Name: "x", Addon: &AddonRef{ID: "Sync!", Destination: "a", Folder: "f"}},
		"subdir":      {Kind: KindAddon, Name: "x", Subdir: "box", Addon: &AddonRef{ID: "sync", Destination: "a", Folder: "f"}},
		"source":      {Kind: KindAddon, Name: "x", Addon: &AddonRef{ID: "sync", Destination: "a", Folder: "f", Source: "tnothere"}},
		"missing":     {Kind: KindAddon, Name: "x"},
	} {
		if _, err := st.Put(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// the source may be the working copy or a local target that exists
	ok.Addon.Source = SourceStaging
	if _, err := st.Put(ok); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(Target{Kind: KindDirectory, Name: "USB", Enabled: true, Encrypt: true, Directory: &Directory{Path: "/media/usb1/backup"}}); err != nil {
		t.Fatal(err)
	}
	ok.Addon.Source = DirectoryID
	if _, err := st.Put(ok); err != nil {
		t.Fatal(err)
	}
}

func addonRig(t *testing.T, source string) (*pipeRig, *fakeProvider, Target) {
	t.Helper()
	r := newPipeRig(t, true)
	// the SFTP target of the rig is not part of these runs
	if err := r.p.Store.Delete(r.sftp.ID); err != nil {
		t.Fatal(err)
	}
	f := newFakeProvider(t)
	tg, err := r.p.Store.Put(Target{Kind: KindAddon, Name: "OneDrive", Enabled: true, MaxBackups: 2, Addon: &AddonRef{ID: "sync", Destination: "acc1", Folder: "openccu-lite/lab-box", Source: source}})
	if err != nil {
		t.Fatal(err)
	}
	r.p.Retry = []time.Duration{time.Millisecond, time.Millisecond}
	return r, f, tg
}

func TestPipelineAddonTargetFromTheLocalTarget(t *testing.T) {
	r, f, tg := addonRig(t, "")
	ctx := context.Background()
	// the stick holds two older encrypted backups the cloud never got, a plain one and another system's
	_ = os.MkdirAll(r.usb, 0o755)
	for _, n := range []string{"lab-box-1.0.0-2026-09-22-0007.sbk.age", "lab-box-1.0.0-2026-09-23-0007.sbk.age", "lab-box-1.0.0-2026-09-21-0007.sbk", "other-1-2026-09-23-0007.sbk.age"} {
		_ = os.WriteFile(filepath.Join(r.usb, n), []byte("old "+n), 0o644)
	}
	// and the cloud an old one of this system, beyond what it keeps
	f.files["acc1|openccu-lite/lab-box"] = map[string][]byte{"lab-box-1.0.0-2026-09-01-0007.sbk.age": []byte("ancient")}
	if err := r.p.CreateRun(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	// the stick's own retention (keep 2) removed the 22nd's; the 23rd's is what a catch-up can send
	if err := r.p.DeliverRun(ctx, "all"); err != nil {
		t.Fatal(err)
	}
	name := "lab-box-1.0.0-2026-09-24-0007.sbk.age"
	res, ok := ReadResult(r.p.Store.SecretDir(tg.ID))
	if !ok || !res.OK || res.State != StateWritable || res.Name != name || res.From != "USB" || res.Verified != "hash" || !res.Encrypted {
		t.Fatalf("%+v", res)
	}
	if len(res.CaughtUp) != 1 || res.CaughtUp[0] != "lab-box-1.0.0-2026-09-23-0007.sbk.age" {
		t.Fatalf("caught up: %v", res.CaughtUp)
	}
	// keep 2: tonight's and the caught-up one; the ancient one is removed, although it was "uploaded" first
	got := f.names("acc1", "openccu-lite/lab-box")
	if len(got) != 2 || got[0] != "lab-box-1.0.0-2026-09-23-0007.sbk.age" || got[1] != name {
		t.Fatalf("on the destination: %v (removed %v)", got, res.Removed)
	}
	if b := f.files["acc1|openccu-lite/lab-box"][name]; !bytes.Equal(r.open(t, filepath.Join(r.usb, name)), r.content) || len(b) == 0 {
		t.Fatal("the upload is not the backup")
	}
	if _, err := os.Stat(filepath.Join(r.p.Store.SecretDir(tg.ID), ProgressFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the progress file stays")
	}
	// the created list names both targets for the file
	if !r.p.Crypt.CreatedHere(res.SHA256) {
		t.Fatal("not in the created list")
	}
}

func TestPipelineAddonTargetAloneRetriesAndFails(t *testing.T) {
	r, f, tg := addonRig(t, SourceStaging)
	ctx := context.Background()
	if err := r.p.Store.Delete(DirectoryID); err != nil {
		t.Fatal(err)
	}
	// two failed attempts, the third passes
	f.set(func() { f.putFails = 2 })
	if err := r.p.CreateRun(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.p.DeliverRun(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	res, _ := ReadResult(r.p.Store.SecretDir(tg.ID))
	if !res.OK || res.From != "" || len(f.names("acc1", "openccu-lite/lab-box")) != 1 {
		t.Fatalf("%+v", res)
	}
	// a full destination is not retried; the provider's state is the result's
	f.set(func() { f.putState, f.puts = StateFull, nil })
	r.p.Now = func() time.Time { return time.Date(2026, 9, 25, 0, 7, 0, 0, time.UTC) }
	if err := r.p.CreateRun(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.p.DeliverRun(ctx, tg.ID); err == nil {
		t.Fatal("a failed upload did not fail the deliver half")
	}
	res, _ = ReadResult(r.p.Store.SecretDir(tg.ID))
	if res.OK || res.State != StateFull || res.LastOK == nil {
		t.Fatalf("%+v", res)
	}
	// a provider that says done but does not list the file
	f.set(func() { f.putState, f.noList = "", true })
	r.p.Now = func() time.Time { return time.Date(2026, 9, 26, 0, 7, 0, 0, time.UTC) }
	_ = r.p.CreateRun(ctx, tg.ID)
	_ = r.p.DeliverRun(ctx, tg.ID)
	res, _ = ReadResult(r.p.Store.SecretDir(tg.ID))
	if res.OK || res.State != StateError || res.Step != "list" {
		t.Fatalf("%+v", res)
	}
	// the addon gone: found before the backup is made, nothing staged
	f.srv.Close()
	r.p.Now = func() time.Time { return time.Date(2026, 9, 27, 0, 7, 0, 0, time.UTC) }
	if err := r.p.CreateRun(ctx, tg.ID); err == nil {
		t.Fatal("a missing addon did not fail the run")
	}
	res, _ = ReadResult(r.p.Store.SecretDir(tg.ID))
	if res.State != StateAddonMissing || res.Step != "addon" {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(r.p.Staging, RunFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a backup was made for a target that cannot take it")
	}
}

func TestPipelineAddonTargetNeverPlain(t *testing.T) {
	r, f, tg := addonRig(t, "")
	ctx := context.Background()
	if _, err := r.p.Crypt.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	// encryption switched off: the stick gets its (plain) backup, the addon's target nothing
	err := r.p.CreateRun(ctx, "all")
	if err == nil || !strings.Contains(err.Error(), tg.ID) {
		t.Fatalf("%v", err)
	}
	_ = r.p.DeliverRun(ctx, "all")
	res, _ := ReadResult(r.p.Store.SecretDir(tg.ID))
	if res.OK || res.State != StateNotEncrypted {
		t.Fatalf("%+v", res)
	}
	if len(f.puts) != 0 {
		t.Fatalf("a plain backup left the system: %v", f.puts)
	}
	if usb, _ := ReadResult(r.p.Store.SecretDir(DirectoryID)); !usb.OK {
		t.Fatalf("the stick did not get its backup: %+v", usb)
	}
}

func TestManagerAddonTarget(t *testing.T) {
	r, f, tg := addonRig(t, "")
	ctx := context.Background()
	installed := true
	m := &Manager{Store: r.p.Store, Crypt: r.p.Crypt, Hostname: func() string { return "lab-box" },
		Providers: func() []Provider {
			if !installed {
				return nil
			}
			return []Provider{{Addon: "sync", Name: "Sync", Label: ProviderText{"de": "Cloud-Speicher", "en": "Cloud storage"}, Setup: "/addon-settings/sync#accounts"}}
		}}
	clock := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return clock }
	tick := func() { clock = clock.Add(time.Minute) }
	v, err := m.View(ctx, tg.ID)
	if err != nil || v.State.State != StateIdle || v.Provider == nil || !v.Provider.Installed || v.Provider.Destination == nil || v.Provider.Destination.Detail != "OneDrive · max@example.org" || v.Provider.UploadFrom != "USB" {
		t.Fatalf("%v %+v %+v", err, v.State, v.Provider)
	}
	pv := m.ProviderViews(ctx, false)
	if len(pv) != 1 || !pv[0].Available || len(pv[0].Destinations) != 1 || pv[0].Label["de"] != "Cloud-Speicher" {
		t.Fatalf("%+v", pv)
	}
	// a request the browser gave up (a reload) does not leave a failure behind for the next one
	gone, cancel := context.WithCancel(ctx)
	cancel()
	tick()
	if v, _ := m.View(gone, tg.ID); v.State.State != StateIdle {
		t.Fatalf("a cancelled request: %+v", v.State)
	}
	if v, _ := m.View(ctx, tg.ID); v.State.State != StateIdle {
		t.Fatalf("after a cancelled request: %+v", v.State)
	}
	// an account made a moment ago is there for a fresh look, not within the cached answer
	f.set(func() { f.dests = append(f.dests, Destination{ID: "acc2", Name: "Haus", State: StateWritable}) })
	if pv = m.ProviderViews(ctx, false); len(pv[0].Destinations) != 1 {
		t.Fatalf("the cached answer was not used: %+v", pv)
	}
	if pv = m.ProviderViews(ctx, true); len(pv[0].Destinations) != 2 {
		t.Fatalf("a fresh look: %+v", pv)
	}
	f.set(func() { f.dests = f.dests[:1] })
	tick()
	if m.Kinds()[KindAddon] != "" {
		t.Fatal("the kind is not offered")
	}
	// the test, and the backups there
	if res, err := m.Test(ctx, tg.ID); err != nil || !res.OK || res.FreeBytes == 0 {
		t.Fatalf("%v %+v", err, res)
	}
	if err := r.p.CreateRun(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.p.DeliverRun(ctx, tg.ID); err != nil {
		t.Fatal(err)
	}
	files, err := m.Backups(ctx, tg.ID)
	if err != nil || len(files) != 1 || files[0].Known != "current" {
		t.Fatalf("%v %+v", err, files)
	}
	rc, err := m.Open(ctx, tg.ID, files[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if len(b) == 0 {
		t.Fatal("nothing read for a restore")
	}
	if u := m.UsedBy("sync"); len(u) != 1 || u[0] != "OneDrive" {
		t.Fatalf("%v", u)
	}
	// a lapsed login: the destination's state, sentence and action are the card's
	tick()
	f.set(func() {
		f.dests[0].State = StateAuthFailed
		f.dests[0].Message = ProviderText{"en": "OneDrive no longer accepts this system's login."}
		f.dests[0].Action = &ProviderAction{Label: ProviderText{"en": "Log in again"}, Href: "/addon-settings/sync#accounts"}
	})
	v, _ = m.View(ctx, tg.ID)
	if v.State.State != StateAuthFailed || v.State.Action == nil || v.State.Message["en"] == "" {
		t.Fatalf("%+v", v.State)
	}
	probs, _ := m.Problems(ctx)
	if len(probs) != 1 || probs[0].Cause != StateAuthFailed || probs[0].Addon != "Sync" {
		t.Fatalf("%+v", probs)
	}
	// the account removed in the addon
	tick()
	f.set(func() { f.dests = nil })
	if v, _ = m.View(ctx, tg.ID); v.State.State != StateError || !v.State.Gone {
		t.Fatalf("%+v", v.State)
	}
	// the addon stopped, then uninstalled: addon-missing either way, and the Status says which addon
	tick()
	f.srv.Close()
	if v, _ = m.View(ctx, tg.ID); v.State.State != StateAddonMissing || !v.Provider.Installed {
		t.Fatalf("%+v", v.State)
	}
	installed = false
	if v, _ = m.View(ctx, tg.ID); v.State.State != StateAddonMissing || v.Provider.Installed {
		t.Fatalf("%+v", v.State)
	}
	if res, _ := m.Test(ctx, tg.ID); res.OK || res.State != StateAddonMissing {
		t.Fatalf("%+v", res)
	}
	probs, _ = m.Problems(ctx)
	if len(probs) != 1 || probs[0].Cause != StateAddonMissing {
		t.Fatalf("%+v", probs)
	}
	// saving needs a recovery key and an installed provider
	if err := m.CheckAddonTarget(tg); !errors.Is(err, ErrInvalid) {
		t.Fatalf("%v", err)
	}
	installed = true
	if err := m.CheckAddonTarget(tg); err != nil {
		t.Fatal(err)
	}
	_, _ = r.p.Crypt.SetEnabled(false)
	if err := m.CheckAddonTarget(tg); !errors.Is(err, ErrNeedsEncryption) {
		t.Fatalf("%v", err)
	}
	if res, _ := m.Test(ctx, tg.ID); res.State != StateNotEncrypted {
		t.Fatalf("%+v", res)
	}
}

func TestReadProgress(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	b, _ := json.Marshal(Progress{Name: "x.sbk.age", Size: 100, Sent: 40, At: now})
	_ = os.WriteFile(filepath.Join(dir, ProgressFile), b, 0o600)
	if p, ok := ReadProgress(dir, now); !ok || p.Sent != 40 {
		t.Fatalf("%+v %v", p, ok)
	}
	if _, ok := ReadProgress(dir, now.Add(time.Hour)); ok {
		t.Fatal("a dead process's progress is shown")
	}
}
