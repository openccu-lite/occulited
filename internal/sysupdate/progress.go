package sysupdate

// The download's progress (occulited task 170, the maintainer on 2026-11-22 while updating his
// system to 2.0.0-rc.1: the page said only "… (299 MB) wird heruntergeladen…" for 76 s, and a
// download that stalls cannot be told from one that runs). The body of the release is read
// through a counting reader; the state tells the bytes, the total the feed or the response
// named, the rate over the last seconds and the last moment data came, so the page draws a
// determinate bar and says when nothing has arrived for a while.

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/hobbyquaker/occulited/internal/system"
)

// The phases of a download.
const (
	PhaseDownloading = "downloading" // the body is being read
	PhaseChecking    = "checking"    // the body is in; the file is checked and linked as the update
	PhaseDone        = "done"        // staged
	PhaseFailed      = "failed"
	PhaseCancelled   = "cancelled"
)

// rateWindow is how far back the rate looks: long enough to smooth TCP's bursts, short enough to
// show a slowdown within a few seconds.
const rateWindow = 8 * time.Second

// Progress is the running download, or the last one of this process, as the API answers it.
type Progress struct {
	File  string `json:"file"`
	Phase string `json:"phase"`
	// Received bytes of Total; Total 0 when neither the feed nor the response named a size.
	Received int64 `json:"received"`
	Total    int64 `json:"total"`
	// Rate is bytes per second over the last seconds; 0 while nothing came in them.
	Rate int64 `json:"rate"`
	// Started is when the download began, LastData when bytes last arrived, Ended when it ended.
	Started  time.Time  `json:"started"`
	LastData *time.Time `json:"last_data,omitempty"`
	Ended    *time.Time `json:"ended,omitempty"`
	// IdleS is how long no data has come, in whole seconds, while the body is read: from the
	// start until the first bytes, from the last bytes after them.
	IdleS int `json:"idle_s"`
	// Error is why a failed download failed, ErrorCode and ErrorDetail as the synchronous POST
	// answers them (no-space with {free, required, download}, else download-failed); Warning the
	// staged file's note (sha256 verified).
	Error       string         `json:"error,omitempty"`
	ErrorCode   string         `json:"error_code,omitempty"`
	ErrorDetail map[string]any `json:"error_detail,omitempty"`
	Warning     string         `json:"warning,omitempty"`
}

type sample struct {
	at time.Time
	n  int64
}

// counter counts the bytes read through it and keeps what Progress reports.
type counter struct {
	now func() time.Time

	mu       sync.Mutex
	p        Progress
	last     time.Time // zero until the first bytes
	samples  []sample  // the received count at most once a second, the last rateWindow
	finished bool
}

func newCounter(file string, total int64, now func() time.Time) *counter {
	if now == nil {
		now = time.Now
	}
	t := now()
	return &counter{now: now, p: Progress{File: file, Phase: PhaseDownloading, Total: total, Started: t}, samples: []sample{{t, 0}}}
}

// reader counts what r gives; at its end the phase becomes checking.
func (c *counter) reader(r io.Reader) io.Reader { return &countingReader{r: r, c: c} }

type countingReader struct {
	r io.Reader
	c *counter
}

func (cr *countingReader) Read(b []byte) (int, error) {
	n, err := cr.r.Read(b)
	cr.c.add(n, err == io.EOF)
	return n, err
}

func (c *counter) add(n int, eof bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return
	}
	t := c.now()
	if n > 0 {
		c.p.Received += int64(n)
		c.last = t
		if s := c.samples[len(c.samples)-1]; t.Sub(s.at) >= time.Second {
			c.samples = append(c.samples, sample{t, c.p.Received})
		}
		c.trimLocked(t)
	}
	if eof && c.p.Phase == PhaseDownloading {
		c.p.Phase = PhaseChecking
	}
}

func (c *counter) trimLocked(t time.Time) {
	i := 0
	for i < len(c.samples)-1 && t.Sub(c.samples[i+1].at) >= rateWindow {
		i++
	}
	c.samples = c.samples[i:]
}

// setTotal takes the response's length when the feed named no size.
func (c *counter) setTotal(n int64) {
	c.mu.Lock()
	if c.p.Total <= 0 && n > 0 {
		c.p.Total = n
	}
	c.mu.Unlock()
}

// end closes the download with its outcome.
func (c *counter) end(phase, errText, warning string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.now()
	c.p.Phase, c.p.Error, c.p.Warning, c.p.Ended = phase, errText, warning, &t
	c.finished = true
}

// fail closes the download with its error, coded as the API codes it.
func (c *counter) fail(err error) {
	c.end(PhaseFailed, err.Error(), "")
	c.mu.Lock()
	defer c.mu.Unlock()
	c.p.ErrorCode = "download-failed"
	var se *system.UpdateSpaceError
	if errors.As(err, &se) {
		c.p.ErrorCode, c.p.ErrorDetail = "no-space", map[string]any{"free": se.Free, "required": se.Required}
		if se.Download {
			c.p.ErrorDetail["download"] = true
		}
	}
}

// view is the progress now.
func (c *counter) view() Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := c.p
	t := c.now()
	if !c.last.IsZero() {
		l := c.last
		p.LastData = &l
	}
	if c.finished {
		p.Rate, p.IdleS = 0, 0
		return p
	}
	c.trimLocked(t)
	// the rate from the oldest sample in the window to now: a stall brings it down at once
	if s := c.samples[0]; t.After(s.at) && t.Sub(c.last) < rateWindow {
		p.Rate = int64(float64(p.Received-s.n) / t.Sub(s.at).Seconds())
	}
	if p.Phase == PhaseDownloading {
		since := p.Started
		if !c.last.IsZero() {
			since = c.last
		}
		p.IdleS = int(t.Sub(since) / time.Second)
	}
	return p
}
