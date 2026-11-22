package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/sysupdate"
)

// occulited task 170: the download as a job - POST {"background": true} answers 202 at once, GET
// /system-update/download (and the feed's download in GET /system-update) reports the bytes, the
// total and the phase while it runs, and the outcome after it.
func TestSystemUpdateDownloadJob(t *testing.T) {
	r := fakeRoot(t)
	_ = os.WriteFile(filepath.Join(string(r), "VERSION"), []byte("VERSION=1\nPRODUCT=ova\nPLATFORM=ova\nVARIANT=lite\nLITE=1.0.0-dev.41\n"), 0o644)
	body := firmwareZip(t)
	sum := sha256.Sum256(body)
	var mu sync.Mutex
	hold := make(chan struct{})
	fail := false
	var feedSrv *httptest.Server
	feedSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		switch {
		case q.URL.Path == "/releases":
			fmt.Fprintf(w, `[{"tag_name":"v1.0.0-dev.42","prerelease":true,"assets":[{"name":"openccu-lite-x86_64-ova-1.0.0-dev.42.zip","browser_download_url":"%s/f/42","size":%d},{"name":"openccu-lite-x86_64-ova-1.0.0-dev.42.zip.sha256","browser_download_url":"%s/s/42","size":1}]}]`, feedSrv.URL, len(body), feedSrv.URL)
		case strings.HasPrefix(q.URL.Path, "/s/"):
			fmt.Fprintf(w, "%s  x\n", hex.EncodeToString(sum[:]))
		default:
			mu.Lock()
			f := fail
			mu.Unlock()
			if f {
				w.WriteHeader(500)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			_, _ = w.Write(body[:100])
			w.(http.Flusher).Flush()
			select {
			case <-hold:
				_, _ = w.Write(body[100:])
			case <-q.Context().Done():
			}
		}
	}))
	t.Cleanup(feedSrv.Close)
	feed := sysupdate.New(r, feedSrv.URL+"/releases", true, nil)
	feed.Channel = sysupdate.ChannelDev
	api := &SystemAPI{Root: r, Feed: feed, Log: testLog}
	svc := scriptBox{}
	api.Services, api.Addons, api.Manager, api.Nav = svc, svc, svc, svc
	mux := http.NewServeMux()
	api.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// before the first download: nothing to report
	if st, out, raw := do(t, srv, "GET", "/api/system/v1/system-update/download", "", nil); st != 200 || out["download"] != nil || out["busy"] != nil {
		t.Fatalf("before: %d %s", st, raw)
	}
	if st, _, _ := do(t, srv, "POST", "/api/system/v1/system-update/check", "", nil); st != 200 {
		t.Fatalf("check: %d", st)
	}
	if st, out, raw := do(t, srv, "POST", "/api/system/v1/system-update/download", `{"background":true,"version":"1.0.0-dev.42"}`, nil); st != 422 || out["error"] != "invalid" {
		t.Fatalf("background with a version: %d %s", st, raw)
	}

	st, out, raw := do(t, srv, "POST", "/api/system/v1/system-update/download", `{"background":true}`, nil)
	if b, _ := out["busy"].(map[string]any); st != 202 || out["file"] != "openccu-lite-x86_64-ova-1.0.0-dev.42.zip" || b["phase"] != "download" {
		t.Fatalf("start: %d %s", st, raw)
	}
	progress := func() map[string]any {
		t.Helper()
		_, out, _ := do(t, srv, "GET", "/api/system/v1/system-update/download", "", nil)
		d, _ := out["download"].(map[string]any)
		return d
	}
	waitFor := func(what string, ok func(map[string]any) bool) map[string]any {
		t.Helper()
		for range 400 {
			if d := progress(); d != nil && ok(d) {
				return d
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s: never; %v", what, progress())
		return nil
	}
	d := waitFor("the first bytes", func(d map[string]any) bool { return d["received"] == float64(100) })
	if d["phase"] != "downloading" || d["total"] != float64(len(body)) || d["file"] != "openccu-lite-x86_64-ova-1.0.0-dev.42.zip" {
		t.Errorf("running: %v", d)
	}
	// the page's poll gets it in the feed as well
	_, whole, _ := do(t, srv, "GET", "/api/system/v1/system-update", "", nil)
	if f, _ := whole["feed"].(map[string]any); f["download"] == nil || f["downloading"] == "" {
		t.Errorf("GET /system-update: %v", whole["feed"])
	}
	// one at a time
	if st, out, _ := do(t, srv, "POST", "/api/system/v1/system-update/download", `{"background":true}`, nil); st != 409 || out["error"] != "busy" {
		t.Errorf("a second: %d %v", st, out)
	}
	close(hold)
	d = waitFor("staged", func(d map[string]any) bool { return d["phase"] == "done" })
	if d["received"] != float64(len(body)) || !strings.Contains(fmt.Sprint(d["warning"]), "verified") || d["ended"] == nil {
		t.Errorf("done: %v", d)
	}
	for range 200 {
		if _, o, _ := do(t, srv, "GET", "/api/system/v1/system-update/download", "", nil); o["busy"] == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r.StagedSystemUpdate() == nil {
		t.Fatal("not staged")
	}

	// a failure: the phase, the code the page words, the feed's download_error
	mu.Lock()
	fail = true
	mu.Unlock()
	if st, _, raw := do(t, srv, "POST", "/api/system/v1/system-update/download", `{"background":true}`, nil); st != 202 {
		t.Fatalf("second start: %d %s", st, raw)
	}
	d = waitFor("failed", func(d map[string]any) bool { return d["phase"] == "failed" })
	if d["error_code"] != "download-failed" || !strings.Contains(fmt.Sprint(d["error"]), "HTTP 500") {
		t.Errorf("failed: %v", d)
	}

	// no feed: 501
	api.Feed = nil
	if st, _, _ := do(t, srv, "GET", "/api/system/v1/system-update/download", "", nil); st != 501 {
		t.Errorf("no feed: %d", st)
	}
}
