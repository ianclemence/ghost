// Package credentials is Ghost's centralized credential vault.
//
// The repository already has credential/security work (AES-GCM
// credential store, secrets file, per-integration key readers). This
// package promotes it to a product-level subsystem: the normal
// application layer works with secure REFERENCES and lifecycle STATES,
// never raw secrets. Secrets cross the boundary only at the exact point
// of use (provider adapter), and never enter chat, events, activity,
// memory, logs, diagnostics, SSE, backups, or persisted messages.
//
// Borrowed pattern (OpenMausBot): keys-once central settings with
// write-only secrets — the UI only ever sees "configured" flags — plus
// shadow/unavailable states instead of startup failures.
package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/utils"
)

// Status is the credential lifecycle state (product-level, shared with
// readiness — no duplicate state system).
type Status string

const (
	StatusNotConfigured Status = "not_configured"
	StatusConfiguring   Status = "configuring"
	StatusConnected     Status = "connected"
	StatusExpired       Status = "expired"
	StatusRevoked       Status = "revoked"
	StatusInvalid       Status = "invalid"
	StatusError         Status = "error"
	StatusDisconnected  Status = "disconnected"
)

// Category groups credentials for the Connections UI.
type Category string

const (
	CategoryModel       Category = "model"
	CategoryChannel     Category = "channel"
	CategoryIntegration Category = "integration"
	CategoryProvider    Category = "provider"
)

// Credential is the metadata record. It NEVER carries secret values —
// JSON marshaling is safe for API responses, events, and diagnostics.
type Credential struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	DisplayName     string     `json:"display_name"`
	Category        Category   `json:"category"`
	Type            string     `json:"type"` // "api_key" | "oauth" | "token"
	Status          Status     `json:"status"`
	Capabilities    []string   `json:"capabilities,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	LastValidatedAt *time.Time `json:"last_validated_at,omitempty"`
}

// Known providers: id → display/category/type. The vault supports any id
// ("aviationstack", "openai", ...); this table seeds product metadata.
// OAuth-backed entries validate by presence of the token store, never by
// reading its contents.
type providerMeta struct {
	display  string
	category Category
	typ      string
	caps     []string
}

var knownProviders = map[string]providerMeta{
	"aviationstack":   {"Flight Data (AviationStack)", CategoryProvider, "api_key", []string{"flight.status"}},
	"aerodatabox":     {"Flight Data (AeroDataBox)", CategoryProvider, "api_key", []string{"flight.status"}},
	"openweather":     {"Weather (OpenWeather)", CategoryProvider, "api_key", []string{"weather.current"}},
	"google-calendar": {"Google Calendar", CategoryIntegration, "oauth", []string{"calendar.read"}},
	"openai":          {"OpenAI", CategoryModel, "api_key", []string{"chat"}},
	"anthropic":       {"Anthropic", CategoryModel, "api_key", []string{"chat"}},
	"telegram":        {"Telegram", CategoryChannel, "token", []string{"messaging"}},
	"homeassistant":   {"Home Assistant", CategoryIntegration, "token", []string{"hass.control"}},
	"spotify":         {"Spotify", CategoryIntegration, "oauth", []string{"media"}},
	"github":          {"GitHub", CategoryIntegration, "token", []string{"code"}},
	"notion":          {"Notion", CategoryIntegration, "token", []string{"docs"}},
	"slack":           {"Slack", CategoryChannel, "token", []string{"messaging"}},
}

// Emitter receives lifecycle events (nil-safe).
type Emitter func(eventType, provider string)

// Vault is the centralized credential authority.
type Vault struct {
	mu        sync.RWMutex
	configDir string
	status    map[string]Status
	meta      map[string]time.Time // last validated
	emit      Emitter
}

// New creates a vault rooted at configDir (secrets file location).
func New(configDir string) *Vault {
	return &Vault{configDir: configDir, status: map[string]Status{}, meta: map[string]time.Time{}}
}

// SetEmitter wires lifecycle events.
func (v *Vault) SetEmitter(e Emitter) { v.emit = e }

func (v *Vault) emitEvent(t, p string) {
	if v.emit != nil {
		v.emit(t, p)
	}
}

// secretValue resolves the raw secret server-side ONLY. Callers must use
// the value immediately (provider adapter) and never persist, log, or
// forward it. OAuth entries resolve presence, not content.
func (v *Vault) secretValue(id string) string {
	switch id {
	case "aviationstack":
		return skills.AviationKey(nil)
	case "aerodatabox":
		return skills.AeroDataBoxKey()
	case "openweather":
		return skills.OpenWeatherKey()
	case "google-calendar":
		if skills.CalendarWebStatus().Connected || skills.CalendarCheck().Connected {
			return "oauth:connected"
		}
		return ""
	}
	// Generic secrets-file + env fallback for future providers.
	dirs := []string{}
	if v.configDir != "" {
		dirs = append(dirs, v.configDir)
	}
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		dirs = append(dirs, d)
	}
	for _, d := range dirs {
		if s, err := config.LoadSecrets(filepath.Join(d, ".secrets.json")); err == nil && s != nil {
			if val := strings.TrimSpace(s.ProviderAPIKeys[id]); val != "" {
				return val
			}
		}
	}
	envKey := strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_API_KEY"
	if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
		return val
	}
	return ""
}

// Ref returns the safe metadata reference for normal application use.
func (v *Vault) Ref(id string) Credential {
	v.mu.RLock()
	override, hasOverride := v.status[id]
	last, hasLast := v.meta[id]
	v.mu.RUnlock()
	meta, known := knownProviders[id]
	if !known {
		meta = providerMeta{display: utils.Prettify(id), category: CategoryProvider, typ: "api_key"}
	}
	status := StatusNotConfigured
	if v.secretValue(id) != "" {
		status = StatusConnected
	}
	if hasOverride {
		status = override
		// An override stands until revalidation, except a live secret
		// always beats a stale not_configured.
		if override == StatusNotConfigured && v.secretValue(id) != "" {
			status = StatusConnected
		}
	}
	now := time.Now()
	cred := Credential{ID: id, Provider: id, DisplayName: meta.display,
		Category: meta.category, Type: meta.typ, Status: status,
		Capabilities: meta.caps, CreatedAt: now, UpdatedAt: now}
	if hasLast {
		t := last
		cred.LastValidatedAt = &t
	}
	return cred
}

// Use provides the raw secret to exactly one server-side function and
// returns its result. The secret never escapes except into fn — the
// narrowest possible boundary for provider adapters.
func (v *Vault) Use(id string, fn func(secret string) error) error {
	secret := v.secretValue(id)
	if secret == "" {
		return errors.New("credential not configured: " + id)
	}
	if secret == "oauth:connected" {
		return errors.New("oauth credential has no exportable secret: " + id)
	}
	return fn(secret)
}

// Store saves a key via the secrets file (0600, backup-excluded) and
// marks CONFIGURING until validated.
func (v *Vault) Store(id, value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("empty credential")
	}
	dir := v.configDir
	if dir == "" {
		dir = defaultConfigDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, ".secrets.json")
	s, err := config.LoadSecrets(path)
	if err != nil {
		return err
	}
	if s.ProviderAPIKeys == nil {
		s.ProviderAPIKeys = map[string]string{}
	}
	s.ProviderAPIKeys[id] = strings.TrimSpace(value)
	if err := config.SaveSecrets(path, s); err != nil {
		return err
	}
	v.mu.Lock()
	v.status[id] = StatusConfiguring
	v.mu.Unlock()
	return nil
}

// Validate proves the credential against the live provider via fn:
// nil error → CONNECTED (+ event); auth-shaped error → INVALID/REVOKED
// (+ event + product explanation available to callers); transport error
// → ERROR without changing a previously good state.
func (v *Vault) Validate(id string, fn func(secret string) error) Status {
	secret := v.secretValue(id)
	if secret == "" {
		v.setStatus(id, StatusNotConfigured, "")
		return StatusNotConfigured
	}
	if secret == "oauth:connected" {
		v.setStatus(id, StatusConnected, "credential.validated")
		return StatusConnected
	}
	now := time.Now()
	err := fn(secret)
	v.mu.Lock()
	defer v.mu.Unlock()
	prev := v.status[id]
	if err == nil {
		v.status[id] = StatusConnected
		v.meta[id] = now
		v.mu.Unlock()
		v.emitEvent("credential.validated", id)
		v.mu.Lock()
		return StatusConnected
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "revok") || strings.Contains(msg, "invalid_grant"):
		v.status[id] = StatusRevoked
		v.mu.Unlock()
		v.emitEvent("credential.revoked", id)
		v.mu.Lock()
	case strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401") ||
		strings.Contains(msg, "forbidden") || strings.Contains(msg, "403") ||
		strings.Contains(msg, "invalid") || strings.Contains(msg, "expired"):
		v.status[id] = StatusInvalid
		v.mu.Unlock()
		v.emitEvent("credential.invalid", id)
		v.mu.Lock()
	default:
		// Transport failure: keep prior good state, surface ERROR only
		// when nothing was known.
		if prev != StatusConnected {
			v.status[id] = StatusError
		}
	}
	return v.status[id]
}

// MarkAuthFailure records a 401/403 observed during normal API use:
// first failure → INVALID (reconnect prompt), preserving the
// don't-wait-forever rule without flapping on one blip? No — a single
// authoritative 401 means the credential is dead; mark immediately.
func (v *Vault) MarkAuthFailure(id string, revoked bool) {
	if revoked {
		v.setStatus(id, StatusRevoked, "credential.revoked")
	} else {
		v.setStatus(id, StatusInvalid, "credential.invalid")
	}
}

// Disconnect removes the secret (user disconnect flow) and marks state.
func (v *Vault) Disconnect(id string) error {
	if id == "google-calendar" {
		if err := skills.CalendarWebDisconnect(); err != nil {
			return err
		}
		_ = skills.CalendarDisconnect()
		v.setStatus(id, StatusDisconnected, "credential.disconnected")
		return nil
	}
	dir := v.configDir
	if dir == "" {
		dir = defaultConfigDir()
	}
	path := filepath.Join(dir, ".secrets.json")
	s, err := config.LoadSecrets(path)
	if err != nil {
		return err
	}
	delete(s.ProviderAPIKeys, id)
	if err := config.SaveSecrets(path, s); err != nil {
		return err
	}
	v.setStatus(id, StatusDisconnected, "credential.disconnected")
	return nil
}

// List returns safe metadata for every known + configured provider (the
// Connections screen model: status, never values).
func (v *Vault) List() []Credential {
	ids := map[string]bool{}
	for id := range knownProviders {
		ids[id] = true
	}
	// Include any extra configured ids found on disk.
	for _, d := range v.secretDirs() {
		if s, err := config.LoadSecrets(filepath.Join(d, ".secrets.json")); err == nil && s != nil {
			for id := range s.ProviderAPIKeys {
				ids[id] = true
			}
		}
	}
	out := make([]Credential, 0, len(ids))
	for id := range ids {
		out = append(out, v.Ref(id))
	}
	return out
}

func (v *Vault) setStatus(id string, st Status, event string) {
	v.mu.Lock()
	v.status[id] = st
	v.mu.Unlock()
	if event != "" {
		v.emitEvent(event, id)
	}
}

func (v *Vault) secretDirs() []string {
	dirs := []string{}
	if v.configDir != "" {
		dirs = append(dirs, v.configDir)
	}
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		dirs = append(dirs, d)
	}
	return dirs
}

func defaultConfigDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".config", "ghost")
	}
	return "./config"
}

