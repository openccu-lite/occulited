package catalog

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// budgetStub is GitHub with an address budget: every API request costs one (a 304 too, as for an
// address without a token), each answer carries X-RateLimit-Remaining and -Reset, and at zero it
// refuses with 403. It serves n addons a0…a<n-1>, each released once.
type budgetStub struct {
	srv   *httptest.Server
	mu    sync.Mutex
	left  int
	reset time.Time
	api   map[string]int // path → requests
}

func newBudgetStub(t *testing.T, n, budget int, reset time.Time) *budgetStub {
	b := &budgetStub{left: budget, reset: reset, api: map[string]int{}}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/catalog.json" {
			var entries []string
			for i := range n {
				entries = append(entries, fmt.Sprintf(`{"git": "https://github.com/o/a%d", "manifest": "openccu-lite.json"}`, i))
			}
			_, _ = w.Write([]byte(`{"format": 1, "addons": [` + strings.Join(entries, ",") + `]}`))
			return
		}
		if strings.HasPrefix(p, "/repos/") {
			b.mu.Lock()
			b.api[p]++
			if b.left == 0 {
				b.mu.Unlock()
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(b.reset.Unix(), 10))
				w.WriteHeader(403)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
				return
			}
			b.left--
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(b.left))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(b.reset.Unix(), 10))
			b.mu.Unlock()
			repo := strings.TrimPrefix(p, "/repos/o/")
			if name, ok := strings.CutSuffix(repo, "/releases"); ok {
				_, _ = fmt.Fprintf(w, `[{"tag_name":"v1.0.0","assets":[{"name":"%s-x86_64-1.0.0.tar.gz","size":3,"browser_download_url":"%s/pkg"}]}]`, name, b.srv.URL)
				return
			}
			_, _ = w.Write([]byte(`{"stargazers_count": 5}`))
			return
		}
		// the raw host: o/a<i>/v1.0.0/openccu-lite.json
		if parts := strings.Split(strings.Trim(p, "/"), "/"); len(parts) == 4 && parts[3] == "openccu-lite.json" {
			id := parts[1]
			_, _ = fmt.Fprintf(w, `{"format": 1, "id": "%s", "name": "%s", "release": {"github": "o/%s", "asset": "%s-{arch}-{version}.tar.gz"}}`, id, id, id, id)
			return
		}
		if p == "/pkg" {
			_, _ = w.Write([]byte("pkg"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *budgetStub) count(suffix string) (n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for p, c := range b.api {
		if strings.HasSuffix(p, suffix) {
			n += c
		}
	}
	return n
}

func (b *budgetStub) service(t *testing.T, now time.Time) *Service {
	s := New([]string{b.srv.URL + "/catalog.json"}, "x86_64", &fakeInstaller{})
	s.GitHubAPI, s.RawGitHub = b.srv.URL, b.srv.URL
	s.CacheFile = filepath.Join(t.TempDir(), "cache.json")
	s.now = func() time.Time { return now }
	return s
}

// B-172: a check without the pre-resolved file reads each release list once - not once for the
// manifest's tag and again for the version - and the star counts after them.
func TestCheckReadsEachReleaseListOnce(t *testing.T) {
	now := time.Now()
	b := newBudgetStub(t, 22, 5000, now.Add(time.Hour))
	s := b.service(t, now)
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := b.count("/releases"); n != 22 {
		t.Errorf("release lists asked %d times, want 22", n)
	}
	if n := b.count("") - b.count("/releases"); n != 22 {
		t.Errorf("star counts asked %d times, want 22", n)
	}
	v := s.Cached()
	if v.ReleasesError != nil || v.CheckError != nil {
		t.Fatalf("a clean check: %+v %+v", v.ReleasesError, v.CheckError)
	}
	for _, it := range v.Addons {
		if it.Latest == nil || it.Latest.Version != "1.0.0" || it.Stars != 5 {
			t.Fatalf("%s: %+v stars %d", it.ID, it.Latest, it.Stars)
		}
	}
	// an install reads its list anew (B-21), outside any check's memo
	before := b.count("/releases")
	if p, err := s.Install(t.Context(), "a3"); err != nil || p.Phase != "done" {
		t.Fatalf("install: %v %+v", err, p)
	}
	if n := b.count("/releases") - before; n != 1 {
		t.Errorf("the install asked %d times, want 1", n)
	}
}

// B-172: the address's budget is what the user's next install needs. A check that sees GitHub's
// count fall to the reserve stops asking - the stars first, then the release lists - and names the
// limit; the install afterwards still gets its list.
func TestCheckKeepsTheReserveForAnInstall(t *testing.T) {
	now := time.Now()
	b := newBudgetStub(t, 22, 30, now.Add(40*time.Minute))
	s := b.service(t, now)
	if err := s.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n := b.count(""); n != 30-apiReserve {
		t.Errorf("the check asked %d times, want %d", n, 30-apiReserve)
	}
	if n := b.count("/releases"); n != 20 {
		t.Errorf("release lists %d, want 20 (stars come last)", n)
	}
	v := s.Cached()
	n := v.ReleasesError
	if n == nil || n.Code != CodeRateLimit || n.RetryMinutes != 40 || !strings.Contains(n.Message, "for an install") {
		t.Fatalf("the notice: %+v", n)
	}
	// a20 and a21 were held back: no manifest yet, their cards say why
	for _, it := range v.Addons {
		if it.Git == "https://github.com/o/a21" && (it.Manifest != nil || !strings.Contains(it.Error, "rate limit")) {
			t.Fatalf("a21: %+v", it)
		}
	}
	if p, err := s.Install(t.Context(), "a0"); err != nil || p.Phase != "done" {
		t.Fatalf("install after the check: %v %+v", err, p)
	}
	// at GitHub's reset the check asks again
	now = now.Add(41 * time.Minute)
	s.now = func() time.Time { return now }
	b.mu.Lock()
	b.left, b.reset = 60, now.Add(time.Hour)
	b.mu.Unlock()
	if _, wait := s.holdBack(withCheck(t.Context())); wait {
		t.Error("held back after the reset")
	}
	if _, wait := s.holdBack(t.Context()); wait {
		t.Error("an install is never held back")
	}
}
