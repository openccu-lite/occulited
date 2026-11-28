package catalog

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// occulited task 190: catalog-cache.json is written only when its content changed. A check wrote
// the 80 kB file up to four times, the daily refresh every day even when nothing was new.

const cacheSentinel = "untouched since the last real change"

// markCache replaces the file's content with a sentinel, which only a write removes.
func markCache(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(cacheSentinel), 0o644); err != nil {
		t.Fatal(err)
	}
}

func cacheWritten(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) != cacheSentinel
}

func TestCacheWrittenOnlyWhenChanged(t *testing.T) {
	release := `[]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/catalog/catalog.json":
			_, _ = w.Write([]byte(`{"format": 1, "addons": [{"git": "https://github.com/hc/mosq", "manifest": "addon_files/openccu-lite.json"}]}`))
		case "/hc/mosq/HEAD/addon_files/openccu-lite.json":
			_, _ = w.Write([]byte(`{"format": 1, "id": "mosquitto", "name": "Mosquitto", "release": {"github": "hc/mosq", "asset": "mosquitto-{arch}-{version}.tar.gz"}}`))
		case "/repos/hc/mosq/releases":
			_, _ = w.Write([]byte(release))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), "catalog-cache.json")
	newService := func() *Service {
		s := New([]string{srv.URL + "/catalog/catalog.json"}, "x86_64", &fakeInstaller{})
		s.GitHubAPI, s.RawGitHub = srv.URL, srv.URL
		s.CacheFile = path
		return s
	}
	s := newService()
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !cacheWritten(t, path) {
		t.Fatal("the first check wrote no cache")
	}
	for _, tc := range []struct {
		name    string
		setup   func() // before the file is marked
		do      func() *Service
		written bool
	}{
		{"the daily refresh, nothing new", nil, func() *Service { s.daily(t.Context()); return s }, false},
		{"again", nil, func() *Service { s.daily(t.Context()); return s }, false},
		{"a restart, then the daily refresh", func() { s = newService(); _, _ = s.Fetch(t.Context(), false) }, func() *Service { s.daily(t.Context()); return s }, false},
		{"a new release", nil, func() *Service {
			release = `[{"tag_name": "v2.0.0", "assets": [{"name": "mosquitto-x86_64-2.0.0.tar.gz", "browser_download_url": "https://example.org/m.tgz"}]}]`
			s.daily(t.Context())
			return s
		}, true},
		{"the user's check (a new check time)", nil, func() *Service { _ = s.Refresh(t.Context()); return s }, true},
	} {
		if tc.setup != nil {
			tc.setup()
		}
		real, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		markCache(t, path)
		tc.do()
		if got := cacheWritten(t, path); got != tc.written {
			t.Fatalf("%s: written %v, want %v", tc.name, got, tc.written)
		}
		if !tc.written {
			// the file as it really is, for the next case's restart to read
			if err := os.WriteFile(path, real, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
