package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/health"
	"github.com/hobbyquaker/occulited/internal/interfaces"
	"github.com/hobbyquaker/occulited/internal/rpcsub"
	"github.com/hobbyquaker/occulited/internal/syshist"
)

func TestETagMatches(t *testing.T) {
	const tag = `"0123456789abcdef"`
	for _, c := range []struct {
		header string
		want   bool
	}{
		{"", false},
		{tag, true},
		{"W/" + tag, true},
		{`"other", ` + tag, true},
		{`"other"`, false},
		{"*", true},
		{`0123456789abcdef`, false},
	} {
		if got := etagMatches(c.header, tag); got != c.want {
			t.Errorf("%q: %v, want %v", c.header, got, c.want)
		}
	}
}

// getTagged is a GET through the mux with an If-None-Match of its own.
func getTagged(t *testing.T, mux http.Handler, path, inm string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	shellHeader(req, nil)
	if inm != "" {
		req.Header.Set("If-None-Match", inm)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// occulited task 162: GET /status/history carries an ETag and answers an unchanged history with a
// 304 without a body - the Status page asks every 10 s, the sampler adds a sample a minute.
func TestStatusHistoryRevalidates(t *testing.T) {
	load := 1.0
	s := &syshist.Sampler{Read: func() (syshist.Reading, bool) { return syshist.Reading{Load: load, Mem: 37}, true }}
	s.Poll()
	mux := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t), SysHist: s}).Register(mux)
	first := getTagged(t, mux, "/api/system/v1/status/history", "")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || tag == "" || first.Header().Get("Cache-Control") != "no-cache" || first.Body.Len() == 0 {
		t.Fatalf("first: %d %q %v", first.Code, tag, first.Header())
	}
	again := getTagged(t, mux, "/api/system/v1/status/history", tag)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 || again.Header().Get("ETag") != tag {
		t.Fatalf("unchanged: %d, %d bytes, %v", again.Code, again.Body.Len(), again.Header())
	}
	load = 2.5
	s.Poll()
	changed := getTagged(t, mux, "/api/system/v1/status/history", tag)
	if changed.Code != 200 || changed.Header().Get("ETag") == tag || changed.Body.Len() == 0 {
		t.Fatalf("after a sample: %d %v", changed.Code, changed.Header())
	}
	// an error is never tagged
	mux2 := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t)}).Register(mux2)
	if rec := getTagged(t, mux2, "/api/system/v1/status/history", "*"); rec.Code != 501 || rec.Header().Get("ETag") != "" {
		t.Fatalf("501: %d %v", rec.Code, rec.Header())
	}
}

// occulited task 162: GET /radio/health?after=<history_cursor> carries only the samples appended
// since the answer that gave the cursor; without one, or with a stale one, the whole history.
func TestRadioHealthAfterCursor(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	var in uint64 = 10
	s := &health.Sampler{Timeout: 2 * time.Second,
		Interfaces: func() []interfaces.Interface {
			return interfaces.FromList([]struct{ Name, URL string }{{"HmIP-RF", "xmlrpc://" + closed}})
		},
		Feed: func() []rpcsub.IfaceStatus {
			return []rpcsub.IfaceStatus{{Name: "HmIP-RF", State: "up", Registered: true, Telegrams: in}}
		},
		MinRateSpan: time.Nanosecond,
	}
	s.Poll(context.Background())
	in = 20
	s.Poll(context.Background())
	mux := http.NewServeMux()
	(&SystemAPI{Root: fakeRoot(t), Health: s}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	rates := func(out map[string]any) []any {
		r, _ := out["rates"].(map[string]any)
		l, _ := r["HmIP-RF"].([]any)
		return l
	}
	st, out, _ := do(t, srv, "GET", "/api/system/v1/radio/health", "", nil)
	cursor, _ := out["history_cursor"].(string)
	if st != 200 || cursor == "" || out["history_delta"] != false || out["history_rows"] != 500.0 || len(rates(out)) != 1 {
		t.Fatalf("whole: %d %v", st, out)
	}
	st, out, _ = do(t, srv, "GET", "/api/system/v1/radio/health?after="+cursor, "", nil)
	if st != 200 || out["history_delta"] != true || len(rates(out)) != 0 || out["history_cursor"] != cursor {
		t.Fatalf("nothing new: %d %v", st, out)
	}
	// the live parts are there in a delta too
	if _, ok := out["interfaces"]; !ok {
		t.Fatalf("a delta without interfaces: %v", out)
	}
	in = 35
	s.Poll(context.Background())
	_, out, _ = do(t, srv, "GET", "/api/system/v1/radio/health?after="+cursor, "", nil)
	if out["history_delta"] != true || len(rates(out)) != 1 || out["history_cursor"] == cursor {
		t.Fatalf("one new sample: %v", out)
	}
	_, out, _ = do(t, srv, "GET", "/api/system/v1/radio/health?after=1.2", "", nil)
	if out["history_delta"] != false || len(rates(out)) != 2 {
		t.Fatalf("stale cursor: %v", out)
	}
}
