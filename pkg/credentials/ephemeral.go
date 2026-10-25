package credentials

import (
	"sync"
	"time"
)

// A sign-in the owner typed into a request card but chose not to save is for
// one task. It lives in memory only, for a short while, under the site it was
// typed for, and is gone when Ghost restarts. It is read through the same door
// as a saved login, so the browser needs no second path, and its password is
// handed to the scrubber like any other secret.

const ephemeralTTL = 20 * time.Minute

type ephemeralLogin struct {
	login WebLogin
	until time.Time
}

var (
	ephemeralMu     sync.Mutex
	ephemeralLogins = map[string]ephemeralLogin{}
)

// PutEphemeralWebLogin holds a sign-in for a site for a short while, unsaved.
func PutEphemeralWebLogin(l WebLogin) error {
	l.Host = NormalizeWebHost(firstNonEmpty(l.Host, l.URL))
	if l.Host == "" || l.Username == "" || l.Password == "" {
		return errNeedsLogin
	}
	if l.URL == "" {
		l.URL = "https://" + l.Host
	}
	ephemeralMu.Lock()
	defer ephemeralMu.Unlock()
	ephemeralLogins[l.Host] = ephemeralLogin{login: l, until: time.Now().Add(ephemeralTTL)}
	invalidateSecretCache()
	return nil
}

func ephemeralWebLogin(host string) (WebLogin, bool) {
	host = NormalizeWebHost(host)
	ephemeralMu.Lock()
	defer ephemeralMu.Unlock()
	e, ok := ephemeralLogins[host]
	if !ok {
		return WebLogin{}, false
	}
	if time.Now().After(e.until) {
		delete(ephemeralLogins, host)
		return WebLogin{}, false
	}
	return e.login, true
}

// ephemeralSecrets are the passwords currently held in memory, for the scrubber.
func ephemeralSecrets() []string {
	ephemeralMu.Lock()
	defer ephemeralMu.Unlock()
	var out []string
	now := time.Now()
	for host, e := range ephemeralLogins {
		if now.After(e.until) {
			delete(ephemeralLogins, host)
			continue
		}
		out = append(out, e.login.Password)
	}
	return out
}

// ForgetEphemeralWebLogin drops a held sign-in at once.
func ForgetEphemeralWebLogin(host string) {
	ephemeralMu.Lock()
	delete(ephemeralLogins, NormalizeWebHost(host))
	ephemeralMu.Unlock()
	invalidateSecretCache()
}
