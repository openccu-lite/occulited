package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// usageClock is a settable clock for the B-154 tests.
type usageClock struct{ t time.Time }

func (c *usageClock) now() time.Time { return c.t }

// fileID is users.json's identity: every write renames a new file into place.
func fileID(t *testing.T, dir string) os.FileInfo {
	t.Helper()
	st, err := os.Stat(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func sameFile(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// storedToken is the token name as users.json holds it.
func storedToken(t *testing.T, dir, name string) Token {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc usersDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, x := range doc.Tokens {
		if x.Name == name {
			return *x
		}
	}
	t.Fatalf("token %s not in users.json", name)
	return Token{}
}

func listedToken(s *Store, name string) Token {
	for _, x := range s.Tokens() {
		if x.Name == name {
			return x
		}
	}
	return Token{}
}

// occulited B-154: a request with a token never writes users.json - a program polling with one
// rewrote it every minute. The use is kept in memory, listed at once, and written by Sweep once
// UsageEvery has passed since the last save, by any other save, and at Close.
func TestTokenUseNotWrittenPerRequest(t *testing.T) {
	for _, tc := range []struct {
		name     string
		requests int           // one every 10 s
		sweepAt  time.Duration // after the token's creation; 0: no sweep
		written  bool          // users.json rewritten
	}{
		{"one request", 1, 0, false},
		{"an hour of polling", 360, 0, false},
		{"swept within the interval", 100, 30 * time.Minute, false},
		{"swept after the interval", 100, time.Hour, true},
		{"swept after the interval without a use", 0, 2 * time.Hour, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := &usageClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
			start := clk.t
			dir := t.TempDir()
			s, err := Open(dir, Options{Now: clk.now})
			if err != nil {
				t.Fatal(err)
			}
			secret, err := s.CreateToken("poller", asRole(RoleUser))
			if err != nil {
				t.Fatal(err)
			}
			before := fileID(t, dir)
			syncs := 0
			old := syncFile
			syncFile = func(f *os.File) error { syncs++; return f.Sync() }
			t.Cleanup(func() { syncFile = old })
			var last time.Time
			for i := 0; i < tc.requests; i++ {
				clk.t = clk.t.Add(10 * time.Second)
				if s.ValidateFrom(secret, "192.168.1.9") == nil {
					t.Fatal("not valid")
				}
				last = clk.t
			}
			if !sameFile(before, fileID(t, dir)) {
				t.Fatalf("users.json written by %d requests", tc.requests)
			}
			if tc.requests > 0 {
				if lu := listedToken(s, "poller").LastUsed; lu == nil || !lu.Equal(last) {
					t.Fatalf("listed last use %v, want %v", lu, last)
				}
			}
			if tc.sweepAt > 0 {
				clk.t = start.Add(tc.sweepAt)
				s.Sweep()
			}
			if got := !sameFile(before, fileID(t, dir)); got != tc.written {
				t.Fatalf("written %v, want %v", got, tc.written)
			}
			if tc.written {
				if lu := storedToken(t, dir, "poller").LastUsed; lu == nil || !lu.Equal(last) {
					t.Errorf("stored last use %v, want %v", lu, last)
				}
				if syncs != 0 {
					t.Errorf("the sweep's save synced %d times (B-146: no fsync under the lock)", syncs)
				}
				// written once: the next sweeps within the interval write nothing
				after := fileID(t, dir)
				clk.t = clk.t.Add(10 * time.Second)
				s.ValidateFrom(secret, "192.168.1.9")
				clk.t = clk.t.Add(5 * time.Minute)
				s.Sweep()
				if !sameFile(after, fileID(t, dir)) {
					t.Error("written again within the interval")
				}
			}
		})
	}
}

// occulited B-154: Close writes the use users.json does not have, synced, and a restart lists it.
func TestTokenUseWrittenAtClose(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	secret, _ := s.CreateToken("poller", asRole(RoleUser))
	clk.t = clk.t.Add(7 * time.Minute)
	s.Validate(secret)
	used := clk.t
	syncs := 0
	old := syncFile
	syncFile = func(f *os.File) error { syncs++; return f.Sync() }
	t.Cleanup(func() { syncFile = old })
	s.Close()
	if syncs == 0 {
		t.Error("Close's save was not synced")
	}
	r, err := Open(dir, Options{Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	if lu := listedToken(r, "poller").LastUsed; lu == nil || !lu.Equal(used) {
		t.Fatalf("after a restart: last use %v, want %v", lu, used)
	}
	// a Close with nothing new writes nothing
	before := fileID(t, dir)
	r.Close()
	if !sameFile(before, fileID(t, dir)) {
		t.Error("Close without a use wrote users.json")
	}
}

// occulited B-154: the use rides along with any other save, and never outvotes what another
// writer (the console) changed on the token meanwhile.
func TestTokenUseMergesWithOtherWrites(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	secret, _ := s.CreateToken("poller", asRole(RoleUser))
	clk.t = clk.t.Add(time.Minute)
	s.Validate(secret)
	used := clk.t
	// the console narrows the token while the daemon holds an unsaved use
	console, err := Open(dir, Options{Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := console.UpdateToken("poller", nil, []string{string(ScopeMetaRead)}); err != nil {
		t.Fatal(err)
	}
	// the daemon's next save (another token) carries the use and keeps the console's scopes
	if _, err := s.CreateToken("other", asRole(RoleUser)); err != nil {
		t.Fatal(err)
	}
	st := storedToken(t, dir, "poller")
	if st.LastUsed == nil || !st.LastUsed.Equal(used) {
		t.Errorf("stored last use %v, want %v", st.LastUsed, used)
	}
	if len(st.Scopes) != 1 || st.Scopes[0] != ScopeMetaRead {
		t.Errorf("the console's scopes were overwritten: %v", st.Scopes)
	}
	// nothing left to write
	before := fileID(t, dir)
	clk.t = clk.t.Add(2 * time.Hour)
	s.Sweep()
	if !sameFile(before, fileID(t, dir)) {
		t.Error("the sweep wrote a use that was already saved")
	}
}

// occulited B-154: a token removed and made again under its name does not inherit the old one's
// unsaved use; a removed one's use is dropped.
func TestTokenUseNotInherited(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	secret, _ := s.CreateToken("poller", asRole(RoleUser))
	clk.t = clk.t.Add(time.Minute)
	s.Validate(secret)
	// removed and made again by the console, the daemon unaware until its next read
	console, _ := Open(dir, Options{Now: clk.now})
	if err := console.DeleteToken("poller"); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Second)
	if _, err := console.CreateToken("poller", asRole(RoleUser)); err != nil {
		t.Fatal(err)
	}
	if lu := listedToken(s, "poller").LastUsed; lu != nil {
		t.Errorf("the new token lists the old one's use %v", lu)
	}
	s.Close()
	if lu := storedToken(t, dir, "poller").LastUsed; lu != nil {
		t.Errorf("the new token stored the old one's use %v", lu)
	}
	// deleted through the daemon: the use goes with it
	secret2, _ := s.CreateToken("gone", asRole(RoleUser))
	s.Validate(secret2)
	if err := s.DeleteToken("gone"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.usage["gone"]; ok {
		t.Error("a deleted token's use is kept")
	}
}

// occulited B-154 with task 219: a paired program's address is kept in memory with its use and
// written with it; a use without an address (ValidateAnyRange, B-129) keeps the address seen
// before and never records its stand-in.
func TestTokenUsePairedAddress(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	c := TokenClient{App: "hm2mqtt.js", PairedAt: clk.t, PairedBy: "admin", Address: "192.168.1.50"}
	name, secret, err := s.CreatePairedToken("hm2mqtt", Scopes{ScopeMetaRead}, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	before := fileID(t, dir)
	for _, tc := range []struct {
		validate func() *Session
		want     string
	}{
		{func() *Session { return s.ValidateFrom(secret, "192.168.1.77") }, "192.168.1.77"},
		{func() *Session { return s.ValidateAnyRange(secret) }, "192.168.1.77"},
		{func() *Session { return s.ValidateFrom(secret, "192.168.1.78") }, "192.168.1.78"},
		{func() *Session { return s.Validate(secret) }, "192.168.1.78"},
	} {
		clk.t = clk.t.Add(time.Second)
		if tc.validate() == nil {
			t.Fatal("not valid")
		}
		if got := listedToken(s, name).Client.LastAddress; got != tc.want {
			t.Fatalf("listed address %q, want %q", got, tc.want)
		}
	}
	if !sameFile(before, fileID(t, dir)) {
		t.Fatal("a moved address wrote users.json on the request path")
	}
	// the listing's copy does not share the store's client record
	l := listedToken(s, name)
	l.Client.LastAddress = "x"
	if listedToken(s, name).Client.LastAddress != "192.168.1.78" {
		t.Fatal("the listing shares the client record")
	}
	s.Close()
	if st := storedToken(t, dir, name); st.Client == nil || st.Client.LastAddress != "192.168.1.78" || st.LastUsed == nil || !st.LastUsed.Equal(clk.t) {
		t.Fatalf("stored %+v %v", st.Client, st.LastUsed)
	}
}
