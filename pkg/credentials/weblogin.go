package credentials

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Website logins.
//
// A website login is an owner-saved sign-in for a site Ghost's browser should
// be able to reach: the login-page URL, the username and password, and
// optional CSS selectors for the fields. It is stored as one sealed JSON blob
// under "weblogin:<host>" in the same vault as every other credential, so the
// model never sees it, it never appears in chat, and deleting the entry is a
// revocation. The browser fills forms from this store; nothing else reads it.

// webLoginIDPrefix namespaces website logins inside the credential store.
const webLoginIDPrefix = "weblogin:"

// WebLogin is one saved sign-in. Password is never returned to callers that
// render; use WebLoginFor only inside the browser tool.
type WebLogin struct {
	Host             string `json:"host"`
	URL              string `json:"url"`
	Username         string `json:"username,omitempty"`
	Password         string `json:"password,omitempty"`
	UsernameSelector string `json:"username_selector,omitempty"`
	PasswordSelector string `json:"password_selector,omitempty"`
	SubmitSelector   string `json:"submit_selector,omitempty"`
}

// WebLoginMeta is the owner-facing view: enough to recognise the entry, never
// a secret. The username is masked.
type WebLoginMeta struct {
	Host     string `json:"host"`
	URL      string `json:"url"`
	Username string `json:"username"` // masked
}

func webLoginID(host string) string { return webLoginIDPrefix + NormalizeWebHost(host) }

// NormalizeWebHost reduces a URL or host to a bare lowercase hostname.
func NormalizeWebHost(raw string) string {
	h := strings.TrimSpace(strings.ToLower(raw))
	if h == "" {
		return ""
	}
	if strings.Contains(h, "://") {
		if u, err := url.Parse(h); err == nil && u.Hostname() != "" {
			return strings.TrimPrefix(u.Hostname(), "www.")
		}
	}
	if i := strings.IndexAny(h, "/?#"); i >= 0 {
		h = h[:i]
	}
	h = strings.TrimPrefix(h, "www.")
	if i := strings.LastIndex(h, ":"); i >= 0 && !strings.Contains(h[i+1:], "]") {
		// Strip a port unless this looks like an IPv6 literal.
		if !strings.Contains(h[:i], ":") {
			h = h[:i]
		}
	}
	return h
}

func maskUsername(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if len(u) == 1 {
		return "***"
	}
	return u[:1] + "***"
}

// configDirForWrites resolves the config directory whose .secrets.json is the
// live vault: an explicit GHOST_CONFIG_DIR first, then the first directory
// that already holds a secrets file, then the first standard candidate.
func configDirForWrites() string {
	dirs := secretDirs()
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		return d
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, ".secrets.json")); err == nil {
			return d
		}
	}
	if len(dirs) > 0 {
		return dirs[0]
	}
	return "."
}

// SaveWebLogin stores (or replaces) a website login. The URL must be http(s).
func SaveWebLogin(l WebLogin) error {
	l.Host = NormalizeWebHost(l.Host)
	if l.Host == "" {
		l.Host = NormalizeWebHost(l.URL)
	}
	if l.Host == "" {
		return fmt.Errorf("a website login needs a URL or host")
	}
	if l.URL == "" {
		l.URL = "https://" + l.Host
	}
	u, err := url.Parse(l.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("login URL must be an http(s) address")
	}
	if strings.TrimSpace(l.Username) == "" || l.Password == "" {
		return fmt.Errorf("a website login needs a username and password")
	}
	blob, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return New(configDirForWrites()).Store(webLoginID(l.Host), string(blob))
}

// WebLoginFor returns the saved sign-in for a host (empty = none). Reads go
// through the Vault so no code outside the credential boundary touches the
// raw secret store.
func WebLoginFor(host string) (WebLogin, bool) {
	raw := New(configDirForWrites()).secretValue(webLoginID(host))
	if raw == "" {
		return WebLogin{}, false
	}
	var l WebLogin
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return WebLogin{}, false
	}
	return l, true
}

// ListWebLogins returns the owner-facing metadata for every saved login,
// host-sorted. Reads go through the Vault; secrets never leave this package.
func ListWebLogins() []WebLoginMeta {
	v := New(configDirForWrites())
	seen := map[string]bool{}
	var out []WebLoginMeta
	for _, c := range v.List() {
		if !strings.HasPrefix(c.ID, webLoginIDPrefix) {
			continue
		}
		raw := v.secretValue(c.ID)
		if raw == "" {
			continue
		}
		var l WebLogin
		if json.Unmarshal([]byte(raw), &l) != nil {
			continue
		}
		host := NormalizeWebHost(l.Host)
		if host == "" {
			host = strings.TrimPrefix(c.ID, webLoginIDPrefix)
		}
		if seen[host] {
			continue
		}
		seen[host] = true
		out = append(out, WebLoginMeta{Host: host, URL: l.URL, Username: maskUsername(l.Username)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

// DeleteWebLogin revokes a saved sign-in.
func DeleteWebLogin(host string) error {
	return New(configDirForWrites()).Disconnect(webLoginID(host))
}
