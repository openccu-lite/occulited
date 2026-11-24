package catalog

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// occulited B-172: one catalogue check spent the address's whole unauthenticated hour, and the
// install the user started next was refused for the rate limit.
//
// GitHub gives an address without a token 60 API requests an hour, conditional ones included (a
// 304 is free only for an authenticated request), and the system shares them with everything
// else behind the same public address. A check without the pre-resolved file (task 124: stale
// whenever GitHub's scheduler skips the resolver's hourly runs, which it does for half a day at a
// time) asked for every addon's release list twice - latestTag for the manifest, Resolve for the
// version - and for its star count once: about 65 requests for the 22 entries of 2026-11. Now
//
//   - a check reads a repository's release list once (checkRun's memo; an install still reads it
//     anew, B-21),
//   - the star counts are asked after the release lists, so what is spent on them comes last,
//   - and a check stops asking the API while GitHub's own count of what is left
//     (X-RateLimit-Remaining) is at apiReserve or below: those requests are an install's. A
//     repository the check held back for is named as rate-limited, with GitHub's reset time.

// apiReserve is how many of the address's API requests a check leaves for installs.
const apiReserve = 10

// checkRun is one check's memo of the release lists it has read, by repository and length.
type checkRun struct {
	mu   sync.Mutex
	rels map[string][]ghRelease
}

type checkKey struct{}

// withCheck marks ctx as a check's (Refresh, the daily run): its API requests keep the reserve
// and its release lists are read once.
func withCheck(ctx context.Context) context.Context {
	return context.WithValue(ctx, checkKey{}, &checkRun{rels: map[string][]ghRelease{}})
}

// checkOf is ctx's check, nil for an install and everything else.
func checkOf(ctx context.Context) *checkRun {
	c, _ := ctx.Value(checkKey{}).(*checkRun)
	return c
}

func (c *checkRun) get(key string) ([]ghRelease, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.rels[key]
	return r, ok
}

func (c *checkRun) put(key string, rels []ghRelease) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rels[key] = rels
}

// apiBudget is what GitHub's last API answer said is left of the address's hour.
type apiBudget struct {
	known     bool
	remaining int
	reset     time.Time
}

// noteBudget reads X-RateLimit-Remaining and X-RateLimit-Reset from an API answer.
func (s *Service) noteBudget(req *http.Request, res *http.Response) {
	if res == nil || s.GitHubAPI == "" || !strings.HasPrefix(req.URL.String(), s.GitHubAPI+"/") {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("X-RateLimit-Remaining")))
	if err != nil {
		return
	}
	b := apiBudget{known: true, remaining: n}
	if r, err := strconv.ParseInt(strings.TrimSpace(res.Header.Get("X-RateLimit-Reset")), 10, 64); err == nil && r > 0 {
		b.reset = time.Unix(r, 0)
	}
	s.mu.Lock()
	s.budget = b
	s.mu.Unlock()
}

// holdBack says whether a check's API request waits, to keep the reserve for an install, and
// until when: only for a check, only while GitHub's last answer said apiReserve or fewer requests
// are left and its reset is still ahead.
func (s *Service) holdBack(ctx context.Context) (time.Time, bool) {
	if checkOf(ctx) == nil {
		return time.Time{}, false
	}
	s.mu.Lock()
	b := s.budget
	s.mu.Unlock()
	if !b.known || b.remaining > apiReserve || !b.reset.After(s.clock()) {
		return time.Time{}, false
	}
	return b.reset, true
}
