package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// occulited task 190: the session store's live copy in /run. A login, a logout of a session the
// userfs copy does not hold and a last-seen time write only the live copy; the userfs copy follows
// at a clean stop, once a day and when a session it holds ends.

// runStore is one process over dir with its live copy in dir/run-state/sessions.json, on *now.
func runStore(t *testing.T, dir string, now *time.Time) *Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "run-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, Options{SessionDir: filepath.Join(dir, "mirror"), SessionFile: storeFile(dir), SessionRunFile: runFile(dir), RestoreMethods: bothMethods,
		Now: func() time.Time { return *now }, IdleTimeout: 24 * time.Hour, MaxAge: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func runFile(dir string) string { return filepath.Join(dir, "run-state", "sessions.json") }

// diskState is the userfs copy as it is now ("" when there is none): a write renames a new file
// into place, so the identity and the content tell a write apart.
func diskState(t *testing.T, dir string) string {
	t.Helper()
	st, err := os.Stat(storeFile(dir))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(storeFile(dir))
	return fmt.Sprintf("%s|%d|%s", st.ModTime(), st.Sys().(*syscall.Stat_t).Ino, b)
}

func hashesOf(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hashesIn(b)
}

func TestSessionRunCopy(t *testing.T) {
	for _, tc := range []struct {
		name string
		// after a login at noon and the userfs state noted, step does something and says whether
		// the userfs copy must have been written by it
		step    func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store
		written bool
	}{
		{"requests within the day", func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store {
			for i := 0; i < 30; i++ {
				*now = now.Add(15 * time.Minute) // a last-seen write every other request
				s.Validate(sess.ID)
				s.Sweep()
			}
			return s
		}, false},
		{"a second login and its logout", func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store {
			other := mustLogin(t, s, "admin", "secret123")
			s.Logout(other.ID)
			return s
		}, false},
		{"a sweep after midnight", func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store {
			*now = now.Add(12*time.Hour + time.Minute)
			s.Validate(sess.ID)
			s.Sweep()
			return s
		}, true},
		{"a clean stop", func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store {
			s.Close()
			return s
		}, true},
		{"a crash and a restart", func(t *testing.T, dir string, s *Store, sess *Session, now *time.Time) *Store {
			*now = now.Add(time.Minute)
			return runStore(t, dir, now)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
			dir := t.TempDir()
			s := runStore(t, dir, &now)
			if err := s.Setup("admin", "secret123"); err != nil {
				t.Fatal(err)
			}
			sess := mustLogin(t, s, "admin", "secret123")
			if !hashesOf(t, runFile(dir))[sessionKey(sess.ID)] {
				t.Fatal("the login is not in the live copy")
			}
			before := diskState(t, dir)
			if before != "" {
				t.Fatalf("a login wrote the userfs copy: %q", before)
			}
			s = tc.step(t, dir, s, sess, &now)
			after := diskState(t, dir)
			if got := after != before; got != tc.written {
				t.Fatalf("userfs copy written %v, want %v", got, tc.written)
			}
			if tc.written && !hashesOf(t, storeFile(dir))[sessionKey(sess.ID)] {
				t.Error("the userfs copy lacks the session")
			}
			if s.Validate(sess.ID) == nil {
				t.Error("the session is gone")
			}
		})
	}
}

// A restart reads the live copy; a reboot - /run empty - the userfs copy, which holds what the
// last clean stop or daily flush wrote.
func TestSessionRunCopyRestartAndReboot(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	a := runStore(t, dir, &now)
	_ = a.Setup("admin", "secret123")
	first := mustLogin(t, a, "admin", "secret123")
	a.Close() // the userfs copy: first
	now = now.Add(time.Minute)
	b := runStore(t, dir, &now) // a restart: the live copy
	second := mustLogin(t, b, "admin", "secret123")
	now = now.Add(time.Minute)
	c := runStore(t, dir, &now) // a crash and restart: both from the live copy
	if c.Validate(first.ID) == nil || c.Validate(second.ID) == nil {
		t.Fatal("a restart lost a session of the live copy")
	}
	// a power loss: /run is gone, the userfs copy has the first session only
	if err := os.RemoveAll(filepath.Join(dir, "run-state")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	d := runStore(t, dir, &now)
	if d.Validate(first.ID) == nil {
		t.Error("the reboot lost the session of the last clean stop")
	}
	if d.Validate(second.ID) != nil {
		t.Error("a session opened after the last flush survived a power loss")
	}
	if !hashesOf(t, runFile(dir))[sessionKey(first.ID)] {
		t.Error("the reboot's start did not write the live copy")
	}
	// a clean reboot: Close writes both, a new boot restores both
	third := mustLogin(t, d, "admin", "secret123")
	d.Close()
	_ = os.RemoveAll(filepath.Join(dir, "run-state"))
	e := runStore(t, dir, &now)
	if e.Validate(first.ID) == nil || e.Validate(third.ID) == nil {
		t.Error("a clean reboot lost a session")
	}
}

// A session the userfs copy holds that ends takes the userfs copy along at once: a power loss
// never brings it back. One that ends before the userfs copy ever held it writes nothing there.
func TestSessionRunCopyRevocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(s *Store, sess *Session, now *time.Time)
	}{
		{"logout", func(s *Store, sess *Session, now *time.Time) { s.Logout(sess.ID) }},
		{"ended from the list", func(s *Store, sess *Session, now *time.Time) { s.LogoutUser("admin", "") }},
		{"expired", func(s *Store, sess *Session, now *time.Time) { *now = now.Add(25 * time.Hour); s.Sweep() }},
		{"account removed", func(s *Store, sess *Session, now *time.Time) {
			_ = s.CreateUser("other", "secret123", RoleAdmin, false)
			_ = s.DeleteUser("admin")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
			dir := t.TempDir()
			s := runStore(t, dir, &now)
			_ = s.Setup("admin", "secret123")
			sess := mustLogin(t, s, "admin", "secret123")
			s.Close() // the userfs copy holds it
			now = now.Add(time.Minute)
			s = runStore(t, dir, &now)
			if !hashesOf(t, storeFile(dir))[sessionKey(sess.ID)] {
				t.Fatal("setup: not in the userfs copy")
			}
			tc.end(s, sess, &now)
			if hashesOf(t, storeFile(dir))[sessionKey(sess.ID)] {
				t.Fatal("the ended session is still in the userfs copy")
			}
			// the power loss right after: the session does not come back
			_ = os.RemoveAll(filepath.Join(dir, "run-state"))
			if r := runStore(t, dir, &now); r.Validate(sess.ID) != nil {
				t.Fatal("an ended session came back after a power loss")
			}
		})
	}
}

// The live copy that cannot be used is passed over for the userfs copy; a missing runtime
// directory (an older unit) writes every change to the userfs copy, as before task 190.
func TestSessionRunCopyFallbacks(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	a := runStore(t, dir, &now)
	_ = a.Setup("admin", "secret123")
	sess := mustLogin(t, a, "admin", "secret123")
	a.Close()
	if err := os.WriteFile(runFile(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := captureLog(t)
	b := runStore(t, dir, &now)
	if b.Validate(sess.ID) == nil {
		t.Fatalf("a corrupt live copy lost the userfs copy's session; log: %s", log)
	}
	if !hashesOf(t, runFile(dir))[sessionKey(sess.ID)] {
		t.Error("the corrupt live copy was not replaced")
	}

	// no runtime directory
	dir2 := t.TempDir()
	c, err := Open(dir2, Options{SessionFile: storeFile(dir2), SessionRunFile: filepath.Join(dir2, "missing", "sessions.json"), RestoreMethods: bothMethods, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Setup("admin", "secret123")
	s2 := mustLogin(t, c, "admin", "secret123")
	if !hashesOf(t, storeFile(dir2))[sessionKey(s2.ID)] {
		t.Error("without a runtime directory the login did not reach the userfs copy")
	}
	if _, err := os.Stat(filepath.Join(dir2, "missing")); !os.IsNotExist(err) {
		t.Error("the missing runtime directory was made")
	}
}

// The console's password change ends the account's sessions in both copies.
func TestConsoleEndsSessionsInRunCopy(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	old := consoleSessionRun
	consoleSessionRun = runFile(dir)
	t.Cleanup(func() { consoleSessionRun = old })
	a := runStore(t, dir, &now)
	_ = a.Setup("admin", "secret123")
	first := mustLogin(t, a, "admin", "secret123")
	a.Close()
	second := mustLogin(t, a, "admin", "secret123") // the live copy only
	if err := ConsoleSetPassword(dir, "admin", "another-secret"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{storeFile(dir), runFile(dir)} {
		h := hashesOf(t, p)
		if h[sessionKey(first.ID)] || h[sessionKey(second.ID)] {
			t.Errorf("%s still holds a session of the account", p)
		}
	}
}

func TestSessionRunPath(t *testing.T) {
	if got := SessionRunPath("/run/occulite/sessions"); got != "/run/occulite/state/sessions.json" {
		t.Fatal(got)
	}
	if consoleSessionRun != "/run/occulite/state/sessions.json" {
		t.Fatal(consoleSessionRun)
	}
}
