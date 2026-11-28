package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// occulited task 190: login times stay in memory, users.json is written once a day, before a
// backup and at Close - and not at all when a save would write the same bytes.

func storedUser(t *testing.T, dir, name string) User {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc usersDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, u := range doc.Users {
		if u.Name == name {
			return *u
		}
	}
	t.Fatalf("%s not in users.json", name)
	return User{}
}

// passkeyStore is a store with admin holding the passkey k1, already discoverable.
func passkeyStore(t *testing.T, dir string, clk *usageClock) *Store {
	t.Helper()
	s, err := Open(dir, Options{Now: clk.now})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Setup("admin", "secret123"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddWebAuthn("admin", keyRecord("k1", true), "phone"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUnchangedSaveWritesNothing(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	_ = s.Setup("admin", "secret123")
	p := Preferences{StartPage: "status"}
	if err := s.SetPreferences("admin", p); err != nil {
		t.Fatal(err)
	}
	secret, _ := s.CreateToken("poller", asRole(RoleUser))
	before := fileID(t, dir)
	for i := 0; i < 3; i++ {
		// a token in use between the saves: its time alone does not make the save a write
		clk.t = clk.t.Add(time.Minute)
		s.Validate(secret)
		if err := s.SetPreferences("admin", p); err != nil {
			t.Fatal(err)
		}
	}
	if !sameFile(before, fileID(t, dir)) {
		t.Fatal("the same preferences rewrote users.json")
	}
	if lu := listedToken(s, "poller").LastUsed; lu == nil || !lu.Equal(clk.t) {
		t.Fatalf("the use kept in memory is not listed: %v", lu)
	}
	if err := s.SetPreferences("admin", Preferences{StartPage: "app"}); err != nil {
		t.Fatal(err)
	}
	if sameFile(before, fileID(t, dir)) {
		t.Fatal("changed preferences were not written")
	}
	// another writer's change is still taken in and written back merged
	console, _ := Open(dir, Options{Now: clk.now})
	if _, err := console.CreateToken("console", asRole(RoleUser)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPreferences("admin", Preferences{StartPage: "app"}); err != nil {
		t.Fatal(err)
	}
	if len(s.Tokens()) != 2 {
		t.Fatal("the console's token was lost")
	}
	_ = storedToken(t, dir, "console")
	// the changed preferences carried the use along
	if lu := storedToken(t, dir, "poller").LastUsed; lu == nil || !lu.Equal(clk.t) {
		t.Fatalf("stored use %v", lu)
	}
}

func TestPasskeyLoginKeptInMemory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		after   func(s *Store, clk *usageClock)
		written bool
	}{
		{"nothing else", func(s *Store, clk *usageClock) {}, false},
		{"a sweep later that day", func(s *Store, clk *usageClock) { clk.t = clk.t.Add(11 * time.Hour); s.Sweep() }, false},
		{"a sweep after midnight", func(s *Store, clk *usageClock) { clk.t = clk.t.Add(12*time.Hour + time.Minute); s.Sweep() }, true},
		{"a backup", func(s *Store, clk *usageClock) { s.FlushStamps() }, true},
		{"a clean stop", func(s *Store, clk *usageClock) { s.Close() }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := &usageClock{time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
			dir := t.TempDir()
			s := passkeyStore(t, dir, clk)
			before := fileID(t, dir)
			clk.t = clk.t.Add(time.Minute)
			used := clk.t
			k := keyRecord("k1", true)
			k.Authenticator.SignCount = 41
			if _, err := s.CompleteLogin("admin", &k, MethodPasskey, "192.0.2.1", "t"); err != nil {
				t.Fatal(err)
			}
			if !sameFile(before, fileID(t, dir)) {
				t.Fatal("a passkey login wrote users.json")
			}
			if v := s.WebAuthnKeys("admin"); v[0].LastUsed == nil || !v[0].LastUsed.Equal(used) {
				t.Fatalf("listed last use %v", v[0].LastUsed)
			}
			// the next ceremony starts from the counter of this use
			if c := s.WebAuthnUser("admin").Credentials[0]; c.Authenticator.SignCount != 41 {
				t.Fatalf("the ceremony sees counter %d", c.Authenticator.SignCount)
			}
			tc.after(s, clk)
			if got := !sameFile(before, fileID(t, dir)); got != tc.written {
				t.Fatalf("written %v, want %v", got, tc.written)
			}
			if tc.written {
				c := storedUser(t, dir, "admin").WebAuthn[0]
				if c.LastUsed == nil || !c.LastUsed.Equal(used) || c.Key.Authenticator.SignCount != 41 {
					t.Fatalf("stored %v %d", c.LastUsed, c.Key.Authenticator.SignCount)
				}
			}
		})
	}
}

// A key's first discoverable login makes it a passkey: that is written at once.
func TestPasskeyBecomingDiscoverableIsWritten(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	_ = s.Setup("admin", "secret123")
	if _, err := s.AddWebAuthn("admin", keyRecord("k1", false), "key"); err != nil {
		t.Fatal(err)
	}
	before := fileID(t, dir)
	k := keyRecord("k1", true)
	if _, err := s.CompleteLogin("admin", &k, MethodPasskey, "192.0.2.1", "t"); err != nil {
		t.Fatal(err)
	}
	if sameFile(before, fileID(t, dir)) {
		t.Fatal("the key turned passkey was not written")
	}
	if c := storedUser(t, dir, "admin").WebAuthn[0]; !c.Passkey() {
		t.Fatal("not stored as a passkey")
	}
}

// The provider's first login of an account is written (occulited task 70 hides the password form
// only after one); the later ones are login times.
func TestProviderLoginTimes(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s, _ := Open(dir, Options{Now: clk.now})
	_ = s.Setup("admin", "secret123")
	before := fileID(t, dir)
	if _, err := s.LoginExternal(MethodOIDC, "admin", "192.0.2.1", "t"); err != nil {
		t.Fatal(err)
	}
	first := clk.t
	if sameFile(before, fileID(t, dir)) {
		t.Fatal("the first provider login was not written")
	}
	before = fileID(t, dir)
	clk.t = clk.t.Add(time.Hour)
	if _, err := s.LoginExternal(MethodOIDC, "admin", "192.0.2.1", "t"); err != nil {
		t.Fatal(err)
	}
	second := clk.t
	if !sameFile(before, fileID(t, dir)) {
		t.Fatal("a later provider login wrote users.json")
	}
	if u := s.Users()[0]; u.LastProviderLogin == nil || !u.LastProviderLogin.Equal(second) {
		t.Fatalf("listed %v", u.LastProviderLogin)
	}
	if u := storedUser(t, dir, "admin"); u.LastProviderLogin == nil || !u.LastProviderLogin.Equal(first) {
		t.Fatalf("stored %v", u.LastProviderLogin)
	}
	// the console sets a password meanwhile: the next save keeps it and adds the time
	if err := ConsoleSetPassword(dir, "admin", "console-secret"); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(13 * time.Hour)
	s.Sweep()
	u := storedUser(t, dir, "admin")
	if u.LastProviderLogin == nil || !u.LastProviderLogin.Equal(second) {
		t.Fatalf("after the sweep: stored %v", u.LastProviderLogin)
	}
	if !verifyPassword(u.Hash, "console-secret") {
		t.Fatal("the login time outvoted the console's password")
	}
}

// An account removed and made again under the name does not inherit the old one's times.
func TestStampsNotInherited(t *testing.T) {
	clk := &usageClock{time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)}
	dir := t.TempDir()
	s := passkeyStore(t, dir, clk)
	_ = s.CreateUser("bob", "secret123", RoleUser, false)
	_, _ = s.LoginExternal(MethodOIDC, "bob", "r", "a")
	clk.t = clk.t.Add(time.Minute)
	_, _ = s.LoginExternal(MethodOIDC, "bob", "r", "a") // kept in memory
	if err := s.DeleteUser("bob"); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Second)
	_ = s.CreateUser("bob", "secret123", RoleUser, false)
	s.Close()
	if u := storedUser(t, dir, "bob"); u.LastProviderLogin != nil {
		t.Fatalf("the new bob has the old one's login %v", u.LastProviderLogin)
	}
}
