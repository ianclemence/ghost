package cards

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// A credentials card is Ghost saying "I need to sign in to X, and I have no
// sign-in for it", with a form the owner fills in on their phone. The card
// carries what the form needs to say and nothing the owner types: the phone
// sends the username and password to the Pod's Vault directly, and the card
// afterwards records only that a sign-in was given.

// Limits on what a request may say.
const (
	MaxCredentialService = 80
	MaxCredentialReason  = 280

	// credentialRequestTTL is how long the form stays open. A sign-in asked for
	// is needed now; an old form is a stale request, not a standing one.
	credentialRequestTTL = 30 * time.Minute
)

var credentialHostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// CredentialHost reads the site a request is for: the host of a URL or a bare
// host, lower case, without www or a port. "" when it is not a site name.
func CredentialHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if strings.Contains(h, "://") {
		if u, err := url.Parse(h); err == nil {
			h = u.Hostname()
		}
	}
	if i := strings.IndexAny(h, "/?#:"); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimPrefix(h, "www.")
	if !credentialHostRe.MatchString(h) {
		return ""
	}
	return h
}

// NewCredentialRequest builds the card. service is the name the owner knows the
// site by ("Google Ads"); reason says what Ghost will do once signed in.
func NewCredentialRequest(site, service, reason string) (Card, error) {
	host := CredentialHost(site)
	if host == "" {
		return Card{}, fmt.Errorf("%q is not a site address", site)
	}
	service = strings.TrimSpace(service)
	if service == "" {
		service = host
	}
	if len([]rune(service)) > MaxCredentialService {
		service = string([]rune(service)[:MaxCredentialService])
	}
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > MaxCredentialReason {
		reason = string([]rune(reason)[:MaxCredentialReason])
	}
	c := Card{
		ID: newID(), Kind: KindCredentials,
		Title: "Sign in to " + service,
		Body:  reason,
		Data:  map[string]interface{}{"host": host, "service": service, "url": "https://" + host},
		Actions: []Action{
			{ID: "submitted", Label: "Signed in", Style: "primary", Kind: "act"},
			{ID: "dismiss", Label: "Not now", Style: "secondary", Kind: "dismiss"},
		},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(credentialRequestTTL),
	}
	if err := c.Validate(); err != nil {
		return Card{}, err
	}
	return c, nil
}

func validateCredentialData(d map[string]interface{}) error {
	host, _ := d["host"].(string)
	if CredentialHost(host) != host || host == "" {
		return errors.New("a credentials card names the site it is for")
	}
	return nil
}

// CredentialHostOf is the site a credentials card is for, or "".
func CredentialHostOf(c Card) string {
	if c.Kind != KindCredentials {
		return ""
	}
	h, _ := c.Data["host"].(string)
	return h
}

// CredentialServiceOf is the name the card shows for the site.
func CredentialServiceOf(c Card) string {
	s, _ := c.Data["service"].(string)
	return s
}
