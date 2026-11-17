package httpapi

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/auth"
	"github.com/hobbyquaker/occulited/internal/sshkeys"
	"github.com/hobbyquaker/occulited/internal/system"
)

const (
	sshAddonBlob = "AAAAC3NzaC1lZDI1NTE5AAAAIEFSx3g/oLwaC84xZMOZIv9tk8m/eImdz0UxBwEjrnMj"
	sshOtherEd   = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ+7yF3h1Cx7HcUGeQ8cH1gI8xYh0wJjWn3sYw0n0b7e"
)

type sshAddonNames struct{}

func (sshAddonNames) ListAddons(context.Context) ([]system.Addon, error) {
	return []system.Addon{{ID: "shell", Name: "Web terminal"}}, nil
}

func sshAddonServer(t *testing.T) (srv *httptest.Server, post func(body string, hdr map[string]string) (int, map[string]any), h *sshHelper, admin, bob map[string]string, store *auth.Store, sys *SystemAPI, confirm func() map[string]string) {
	t.Helper()
	srv, h, admin, bob, store, sys = sshAPIServerWith(t)
	rc := filepath.Join(h.root, "usr/local/etc/config/rc.d")
	if err := os.MkdirAll(rc, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"shell", "other"} {
		if err := os.WriteFile(filepath.Join(rc, id), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sys.warn.now = func() time.Time { return time.Date(2026, 11, 17, 21, 29, 0, 0, time.UTC) }
	post = func(body string, hdr map[string]string) (int, map[string]any) {
		st, out, _ := do(t, srv, "POST", SSHKeysPath, body, hdr)
		return st, out
	}
	return srv, post, h, admin, bob, store, sys, func() map[string]string { return sshConfirm(t, srv, admin, SSHKeysPath) }
}

// occulited task 152: every refusal of an addon's key, each before anything is written - and the
// ones before the confirmation do not spend a password on it
func TestSSHAddonKeyRefusals(t *testing.T) {
	_, post, h, admin, bob, store, _, confirm := sshAddonServer(t)
	start := string(h.file)
	token, err := store.CreateToken("ci", auth.TokenOptions{Scopes: auth.Scopes{auth.ScopeAll}})
	if err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Authorization": "Bearer " + token}
	key := "ssh-ed25519 " + sshAddonBlob
	good := `{"key":"` + key + `","for_addon":"shell"}`
	for _, c := range []struct {
		name, body string
		hdr        map[string]string
		status     int
		code       string
	}{
		{"no session", good, nil, 401, "unauthenticated"},
		{"a token, though a token is not asked for the confirmation", good, bearer, 403, "session-required"},
		{"a user", good, bob, 403, ""},
		{"no confirmation", good, admin, 403, "confirm-required"},
		{"an addon that is not installed", `{"key":"` + key + `","for_addon":"ghost"}`, admin, 422, "unknown-addon"},
		{"no addon id", `{"key":"` + key + `","for_addon":""}`, admin, 422, "invalid"},
		{"an id with a path", `{"key":"` + key + `","for_addon":"../shell"}`, admin, 422, "invalid"},
		{"an id with a quote", `{"key":"` + key + `","for_addon":"sh\"ell"}`, admin, 422, "invalid"},
		{"an RSA key", `{"key":"` + sshTestRSA + `","for_addon":"shell"}`, admin, 422, "invalid"},
		{"a key with options", `{"key":"from=\"*\" ` + key + `","for_addon":"shell"}`, admin, 422, "invalid"},
		{"the system's own options pasted", `{"key":"restrict,pty ` + key + `","for_addon":"shell"}`, admin, 422, "invalid"},
		{"two lines", `{"key":"` + key + `\n` + key + `","for_addon":"shell"}`, admin, 422, "invalid"},
		{"no key", `{"key":"","for_addon":"shell"}`, admin, 422, "invalid"},
		{"too short a window", `{"key":"` + key + `","for_addon":"shell","arm_seconds":29}`, admin, 422, "invalid"},
		{"too long a window", `{"key":"` + key + `","for_addon":"shell","arm_seconds":121}`, admin, 422, "invalid"},
		{"no window", `{"key":"` + key + `","for_addon":"shell","arm_seconds":0}`, admin, 422, "invalid"},
		{"a window without an addon", `{"key":"` + key + `","arm_seconds":60}`, admin, 422, "invalid"},
		{"option text beside the key", `{"key":"` + key + `","for_addon":"shell","options":"from=\"*\""}`, admin, 422, "invalid-body"},
		{"an ECDSA key", `{"key":"` + strings.Join(strings.Fields(sshLabKey)[:2], " ") + `","for_addon":"shell"}`, admin, 422, "invalid"},
	} {
		st, out := post(c.body, c.hdr)
		if st != c.status || (c.code != "" && out["error"] != c.code) {
			t.Errorf("%s: %d %v, want %d %s", c.name, st, out, c.status, c.code)
		}
	}
	if string(h.file) != start {
		t.Errorf("a refusal wrote the file:\n%s", h.file)
	}
	// a token is refused with a confirmed ticket as well
	with := confirm()
	with["Authorization"] = "Bearer " + token
	delete(with, "Cookie")
	if st, out := post(good, with); st != 403 || out["error"] != "session-required" {
		t.Errorf("a token with a ticket: %d %v", st, out)
	}
	// a key that stands in the file as a key of the page: 409 before the confirmation
	plain := confirm()
	if st, out := post(`{"key":"`+key+` laptop"}`, plain); st != 201 {
		t.Fatalf("a plain key: %d %v", st, out)
	}
	before := string(h.file)
	if st, out := post(good, admin); st != 409 || out["error"] != "exists" {
		t.Errorf("the page's key as the addon's: %d %v", st, out)
	}
	if string(h.file) != before {
		t.Errorf("a refusal wrote the file:\n%s", h.file)
	}
}

// the line written, the listing, the re-arm and the replacement, the removal, and the warning
func TestSSHAddonKeyArmListRemove(t *testing.T) {
	srv, post, h, admin, _, _, sys, confirm := sshAddonServer(t)
	h.file = nil
	line := func(stamp, blob string) string {
		return `restrict,pty,from="127.0.0.1,::1",expiry-time="` + stamp + `Z" ssh-ed25519 ` + blob + ` openccu-lite-addon:shell`
	}
	section := func(lines ...string) string {
		return sshkeys.Begin + "\n" + strings.Join(lines, "\n") + "\n" + sshkeys.End + "\n"
	}
	if w, ok := sys.sshAddonKeyWarnings(context.Background()); !ok || len(w) != 0 {
		t.Fatalf("a warning without a key: %v %v", w, ok)
	}
	// new: 201, the default window of a minute; the pasted comment is not the line's
	st, out := post(`{"key":"ssh-ed25519 `+sshAddonBlob+` evil\",command=\"sh","for_addon":"shell"}`, confirm())
	if st != 201 || out["addon"] != "shell" || out["armed_until"] != "2026-11-17T21:30:00Z" || out["fingerprint"] != sshTestFP ||
		out["options"] != `restrict,pty,from="127.0.0.1,::1",expiry-time="20261117213000Z"` || out["comment"] != "openccu-lite-addon:shell" || out["type"] != "ssh-ed25519" {
		t.Fatalf("armed: %d %v", st, out)
	}
	if string(h.file) != section(line("20261117213000", sshAddonBlob)) {
		t.Fatalf("file:\n%s", h.file)
	}
	// the ticket is spent: every arming is a password
	spent := confirm()
	if st, _ := post(`{"key":"ssh-ed25519 `+sshAddonBlob+`","for_addon":"shell","arm_seconds":120}`, spent); st != 200 {
		t.Fatalf("re-arm: %d", st)
	}
	if st, out := post(`{"key":"ssh-ed25519 `+sshAddonBlob+`","for_addon":"shell"}`, spent); st != 403 || out["error"] != "confirm-required" {
		t.Errorf("a ticket armed twice: %d %v", st, out)
	}
	if string(h.file) != section(line("20261117213100", sshAddonBlob)) {
		t.Fatalf("re-armed:\n%s", h.file)
	}
	// the same key for another addon stands in the file already
	if st, out := post(`{"key":"ssh-ed25519 `+sshAddonBlob+`","for_addon":"other"}`, admin); st != 409 || out["error"] != "exists" {
		t.Errorf("another addon, the same key: %d %v", st, out)
	}
	// another key replaces the addon's line: 200, one line
	st, out = post(`{"key":"`+sshOtherEd+`","for_addon":"shell","arm_seconds":30}`, confirm())
	if st != 200 || out["armed_until"] != "2026-11-17T21:29:30Z" || out["fingerprint"] == sshTestFP {
		t.Fatalf("another key: %d %v", st, out)
	}
	otherFP, _ := out["fingerprint"].(string)
	if string(h.file) != section(line("20261117212930", strings.Fields(sshOtherEd)[1])) {
		t.Fatalf("replaced:\n%s", h.file)
	}
	// listed as the addon's
	st, out, _ = do(t, srv, "GET", SSHKeysPath, "", admin)
	managed, _ := out["managed"].([]any)
	if st != 200 || len(managed) != 1 {
		t.Fatalf("list: %d %v", st, out)
	}
	if m := managed[0].(map[string]any); m["addon"] != "shell" || m["armed_until"] != "2026-11-17T21:29:30Z" || m["options"] != `restrict,pty,from="127.0.0.1,::1",expiry-time="20261117212930Z"` {
		t.Fatalf("listed: %v", m)
	}
	// the warning: from the line, with the addon's name
	sys.Addons = sshAddonNames{}
	w, ok := sys.sshAddonKeyWarnings(context.Background())
	if !ok || len(w) != 1 || w[0].ID != "ssh-addon-key" || w[0].Variant != "shell" || w[0].Severity != "warning" || w[0].Href != "/system/remote-access#ssh" ||
		w[0].Params["addon"] != "shell" || w[0].Params["name"] != "Web terminal" || w[0].Params["armed_until"] != "2026-11-17T21:29:30Z" || w[0].Params["fingerprint"] != otherFP {
		t.Fatalf("warning: %+v %v", w, ok)
	}
	// only key login counts it as a key (nothing changes there): on with it alone, and then it is
	// the last key
	if st, out, _ := do(t, srv, "PUT", "/api/system/v1/ssh/key-only", `{"on":true}`, admin); st != 200 || out["key_only"] != true {
		t.Fatalf("only key login with the addon's key: %d %v", st, out)
	}
	if st, out, _ := do(t, srv, "DELETE", SSHKeysPath+"?fingerprint="+url.QueryEscape(otherFP), "", admin); st != 409 || out["error"] != "last-key" {
		t.Fatalf("the last key: %d %v", st, out)
	}
	h.keyOnly = false
	// a line somebody edited is no addon's line: not listed as one, no warning
	saved := h.file
	h.file = []byte(strings.Replace(string(saved), "restrict,pty,", "pty,", 1))
	if _, out, _ := do(t, srv, "GET", SSHKeysPath, "", admin); len(out["managed"].([]any)) != 0 {
		t.Errorf("an edited line listed in the section: %v", out)
	}
	if w, ok := sys.sshAddonKeyWarnings(context.Background()); !ok || len(w) != 0 {
		t.Errorf("a warning for an edited line: %v", w)
	}
	h.file = saved
	// removed like any key of the section; the warning goes with it
	if st, _, _ := do(t, srv, "DELETE", SSHKeysPath+"?fingerprint="+url.QueryEscape(otherFP), "", admin); st != 200 {
		t.Fatalf("remove: %d", st)
	}
	if len(h.file) != 0 {
		t.Errorf("after the removal:\n%s", h.file)
	}
	if w, ok := sys.sshAddonKeyWarnings(context.Background()); !ok || len(w) != 0 {
		t.Errorf("the warning stayed: %v", w)
	}
}
