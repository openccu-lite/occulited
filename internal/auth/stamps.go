package auth

import (
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Login times in memory (occulited task 190). A passkey login and an identity provider's login
// used to rewrite users.json on the spot - on an SD card, a burst of writes for a time stamp. They
// are kept here instead, like the tokens' use (B-154): every save of users.json folds them in, and
// Sweep writes them once a day, FlushStamps before a backup, Close at a clean stop. A power loss
// loses at most that day's times. What is not a time is still written at once: a passkey's first
// discoverable login (it becomes a passkey), an account's first provider login (the password form
// may be hidden only after one, occulited task 70).

// userStamp is one account's login times that users.json does not hold yet.
type userStamp struct {
	created  time.Time // the account's: an account removed and made again under the name does not inherit them
	provider time.Time // the identity provider's last login; zero: none kept
	keys     map[string]keyStamp
}

// keyStamp is a passkey's latest use (by its credential id): the time, and the counter and flags
// the library answered, which the next ceremony must start from.
type keyStamp struct {
	at        time.Time
	signCount uint32
	flags     webauthn.CredentialFlags
}

// stampOf is the account's entry, made when missing; nil for an account the store does not have.
func (s *Store) stampOf(u *User) *userStamp {
	st := s.stamps[u.Name]
	if st == nil || !st.created.Equal(u.Created) {
		st = &userStamp{created: u.Created}
		s.stamps[u.Name] = st
	}
	return st
}

// stampFor is the account's entry when it belongs to this account, else nil.
func (s *Store) stampFor(u *User) *userStamp {
	if st := s.stamps[u.Name]; st != nil && st.created.Equal(u.Created) {
		return st
	}
	return nil
}

// applyStamps writes the login times kept in memory into the accounts, before a save. A time of an
// account or a key that is gone, or was made again under its name, is dropped.
func (s *Store) applyStamps() {
	for _, u := range s.users {
		st := s.stampFor(u)
		if st == nil {
			continue
		}
		if !st.provider.IsZero() && (u.LastProviderLogin == nil || st.provider.After(*u.LastProviderLogin)) {
			at := st.provider
			u.LastProviderLogin = &at
		}
		for _, c := range u.WebAuthn {
			if k, ok := st.keys[string(c.Key.ID)]; ok {
				k.applyTo(c)
			}
		}
	}
}

// applyTo writes the use into the stored key, unless the key holds a later one.
func (k keyStamp) applyTo(c *WebAuthnCredential) {
	if c.LastUsed != nil && !k.at.After(*c.LastUsed) {
		return
	}
	at := k.at
	c.LastUsed = &at
	c.Key.Authenticator.SignCount = k.signCount
	c.Key.Flags = k.flags
}

// stampedKey is the key as the ceremony and the listings must see it: with its latest use.
func (s *Store) stampedKey(u *User, c *WebAuthnCredential) WebAuthnCredential {
	out := *c
	if st := s.stampFor(u); st != nil {
		if k, ok := st.keys[string(c.Key.ID)]; ok {
			k.applyTo(&out)
		}
	}
	return out
}

// providerLogin is the account's last provider login as the listings must see it.
func (s *Store) providerLogin(u *User) *time.Time {
	if st := s.stampFor(u); st != nil && !st.provider.IsZero() && (u.LastProviderLogin == nil || st.provider.After(*u.LastProviderLogin)) {
		at := st.provider
		return &at
	}
	return u.LastProviderLogin
}
