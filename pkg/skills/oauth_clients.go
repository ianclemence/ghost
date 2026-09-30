package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// Sign-in to Google, Microsoft and Spotify needs an app registered with them.
// Ghost does not ship one, so the owner registers their own (a few minutes,
// free) and gives Ghost its ID and secret. They are kept sealed, outside the
// backups, like the tokens they produce, and are readable by both the console
// and the daemon so a token can be refreshed long after sign-in.
//
// Environment variables still win when set, for people who deploy that way.

// Providers. Google covers Calendar and Gmail: one app, both.
const (
	ProviderGoogle    = "google"
	ProviderMicrosoft = "microsoft"
	ProviderSpotify   = "spotify"
)

// OAuthClient is the owner's registered app.
type OAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Tenant       string `json:"tenant,omitempty"` // Microsoft only
}

var ErrUnknownProvider = errors.New("unknown sign-in provider")

// DefaultRedirect is the address the owner registers with the provider. It is
// a loopback address: after approving, the browser lands on a page that will
// not load (nothing listens there) and the owner pastes that address back.
// Providers accept this form for apps that run on the owner's own devices.
func DefaultRedirect(provider string) string {
	switch provider {
	case ProviderSpotify:
		return "http://127.0.0.1:8888/callback" // Spotify accepts only the IP form
	default:
		return "http://localhost"
	}
}

func oauthClientsPath() string { return filepath.Join(CalendarTokenDir(), "oauth-clients.json") }

var envKeys = map[string][3]string{
	ProviderGoogle:    {"GHOST_GOOGLE_CLIENT_ID", "GHOST_GOOGLE_CLIENT_SECRET", ""},
	ProviderMicrosoft: {"GHOST_OUTLOOK_CLIENT_ID", "GHOST_OUTLOOK_CLIENT_SECRET", "GHOST_OUTLOOK_TENANT"},
	ProviderSpotify:   {"GHOST_SPOTIFY_CLIENT_ID", "GHOST_SPOTIFY_CLIENT_SECRET", ""},
}

func loadOAuthClients() map[string]OAuthClient {
	out := map[string]OAuthClient{}
	data, err := os.ReadFile(oauthClientsPath())
	if err != nil {
		return out
	}
	if config.IsSealed(data) {
		key, err := config.ResolveMasterKey(oauthClientsPath())
		if err != nil {
			return out
		}
		if data, err = config.Unseal(key, data); err != nil {
			return out
		}
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// LoadOAuthClient returns the owner's app for a provider: the environment first,
// then the sealed file. Empty fields mean not set up.
func LoadOAuthClient(provider string) OAuthClient {
	c := loadOAuthClients()[provider]
	if k, ok := envKeys[provider]; ok {
		if v := strings.TrimSpace(os.Getenv(k[0])); v != "" {
			c.ClientID = v
		}
		if v := strings.TrimSpace(os.Getenv(k[1])); v != "" {
			c.ClientSecret = v
		}
		if k[2] != "" {
			if v := strings.TrimSpace(os.Getenv(k[2])); v != "" {
				c.Tenant = v
			}
		}
	}
	return c
}

// OAuthClientConfigured reports whether sign-in for the provider can start.
func OAuthClientConfigured(provider string) bool {
	c := LoadOAuthClient(provider)
	return c.ClientID != "" && c.ClientSecret != ""
}

// SaveOAuthClient stores the owner's app, sealed. It never echoes the secret.
func SaveOAuthClient(provider string, c OAuthClient) error {
	if _, ok := envKeys[provider]; !ok {
		return ErrUnknownProvider
	}
	c.ClientID, c.ClientSecret, c.Tenant = strings.TrimSpace(c.ClientID), strings.TrimSpace(c.ClientSecret), strings.TrimSpace(c.Tenant)
	if c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("both the client ID and the client secret are needed")
	}
	all := loadOAuthClients()
	all[provider] = c
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(CalendarTokenDir(), 0o700); err != nil {
		return err
	}
	key, err := config.MasterKeyFor(oauthClientsPath())
	if err != nil {
		return err
	}
	sealed, err := config.Seal(key, data)
	if err != nil {
		return err
	}
	tmp := oauthClientsPath() + ".tmp"
	if err := os.WriteFile(tmp, sealed, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, oauthClientsPath())
}

// RemoveOAuthClient forgets the owner's app for a provider.
func RemoveOAuthClient(provider string) error {
	all := loadOAuthClients()
	if _, ok := all[provider]; !ok {
		return nil
	}
	delete(all, provider)
	data, _ := json.Marshal(all)
	key, err := config.MasterKeyFor(oauthClientsPath())
	if err != nil {
		return err
	}
	sealed, err := config.Seal(key, data)
	if err != nil {
		return err
	}
	return os.WriteFile(oauthClientsPath(), sealed, 0o600)
}

func redirectOr(env, provider string) string {
	if v := strings.TrimSpace(os.Getenv(env)); v != "" {
		return v
	}
	return DefaultRedirect(provider)
}

// ClientConfigs for each service, from the owner's app.
func CalendarClientConfig() (CalendarOAuthConfig, bool) {
	c := LoadOAuthClient(ProviderGoogle)
	cfg := CalendarOAuthConfig{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: redirectOr("GHOST_CALENDAR_REDIRECT_URL", ProviderGoogle)}
	return cfg, cfg.ClientID != "" && cfg.ClientSecret != ""
}
