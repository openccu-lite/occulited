package sysupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hobbyquaker/occulited/internal/system"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// occulited task 170: the counting reader - bytes, the rate over the last seconds, how long no
// data came, and the phase at the end of the body.
func TestCounter(t *testing.T) {
	clk := &fakeClock{t: time.Date(2026, 11, 22, 11, 15, 35, 0, time.UTC)}
	c := newCounter("openccu-lite-aarch64-rpi3-2.0.0-rc.1.zip", 0, clk.now)
	c.setTotal(-1) // a chunked answer names none
	r := c.reader(strings.NewReader(strings.Repeat("x", 1000)))
	buf := make([]byte, 100)

	clk.add(3 * time.Second)
	if p := c.view(); p.Phase != PhaseDownloading || p.Received != 0 || p.Total != 0 || p.IdleS != 3 || p.Rate != 0 || p.LastData != nil {
		t.Fatalf("before the first bytes: %+v", p)
	}
	c.setTotal(1000)
	for range 4 { // 100 bytes a second
		clk.add(time.Second)
		_, _ = r.Read(buf)
	}
	p := c.view()
	if p.Received != 400 || p.Total != 1000 || p.IdleS != 0 || p.LastData == nil || p.Rate != 400/7 {
		t.Fatalf("running: %+v (the window reaches back to the start: 400 in 7 s)", p)
	}
	for range 6 {
		clk.add(time.Second)
		_, _ = r.Read(buf)
	}
	if p = c.view(); p.Received != 1000 || p.Rate != 100 {
		t.Fatalf("steady: %+v", p)
	}
	// a stall: the idle seconds count, the rate falls to 0 once the window holds no data
	clk.add(5 * time.Second)
	if p = c.view(); p.IdleS != 5 || p.Phase != PhaseDownloading {
		t.Fatalf("stalled 5 s: %+v", p)
	}
	clk.add(5 * time.Second)
	if p = c.view(); p.IdleS != 10 || p.Rate != 0 {
		t.Fatalf("stalled 10 s: %+v", p)
	}
	// the end of the body: checking, then done with the file's note
	if _, err := r.Read(buf); err != io.EOF {
		t.Fatalf("eof: %v", err)
	}
	if p = c.view(); p.Phase != PhaseChecking || p.IdleS != 0 {
		t.Fatalf("checking: %+v", p)
	}
	c.end(PhaseDone, "", "sha256 verified")
	c.add(5, false) // nothing after the end
	if p = c.view(); p.Phase != PhaseDone || p.Warning != "sha256 verified" || p.Ended == nil || p.Rate != 0 || p.Received != 1000 {
		t.Fatalf("done: %+v", p)
	}
}

// occulited task 170: the service's download reports its progress while it runs and its outcome
// after - staged, failed or cancelled - in State and Progress.
func TestDownloadProgress(t *testing.T) {
	body := releaseZip()
	sum := sha256.Sum256(body)
	half := make(chan struct{})
	release := make(chan struct{})
	var srv *httptest.Server
	mode := "ok"
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"t","assets":[{"name":"openccu-lite-x86_64-ova-2.zip","browser_download_url":"%s/a","size":%d},{"name":"openccu-lite-x86_64-ova-2.zip.sha256","browser_download_url":"%s/s","size":1}]}`, srv.URL, len(body), srv.URL)
		case "/a":
			if mode == "404" {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			_, _ = w.Write(body[:len(body)/2])
			w.(http.Flusher).Flush()
			half <- struct{}{}
			<-release
			_, _ = w.Write(body[len(body)/2:])
		case "/s":
			fmt.Fprintf(w, "%s  x\n", hex.EncodeToString(sum[:]))
		}
	}))
	t.Cleanup(srv.Close)
	r := fakeRoot(t, "VERSION=1\nPRODUCT=ova\nPLATFORM=ova\nVARIANT=lite\n")
	s := New(r, srv.URL+"/latest", true, nil)
	if s.Progress() != nil || s.State().Download != nil {
		t.Fatal("progress before a download")
	}
	if err := s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Download(context.Background()); done <- err }()
	<-half
	var p *Progress
	for range 200 {
		if p = s.Progress(); p != nil && p.Received >= int64(len(body)/2) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p.Phase != PhaseDownloading || p.Total != int64(len(body)) || p.Received < int64(len(body)/2) || p.Received >= int64(len(body)) || p.File != "openccu-lite-x86_64-ova-2.zip" {
		t.Fatalf("halfway: %+v", p)
	}
	if st := s.State(); st.Download == nil || st.Downloading == "" {
		t.Fatalf("state halfway: %+v", st)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if p = s.Progress(); p.Phase != PhaseDone || p.Received != int64(len(body)) || !strings.Contains(p.Warning, "verified") || p.Ended == nil {
		t.Fatalf("done: %+v", p)
	}

	// a failure is kept with its reason
	mode = "404"
	if _, err := s.Download(context.Background()); err == nil {
		t.Fatal("a 404 downloaded")
	}
	if p = s.Progress(); p.Phase != PhaseFailed || !strings.Contains(p.Error, "HTTP 404") || p.ErrorCode != "download-failed" {
		t.Fatalf("failed: %+v", p)
	}

	// a cancel while the body runs
	mode = "ok"
	release = make(chan struct{})
	go func() { _, err := s.Download(context.Background()); done <- err }()
	<-half
	if !s.CancelDownload() {
		t.Fatal("no download to cancel")
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrDownloadCancelled) {
		t.Fatalf("cancel: %v", err)
	}
	if p = s.Progress(); p.Phase != PhaseCancelled || p.Error != "" {
		t.Fatalf("cancelled: %+v", p)
	}
	// no space: coded as the synchronous answer codes it
	c := newCounter("f", 1, nil)
	c.fail(fmt.Errorf("stage: %w", &system.UpdateSpaceError{Free: 10, Required: 20, Download: true}))
	if p := c.view(); p.ErrorCode != "no-space" || p.ErrorDetail["free"] != int64(10) || p.ErrorDetail["download"] != true {
		t.Errorf("no space: %+v", p)
	}
}

// lateBody hands out the first half, signals half, waits for release and hands out the rest with
// the EOF in the same read - it does not look at the request's context, as a transport that
// already holds the bytes does not, so no later Read sees the cancel.
type lateBody struct {
	body          []byte
	pos           int
	half, release chan struct{}
}

func (b *lateBody) Read(p []byte) (int, error) {
	mid := len(b.body) / 2
	if b.pos == mid && b.half != nil {
		b.half <- struct{}{}
		<-b.release
		b.half = nil
	}
	if b.pos >= len(b.body) {
		return 0, io.EOF
	}
	end := len(b.body)
	if b.pos < mid {
		end = mid
	}
	n := copy(p, b.body[b.pos:end])
	b.pos += n
	if b.pos == len(b.body) {
		return n, io.EOF // net/http's body hands out the last bytes with the EOF, as here
	}
	return n, nil
}
func (b *lateBody) Close() error { return nil }

type lateTransport struct {
	next http.RoundTripper
	body *lateBody
}

func (t lateTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path != "/a" {
		return t.next.RoundTrip(r)
	}
	return &http.Response{StatusCode: 200, ContentLength: int64(len(t.body.body)), Body: t.body, Header: http.Header{}, Request: r}, nil
}

// B-177: a cancel CancelDownload said yes to wins also when the rest of the body arrives before
// the copy sees the cancelled context - the download ends cancelled and nothing stays staged.
func TestDownloadCancelBeforeTheLastBytes(t *testing.T) {
	body := releaseZip()
	sum := sha256.Sum256(body)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			fmt.Fprintf(w, `{"tag_name":"t","assets":[{"name":"openccu-lite-x86_64-ova-2.zip","browser_download_url":"%s/a","size":%d},{"name":"openccu-lite-x86_64-ova-2.zip.sha256","browser_download_url":"%s/s","size":1}]}`, srv.URL, len(body), srv.URL)
		case "/s":
			fmt.Fprintf(w, "%s  x\n", hex.EncodeToString(sum[:]))
		}
	}))
	t.Cleanup(srv.Close)
	r := fakeRoot(t, "VERSION=1\nPRODUCT=ova\nPLATFORM=ova\nVARIANT=lite\n")
	s := New(r, srv.URL+"/latest", true, nil)
	lb := &lateBody{body: body, half: make(chan struct{}), release: make(chan struct{})}
	s.HTTP = &http.Client{Transport: lateTransport{next: http.DefaultTransport, body: lb}}
	if err := s.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Download(context.Background()); done <- err }()
	<-lb.half
	if !s.CancelDownload() {
		t.Fatal("no download to cancel")
	}
	close(lb.release)
	if err := <-done; !errors.Is(err, ErrDownloadCancelled) {
		t.Fatalf("cancel: %v", err)
	}
	if p := s.Progress(); p.Phase != PhaseCancelled {
		t.Fatalf("progress: %+v", p)
	}
	if st := s.State(); st.Downloading != "" || st.DownloadError != "" {
		t.Fatalf("state: %+v", st)
	}
	if u := r.StagedSystemUpdate(); u != nil {
		t.Fatalf("staged after the cancel: %+v", u)
	}
	// after the end no cancel is taken
	if s.CancelDownload() {
		t.Fatal("a cancel was taken after the download ended")
	}
}
