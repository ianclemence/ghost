package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// Named model connections.
//
// A connection names where a model lives and which environment variable
// carries its credential. Resolution reads the secret from the process
// environment at call time — configs stay committable, and `CONN=...`
// overrides apply per-invocation without rewriting files. Local
// loopback HTTP is allowed for Ollama-style servers; anything remote
// requires HTTPS.
//
// Fail-closed rules (fx discipline):
//   - a missing required credential errors; never silently anonymous,
//     never silently another provider.
//   - an invalid connection fails model startup; no Gateway fallback.
//   - unknown cost is never treated as zero (see lab.Usage).
//   - sessions pin the connection fingerprint: resuming against a
//     different destination requires explicit re-attachment.
type ModelConnection struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url,omitempty"`
	AuthEnv  string `json:"auth_env,omitempty"`
	AuthType string `json:"auth_type,omitempty"` // bearer | none; default bearer when AuthEnv set
}

// ResolvedConnection is a connection with its credential materialized
// (in memory only, never persisted). AuthEnv names the variable, never
// the value.
type ResolvedConnection struct {
	Name     string
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
	AuthType string
	AuthEnv  string
}

// FindConnection returns the named connection, or nil.
func (c *Config) FindConnection(name string) *ModelConnection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Agents.Connections {
		if c.Agents.Connections[i].Name == name {
			conn := c.Agents.Connections[i]
			return &conn
		}
	}
	return nil
}

// ResolveConnection validates a named connection and materializes its
// credential from the environment. A connection naming AuthEnv without
// the variable set fails closed; AuthType none sends no credential and
// is only valid for loopback endpoints.
func (c *Config) ResolveConnection(name string) (*ResolvedConnection, error) {
	conn := c.FindConnection(name)
	if conn == nil {
		return nil, fmt.Errorf("unknown model connection %q", name)
	}
	if strings.TrimSpace(conn.Provider) == "" || strings.TrimSpace(conn.Model) == "" {
		return nil, fmt.Errorf("connection %q names no provider/model (invalid profile)", name)
	}
	authType := strings.ToLower(strings.TrimSpace(conn.AuthType))
	if authType == "" && conn.AuthEnv != "" {
		authType = "bearer"
	}
	rc := &ResolvedConnection{Name: conn.Name, Provider: conn.Provider, Model: conn.Model, BaseURL: conn.BaseURL, AuthType: authType, AuthEnv: conn.AuthEnv}
	if conn.AuthEnv != "" {
		key := strings.TrimSpace(os.Getenv(conn.AuthEnv))
		if key == "" {
			return nil, fmt.Errorf("connection %q needs %s set (no silent fallback)", name, conn.AuthEnv)
		}
		rc.APIKey = key
	} else if authType == "" || authType == "none" {
		rc.AuthType = "none"
		if conn.BaseURL != "" && !isLoopback(conn.BaseURL) && !strings.HasPrefix(strings.ToLower(conn.BaseURL), "https://") {
			return nil, fmt.Errorf("connection %q: anonymous auth requires loopback or HTTPS", name)
		}
	}
	return rc, nil
}

// Fingerprint identifies the destination without key material:
// provider, model, endpoint, and auth shape. Sessions pin it; a resume
// against a different fingerprint must re-attach explicitly instead of
// silently continuing elsewhere.
func (r *ResolvedConnection) Fingerprint() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{r.Provider, r.Model, r.BaseURL, r.AuthType, hasKey(r.APIKey)}, "|")))
	return hex.EncodeToString(sum[:])[:16]
}

func hasKey(k string) string {
	if k != "" {
		return "keyed"
	}
	return "anon"
}

// CheckResume blocks silent resume across destinations: a session
// pinned to one fingerprint continues only on the same fingerprint.
func CheckResume(pinned, current string) error {
	if pinned == "" {
		return nil // unpinned sessions predate fingerprinting
	}
	if current == "" {
		return fmt.Errorf("session pinned to %s but no connection resolved (re-attach explicitly)", pinned)
	}
	if pinned != current {
		return fmt.Errorf("session destination changed (%s -> %s); re-attach explicitly, never resume silently", pinned, current)
	}
	return nil
}

func isLoopback(url string) bool {
	l := strings.ToLower(url)
	for _, host := range []string{"localhost", "127.0.0.1", "[::1]", "::1"} {
		if strings.Contains(l, "://"+host) || strings.Contains(l, "://"+host+":") {
			return true
		}
	}
	return false
}
