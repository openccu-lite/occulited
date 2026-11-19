package backuptarget

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/hobbyquaker/occulited/internal/priv"
)

// Addon-provided backup targets (occulited task 150): an installed addon declares backup.targets
// in its manifest and serves six HTTP calls on a Unix socket; its destinations are targets of kind
// addon. docs/backup-provider.md is the contract; this file is the system's side of it - the
// client, its limits and the mapping of a provider's answers onto the targets' states.
//
// Words: a provider is an addon that declares the extension; a destination is one place it offers
// (an account, a bucket); a target is the system's and names a provider, a destination and a folder.

// KindAddon is a target on an addon's destination.
const KindAddon = "addon"

// The states this kind adds.
const (
	// StateAddonMissing: the provider's socket is not there or does not answer - the addon is
	// uninstalled, switched off, stopped, or its program files are missing after a restore. The
	// target stays configured; the run skips it and delivers to the others.
	StateAddonMissing = "addon-missing"
	// StateNotEncrypted: an addon target gets encrypted backups only, and there is no recovery
	// key (encryption was switched off after the target was made). The run skips it.
	StateNotEncrypted = "not-encrypted"
)

// ProviderProtocol is the contract's version.
const ProviderProtocol = 1

// ProviderSocketBase holds one directory per provider, made by root before the addon's unit starts
// (occulited -addon-own): <base>/<addon id>/, owned by occulite with the addon's group, 0770 - the
// addon binds target.sock there, the system's service connects, nobody else reaches it.
const ProviderSocketBase = priv.ProviderDirBase

// ProviderSocketName is the socket's file name in the provider's directory.
const ProviderSocketName = "target.sock"

// ProviderSocket is a provider's socket path.
func ProviderSocket(addon string) string {
	return filepath.Join(ProviderSocketBase, addon, ProviderSocketName)
}

// The contract's limits.
const (
	providerConnectTimeout = 5 * time.Second
	providerInfoTimeout    = 10 * time.Second
	providerTestTimeout    = 60 * time.Second
	providerListTimeout    = 60 * time.Second
	// a put and a get may take the file's size at providerMinRate, and at least providerMinTransfer
	providerMinTransfer = 10 * time.Minute
	providerMinRate     = 32 * 1024 // bytes per second
	// providerStall: a put the provider takes no byte of for this long is given up
	providerStall = 2 * time.Minute
	// providerMaxAnswer bounds every JSON answer
	providerMaxAnswer = 1 << 20
	// providerMaxFiles bounds a listing
	providerMaxFiles = 5000
)

// AddonRef is an addon target's settings: the provider, its destination, the folder there and
// where the upload reads the backup from.
type AddonRef struct {
	// ID is the provider's addon id.
	ID string `json:"id"`
	// Destination is the provider's opaque, stable id of the place (an account).
	Destination string `json:"destination"`
	// Folder is the folder on the destination, one per system so several can share an account.
	Folder string `json:"folder"`
	// Source is where the upload reads from: "" = automatic (the first enabled local target that
	// keeps encrypted backups, else the working copy), SourceStaging = always the working copy, or
	// a directory or share target's id.
	Source string `json:"source,omitempty"`
}

// SourceStaging is AddonRef.Source for "the system's working copy".
const SourceStaging = "staging"

var (
	providerIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	destIDRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	folderSegRe  = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._ -]{0,63}$`)
)

// ValidProviderFolder checks a folder on a destination: one to six segments of letters, digits,
// space, . _ -, none hidden, no "..".
func ValidProviderFolder(f string) bool {
	if f == "" || len(f) > 255 {
		return false
	}
	parts := strings.Split(f, "/")
	if len(parts) > 6 {
		return false
	}
	for _, p := range parts {
		if !folderSegRe.MatchString(p) || strings.HasSuffix(p, " ") || p == "." || p == ".." {
			return false
		}
	}
	return true
}

func (a *AddonRef) validate() error {
	if !providerIDRe.MatchString(a.ID) {
		return invalid("addon.id: the id of the addon that provides the target")
	}
	if !destIDRe.MatchString(a.Destination) {
		return invalid("addon.destination: the id of one of the addon's destinations")
	}
	if !ValidProviderFolder(a.Folder) {
		return invalid("addon.folder: up to six levels of letters, digits, space, . _ -")
	}
	if a.Source != "" && a.Source != SourceStaging && a.Source != DirectoryID && !providerSourceRe.MatchString(a.Source) {
		return invalid("addon.source: empty (automatic), %q or a directory or share target's id", SourceStaging)
	}
	return nil
}

var providerSourceRe = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// ProviderText is a sentence in the languages the provider has it in ("de", "en").
type ProviderText map[string]string

// ProviderAction is a link the card shows as a button ("Log in again" → the addon's page).
type ProviderAction struct {
	Label ProviderText `json:"label"`
	Href  string       `json:"href"`
}

var actionHrefRe = regexp.MustCompile(`^/(addons|addon-settings)/[A-Za-z0-9._~/?#=&%+-]{1,300}$`)

// clean drops an action that is not an in-app link with a label, and bounds a text.
func (a *ProviderAction) clean() *ProviderAction {
	if a == nil || !actionHrefRe.MatchString(a.Href) || strings.Contains(a.Href, "..") || strings.Contains(a.Href, "//") {
		return nil
	}
	l := a.Label.clean(60)
	if len(l) == 0 {
		return nil
	}
	return &ProviderAction{Label: l, Href: a.Href}
}

func (t ProviderText) clean(max int) ProviderText {
	out := ProviderText{}
	for _, lang := range []string{"de", "en"} {
		s := strings.TrimSpace(t[lang])
		if s == "" || !printable(s, 4*max) {
			continue
		}
		if r := []rune(s); len(r) > max {
			s = string(r[:max])
		}
		out[lang] = s
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Destination is one place a provider offers, as GET /v1/info answers it.
type Destination struct {
	ID string `json:"id"`
	// Name is the user's name for it; Detail a line like "OneDrive · max@example.org".
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	// State is one of the provider states; Message the provider's own sentence for it.
	State   string          `json:"state"`
	Message ProviderText    `json:"message,omitempty"`
	Action  *ProviderAction `json:"action,omitempty"`
}

// ProviderInfo is GET /v1/info.
type ProviderInfo struct {
	Protocol     int           `json:"protocol"`
	Destinations []Destination `json:"destinations"`
}

// providerStates are the states a provider may answer; anything else is an error.
var providerStates = map[string]bool{
	StateWritable: true, StateUnreachable: true, StateAuthFailed: true, StateFull: true, StateReadOnly: true, StateError: true,
}

// ProviderError is a provider's refusal: its state, its sentence and its action.
type ProviderError struct {
	State   string
	Message ProviderText
	Action  *ProviderAction
	// Text is the plain reason for the log and last.json (the English sentence, or the fault).
	Text string
}

func (e *ProviderError) Error() string {
	if e.Text != "" {
		return e.Text
	}
	return "the provider answered " + e.State
}

// ErrAddonMissing: the provider's socket is not there, is not the addon's, or nothing answers on it.
var ErrAddonMissing = errors.New("the addon that provides this target is not installed or not running")

func providerErr(state string, format string, a ...any) *ProviderError {
	return &ProviderError{State: state, Text: fmt.Sprintf(format, a...)}
}

// ProviderClient speaks the contract to one provider.
type ProviderClient struct {
	// Addon is the provider's id.
	Addon string
	// Socket is its socket; "" = ProviderSocket(Addon).
	Socket string
	// OwnerOK says whether a socket owned by uid is the addon's; nil = the user addon-<id>, or root
	// (an addon the user runs as root).
	OwnerOK func(uid uint32) bool
	// Stall and MinTransfer replace the contract's limits (tests).
	Stall       time.Duration
	MinTransfer time.Duration

	once sync.Once
	hc   *http.Client
}

func (c *ProviderClient) socket() string {
	if c.Socket != "" {
		return c.Socket
	}
	return ProviderSocket(c.Addon)
}

// AddonUserPrefix names a confined addon's user: addon-<id>.
const AddonUserPrefix = "addon-"

func (c *ProviderClient) ownerOK(uid uint32) bool {
	if c.OwnerOK != nil {
		return c.OwnerOK(uid)
	}
	if uid == 0 {
		return true
	}
	u, err := user.Lookup(AddonUserPrefix + c.Addon)
	if err != nil {
		return false
	}
	n, err := strconv.ParseUint(u.Uid, 10, 32)
	return err == nil && uint32(n) == uid
}

// check is the socket's own test before every call: there, a socket, and the addon's.
func (c *ProviderClient) check() error {
	var st syscall.Stat_t
	if err := syscall.Lstat(c.socket(), &st); err != nil {
		return fmt.Errorf("%w (%s: %v)", ErrAddonMissing, c.socket(), err)
	}
	if st.Mode&syscall.S_IFMT != syscall.S_IFSOCK {
		return fmt.Errorf("%w (%s is not a socket)", ErrAddonMissing, c.socket())
	}
	if !c.ownerOK(st.Uid) {
		return fmt.Errorf("%w (%s belongs to uid %d, not to the addon)", ErrAddonMissing, c.socket(), st.Uid)
	}
	return nil
}

func (c *ProviderClient) client() *http.Client {
	c.once.Do(func() {
		c.hc = &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: providerConnectTimeout}
				return d.DialContext(ctx, "unix", c.socket())
			},
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: 0,
			DisableCompression:    true,
		}}
	})
	return c.hc
}

// Reachable is the cheap check the root half of a run makes: the socket is the addon's and a
// connection opens. Nothing the provider says is read.
func (c *ProviderClient) Reachable(ctx context.Context) error {
	if err := c.check(); err != nil {
		return err
	}
	d := net.Dialer{Timeout: providerConnectTimeout}
	conn, err := d.DialContext(ctx, "unix", c.socket())
	if err != nil {
		return fmt.Errorf("%w (%v)", ErrAddonMissing, err)
	}
	return conn.Close()
}

// answer is the body every JSON answer shares.
type providerAnswer struct {
	State   string          `json:"state"`
	Message ProviderText    `json:"message"`
	Action  *ProviderAction `json:"action"`
}

func (c *ProviderClient) do(req *http.Request) (*http.Response, error) {
	if err := c.check(); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "occulited")
	res, err := c.client().Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" {
			return nil, fmt.Errorf("%w (%v)", ErrAddonMissing, err)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			if cause := context.Cause(req.Context()); cause != nil && !errors.Is(cause, context.DeadlineExceeded) && !errors.Is(cause, context.Canceled) {
				return nil, providerErr(StateError, "%v", cause)
			}
			return nil, providerErr(StateError, "the addon did not answer in time")
		}
		return nil, providerErr(StateError, "the addon's answer could not be read: %v", err)
	}
	return res, nil
}

// readAnswer reads a bounded JSON body into v.
func readAnswer(res *http.Response, v any) error {
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, providerMaxAnswer+1))
	if err != nil {
		return providerErr(StateError, "the addon's answer could not be read: %v", err)
	}
	if len(b) > providerMaxAnswer {
		return providerErr(StateError, "the addon's answer is larger than %d bytes", providerMaxAnswer)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var a providerAnswer
		if json.Unmarshal(b, &a) == nil && a.State != "" {
			return refusal(a)
		}
		return providerErr(StateError, "the addon answered HTTP %d", res.StatusCode)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return providerErr(StateError, "the addon's answer is not the contract's JSON: %v", err)
	}
	return nil
}

// refusal makes the error of an answer whose state is not writable.
func refusal(a providerAnswer) *ProviderError {
	e := &ProviderError{State: a.State, Message: a.Message.clean(300), Action: a.Action.clean()}
	if !providerStates[e.State] || e.State == StateWritable {
		e.Text = fmt.Sprintf("the addon answered a state the contract does not know: %q", a.State)
		e.State, e.Message, e.Action = StateError, nil, nil
		return e
	}
	e.Text = e.Message["en"]
	if e.Text == "" {
		e.Text = e.Message["de"]
	}
	return e
}

func (c *ProviderClient) url(path string, q url.Values) string {
	u := "http://provider" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// Info asks the provider for its destinations.
func (c *ProviderClient) Info(ctx context.Context) (ProviderInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, providerInfoTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/v1/info", nil), nil)
	res, err := c.do(req)
	if err != nil {
		return ProviderInfo{}, err
	}
	var info ProviderInfo
	if err := readAnswer(res, &info); err != nil {
		return ProviderInfo{}, err
	}
	if info.Protocol != ProviderProtocol {
		return ProviderInfo{}, providerErr(StateError, "the addon speaks protocol %d, this system %d", info.Protocol, ProviderProtocol)
	}
	out := ProviderInfo{Protocol: info.Protocol, Destinations: []Destination{}}
	for _, d := range info.Destinations {
		if !destIDRe.MatchString(d.ID) || len(out.Destinations) >= 64 {
			continue
		}
		if !providerStates[d.State] {
			d.State, d.Message, d.Action = StateError, nil, nil
		}
		d.Name, d.Detail = oneLine(d.Name, 64), oneLine(d.Detail, 120)
		if d.Name == "" {
			d.Name = d.ID
		}
		d.Message, d.Action = d.Message.clean(300), d.Action.clean()
		out.Destinations = append(out.Destinations, d)
	}
	return out, nil
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}

// ProviderTest is POST /v1/destinations/{id}/test's answer.
type ProviderTest struct {
	State      string          `json:"state"`
	Message    ProviderText    `json:"message,omitempty"`
	Action     *ProviderAction `json:"action,omitempty"`
	FreeBytes  int64           `json:"free_bytes,omitempty"`
	TotalBytes int64           `json:"total_bytes,omitempty"`
}

func destPath(dest, tail string) string {
	return "/v1/destinations/" + url.PathEscape(dest) + tail
}

// Test asks the provider to write, read back and delete a small file in the folder.
func (c *ProviderClient) Test(ctx context.Context, dest, folder string) (ProviderTest, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTestTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]string{"folder": folder})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.url(destPath(dest, "/test"), nil), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return ProviderTest{}, err
	}
	var t ProviderTest
	if err := readAnswer(res, &t); err != nil {
		return ProviderTest{}, err
	}
	if t.State != StateWritable {
		return ProviderTest{}, refusal(providerAnswer{State: t.State, Message: t.Message, Action: t.Action})
	}
	if t.FreeBytes < 0 || t.TotalBytes < 0 {
		t.FreeBytes, t.TotalBytes = 0, 0
	}
	t.Message, t.Action = nil, nil
	return t, nil
}

// ProviderPut is PUT …/files/{name}'s answer.
type ProviderPut struct {
	State string `json:"state"`
	Size  int64  `json:"size"`
	// Verified is how the provider checked the upload: "hash" (the destination's hash against the
	// one it computed while reading) or "size" (where the destination has none).
	Verified   string `json:"verified"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// IsProviderName says whether a file name is one the system hands to a provider: an encrypted
// backup, and nothing else.
func IsProviderName(name string) bool {
	return IsBackupName(name) && strings.HasSuffix(name, ".sbk.age") && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\")
}

func (c *ProviderClient) transferTimeout(size int64) time.Duration {
	min := providerMinTransfer
	if c.MinTransfer > 0 {
		min = c.MinTransfer
	}
	if d := time.Duration(size/providerMinRate) * time.Second; d > min {
		return d
	}
	return min
}

// stallReader counts what the provider took and notes when it last took something.
type stallReader struct {
	r    io.Reader
	sent atomic.Int64
	last atomic.Int64
	on   func(sent int64)
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.last.Store(time.Now().UnixNano())
		v := s.sent.Add(int64(n))
		if s.on != nil {
			s.on(v)
		}
	}
	return n, err
}

// Put streams one encrypted backup to the provider: the body with its length and its SHA-256, the
// provider's upload and verification, its answer. progress is called with the bytes the provider
// has taken so far (nil = not wanted).
func (c *ProviderClient) Put(ctx context.Context, dest, folder, name string, r io.Reader, size int64, sha256hex string, progress func(sent int64)) (ProviderPut, error) {
	if !IsProviderName(name) {
		return ProviderPut{}, fmt.Errorf("%w: %q is not an encrypted backup's name", ErrInvalid, name)
	}
	if size <= 0 {
		return ProviderPut{}, fmt.Errorf("%w: an empty file", ErrInvalid)
	}
	ctx, cancel := context.WithTimeoutCause(ctx, c.transferTimeout(size), errors.New("the upload took longer than its limit"))
	defer cancel()
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	sr := &stallReader{r: io.LimitReader(r, size), on: progress}
	sr.last.Store(time.Now().UnixNano())
	stall := providerStall
	if c.Stall > 0 {
		stall = c.Stall
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(stall / 4)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				// only while the body is still going out: afterwards the provider verifies, bounded
				// by the transfer's own limit
				if sr.sent.Load() < size && time.Since(time.Unix(0, sr.last.Load())) > stall {
					stop(fmt.Errorf("the addon took no data for %s", stall))
					return
				}
			}
		}
	}()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, c.url(destPath(dest, "/files/"+url.PathEscape(name)), url.Values{"folder": {folder}}), sr)
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-SHA256", sha256hex)
	res, err := c.do(req)
	if err != nil {
		return ProviderPut{}, err
	}
	var p struct {
		ProviderPut
		Message ProviderText    `json:"message"`
		Action  *ProviderAction `json:"action"`
	}
	if err := readAnswer(res, &p); err != nil {
		return ProviderPut{}, err
	}
	if p.State != StateWritable {
		return ProviderPut{}, refusal(providerAnswer{State: p.State, Message: p.Message, Action: p.Action})
	}
	if p.Size != size {
		return ProviderPut{}, providerErr(StateError, "the addon says it stored %d bytes of %d", p.Size, size)
	}
	if p.Verified != "hash" && p.Verified != "size" {
		return ProviderPut{}, providerErr(StateError, "the addon did not say how it verified the upload")
	}
	return p.ProviderPut, nil
}

// List asks for the files in the folder; only backups' names come back, newest first.
func (c *ProviderClient) List(ctx context.Context, dest, folder string) ([]BackupFile, error) {
	ctx, cancel := context.WithTimeout(ctx, providerListTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.url(destPath(dest, "/files"), url.Values{"folder": {folder}}), nil)
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	var l struct {
		Files []struct {
			Name string    `json:"name"`
			Size int64     `json:"size"`
			Time time.Time `json:"time"`
		} `json:"files"`
	}
	if err := readAnswer(res, &l); err != nil {
		return nil, err
	}
	out := []BackupFile{}
	for _, f := range l.Files {
		if !IsProviderName(f.Name) || f.Size < 0 || len(out) >= providerMaxFiles {
			continue
		}
		out = append(out, BackupFile{Name: f.Name, Size: f.Size, Time: f.Time, Encrypted: true})
	}
	return byNameTime(out), nil
}

// Get opens a backup on the destination; the caller closes it.
func (c *ProviderClient) Get(ctx context.Context, dest, folder, name string) (io.ReadCloser, int64, error) {
	if !IsProviderName(name) {
		return nil, 0, fmt.Errorf("%w: name %q", ErrInvalid, name)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.url(destPath(dest, "/files/"+url.PathEscape(name)), url.Values{"folder": {folder}}), nil)
	res, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, 0, readAnswer(res, nil)
	}
	return res.Body, res.ContentLength, nil
}

// Delete removes one backup on the destination (retention).
func (c *ProviderClient) Delete(ctx context.Context, dest, folder, name string) error {
	if !IsProviderName(name) {
		return fmt.Errorf("%w: name %q", ErrInvalid, name)
	}
	ctx, cancel := context.WithTimeout(ctx, providerListTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, c.url(destPath(dest, "/files/"+url.PathEscape(name)), url.Values{"folder": {folder}}), nil)
	res, err := c.do(req)
	if err != nil {
		return err
	}
	return readAnswer(res, nil)
}

// providerState is the state an error from a provider call means.
func providerState(err error) (string, bool) {
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe.State, true
	}
	if errors.Is(err, ErrAddonMissing) {
		return StateAddonMissing, true
	}
	return "", false
}

// NewProviderClient is how the package makes a client for a target (a test swaps it).
var NewProviderClient = func(addon string) *ProviderClient { return &ProviderClient{Addon: addon} }

// ProviderDir makes a provider's socket directory - as root, before the addon's unit starts:
// base/<id>, owned by occulite with the addon's group, 0770, emptied of a socket a previous run
// left. Without the users (a workstation) the directory is made and left as it is.
func ProviderDir(base, id string) error {
	if !providerIDRe.MatchString(id) {
		return fmt.Errorf("not an addon id: %q", id)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	dir := filepath.Join(base, id)
	if err := os.Mkdir(dir, 0o770); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	_ = os.Remove(filepath.Join(dir, ProviderSocketName))
	uid, gid := -1, -1
	if u, err := user.Lookup("occulite"); err == nil {
		uid, _ = strconv.Atoi(u.Uid)
		gid, _ = strconv.Atoi(u.Gid)
	}
	if g, err := user.LookupGroup(AddonUserPrefix + id); err == nil {
		gid, _ = strconv.Atoi(g.Gid)
	}
	if uid < 0 {
		return nil
	}
	if err := os.Lchown(dir, uid, gid); err != nil {
		return err
	}
	return os.Chmod(dir, 0o770)
}
