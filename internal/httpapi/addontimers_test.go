package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/system"
)

// fakeAddonTimers keeps addons' timers in memory, keyed addon/name, and says who was served.
type fakeAddonTimers struct {
	fakeTimers
	timers map[string]system.AddonTimer
	calls  []string
}

func (f *fakeAddonTimers) AddonTimers(addon string) []system.AddonTimer {
	out := []system.AddonTimer{}
	for k, t := range f.timers {
		if strings.HasPrefix(k, addon+"/") {
			out = append(out, t)
		}
	}
	return out
}
func (f *fakeAddonTimers) AddonTimerReadable(addon string) []string { return []string{"history"} }
func (f *fakeAddonTimers) ReadAddonTimer(addon, name string) (system.AddonTimer, error) {
	t, ok := f.timers[addon+"/"+name]
	if !ok {
		return t, fmt.Errorf("the addon %s has no timer named %s: %w", addon, name, system.ErrLocalTimerNotFound)
	}
	return t, nil
}
func (f *fakeAddonTimers) PutAddonTimer(_ context.Context, addon, name string, in system.AddonTimerInput) (system.AddonTimer, bool, error) {
	f.calls = append(f.calls, "put "+addon+"/"+name)
	switch name {
	case "taken":
		return system.AddonTimer{}, false, fmt.Errorf("an own timer named %s-%s exists already and is not this addon's: %w", addon, name, system.ErrLocalTimerExists)
	case "full":
		return system.AddonTimer{}, false, fmt.Errorf("an addon keeps at most 32 timers: %w", system.ErrAddonTimerLimit)
	case "undeclared":
		return system.AddonTimer{}, false, fmt.Errorf("no runtime.timers: %w", system.ErrAddonTimersUndeclared)
	case "bad":
		return system.AddonTimer{}, false, fmt.Errorf("timer: line 3: Unit= is not allowed in an addon's timer: %w", system.ErrAddonTimerInvalid)
	}
	_, had := f.timers[addon+"/"+name]
	t := system.AddonTimer{Addon: addon, Name: name, Unit: "local-" + addon + "-" + name + ".timer", Service: "local-" + addon + "-" + name + ".service", TimerFile: in.Timer, Enabled: in.Enabled == nil || *in.Enabled, Read: []string{}}
	if in.Description != nil {
		t.Description = *in.Description
	}
	f.timers[addon+"/"+name] = t
	return t, !had, nil
}
func (f *fakeAddonTimers) DeleteAddonTimer(_ context.Context, addon, name string) error {
	if _, err := f.ReadAddonTimer(addon, name); err != nil {
		return err
	}
	delete(f.timers, addon+"/"+name)
	return nil
}
func (f *fakeAddonTimers) RunAddonTimer(_ context.Context, addon, name string) (string, error) {
	_, err := f.ReadAddonTimer(addon, name)
	f.calls = append(f.calls, "run "+addon+"/"+name)
	return "", err
}
func (f *fakeAddonTimers) StopAddonTimer(_ context.Context, addon, name string) (string, error) {
	_, err := f.ReadAddonTimer(addon, name)
	f.calls = append(f.calls, "stop "+addon+"/"+name)
	return "", err
}
func (f *fakeAddonTimers) SetAddonTimerEnabled(_ context.Context, addon, name string, on bool) (string, error) {
	t, err := f.ReadAddonTimer(addon, name)
	if err != nil {
		return "", err
	}
	t.Enabled = on
	f.timers[addon+"/"+name] = t
	return "", nil
}

// occulited task 154: the routes of timers:addon serve an addon's own API token and that addon's
// timers - the addon comes from the token, never from the request - and nobody else: not a token
// without the scope, not another scope's, not an administrator.
func TestAddonTimerRoutes(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.Open(dir, auth.Options{SessionDir: filepath.Join(dir, "s")})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Setup("admin", "secret123"); err != nil {
		t.Fatal(err)
	}
	ft := &fakeAddonTimers{fakeTimers: fakeTimers{own: map[string]system.LocalTimer{}}, timers: map[string]system.AddonTimer{}}
	mux := http.NewServeMux()
	a := &AuthAPI{Store: store}
	a.Register(mux)
	(&SystemAPI{Root: logRoot(t), Timers: ft}).Register(mux)
	srv := httptest.NewServer(a.Middleware(mux))
	t.Cleanup(srv.Close)

	mint := func(id string, scopes ...string) map[string]string {
		t.Helper()
		secret, _, _, err := store.MintAddonToken(id, scopes)
		if err != nil || secret == "" {
			t.Fatal(id, err)
		}
		return map[string]string{"Authorization": "Bearer " + secret}
	}
	sync := mint("sync", "system:read", "timers:addon")
	other := mint("history", "timers:addon")
	without := mint("nut", "system:write")
	admin, err := store.CreateToken("ci", auth.TokenOptions{Scopes: auth.Scopes{auth.ScopeAll}})
	if err != nil {
		t.Fatal(err)
	}
	person, err := store.CreateToken("writer", auth.TokenOptions{Scopes: auth.Scopes{auth.ScopeSystemWrite, auth.ScopeAddonsWrite}})
	if err != nil {
		t.Fatal(err)
	}
	base := "/api/system/v1/addon-timers"
	body := `{"timer":"[Timer]\nOnCalendar=hourly\n","description":"Fotos"}`

	// who is not served
	for who, hdr := range map[string]map[string]string{
		"an addon without the scope": without,
		"a person's system:write":    {"Authorization": "Bearer " + person},
		"an administrator's token":   {"Authorization": "Bearer " + admin},
	} {
		for _, c := range [][2]string{{"GET", base}, {"PUT", base + "/fotos"}, {"POST", base + "/fotos/run"}, {"DELETE", base + "/fotos"}} {
			st, out, _ := do(t, srv, c[0], c[1], body, hdr)
			if st != 403 {
				t.Errorf("%s: %s %s answered %d %v", who, c[0], c[1], st, out)
			}
		}
	}
	if st, out, _ := do(t, srv, "GET", base, "", map[string]string{"Authorization": "Bearer " + admin}); st != 403 || out["error"] != "addon-only" {
		t.Errorf("an administrator: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "GET", base, "", without); st != 403 || out["scope"] != "timers:addon" {
		t.Errorf("an addon without the scope: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "GET", base, "", nil); st != 401 {
		t.Errorf("without a session: %d", st)
	}
	if len(ft.calls) != 0 {
		t.Fatalf("something happened: %v", ft.calls)
	}

	// the addon's own
	st, out, _ := do(t, srv, "PUT", base+"/fotos", body, sync)
	if st != 201 || out["addon"] != "sync" || out["unit"] != "local-sync-fotos.timer" || out["description"] != "Fotos" || out["enabled"] != true {
		t.Errorf("create: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "PUT", base+"/fotos", `{"description":"Fotos 2"}`, sync); st != 200 || out["description"] != "Fotos 2" {
		t.Errorf("change: %d %v", st, out)
	}
	st, out, _ = do(t, srv, "GET", base, "", sync)
	if list, _ := out["timers"].([]any); st != 200 || out["addon"] != "sync" || len(list) != 1 || out["max"] != float64(system.AddonTimersMax) || fmt.Sprint(out["readable"]) != "[history]" {
		t.Errorf("list: %d %v", st, out)
	}
	// the other addon sees and reaches nothing of it
	st, out, _ = do(t, srv, "GET", base, "", other)
	if list, _ := out["timers"].([]any); st != 200 || out["addon"] != "history" || len(list) != 0 {
		t.Errorf("another addon's list: %d %v", st, out)
	}
	for _, c := range [][2]string{{"GET", base + "/fotos"}, {"DELETE", base + "/fotos"}, {"POST", base + "/fotos/run"}, {"POST", base + "/fotos/stop"}, {"POST", base + "/fotos/disable"}} {
		if st, out, _ := do(t, srv, c[0], c[1], "", other); st != 404 || out["error"] != "not-found" {
			t.Errorf("another addon %s %s: %d %v", c[0], c[1], st, out)
		}
	}
	if st, out, _ := do(t, srv, "POST", base+"/fotos/disable", "", sync); st != 200 || out["enabled"] != false || ft.timers["sync/fotos"].Enabled {
		t.Errorf("disable: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "POST", base+"/fotos/enable", "", sync); st != 200 || out["enabled"] != true || !ft.timers["sync/fotos"].Enabled {
		t.Errorf("enable: %d %v", st, out)
	}
	ft.calls = nil
	if st, _, _ := do(t, srv, "POST", base+"/fotos/run", "", sync); st != 200 {
		t.Errorf("run: %d", st)
	}
	if st, _, _ := do(t, srv, "POST", base+"/fotos/stop", "", sync); st != 200 || fmt.Sprint(ft.calls) != "[run sync/fotos stop sync/fotos]" {
		t.Errorf("stop: %d %v", st, ft.calls)
	}
	// the errors' answers
	for name, want := range map[string][2]any{"taken": {409, "exists"}, "full": {409, "too-many"}, "undeclared": {409, "undeclared"}, "bad": {422, "invalid"}} {
		if st, out, _ := do(t, srv, "PUT", base+"/"+name, body, sync); st != want[0] || out["error"] != want[1] {
			t.Errorf("%s: %d %v", name, st, out)
		}
	}
	if st, out, _ := do(t, srv, "PUT", base+"/x", `{"timer": 5}`, sync); st != 422 || out["error"] != "invalid-body" {
		t.Errorf("a body of the wrong shape: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "DELETE", base+"/fotos", "", sync); st != 200 || out["ok"] != true || len(ft.timers) != 0 {
		t.Errorf("delete: %d %v", st, out)
	}
	if st, _, _ := do(t, srv, "GET", base+"/fotos", "", sync); st != 404 {
		t.Errorf("get after the delete: %d", st)
	}

	// without systemd: 501
	mux2 := http.NewServeMux()
	a2 := &AuthAPI{Store: store}
	a2.Register(mux2)
	(&SystemAPI{Root: logRoot(t)}).Register(mux2)
	srv2 := httptest.NewServer(a2.Middleware(mux2))
	t.Cleanup(srv2.Close)
	if st, out, _ := do(t, srv2, "GET", base, "", sync); st != 501 || out["error"] != "not-systemd" {
		t.Errorf("without systemd: %d %v", st, out)
	}
}

func TestAddonOfSession(t *testing.T) {
	for id, want := range map[string]string{
		"token:addon:sync":    "sync",
		"token:addon:a.b_c-d": "a.b_c-d",
		"token:addon:":        "",
		"token:ci":            "",
		"token:addon":         "",
		"addon:sync":          "",
		"s1":                  "",
	} {
		got, ok := addonOfSession(&auth.Session{ID: id})
		if got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", id, got, ok)
		}
	}
	if _, ok := addonOfSession(nil); ok {
		t.Error("no session is an addon's")
	}
}
