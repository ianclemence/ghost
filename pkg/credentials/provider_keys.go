package credentials

import (
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// Provider-key accessors.
//
// These are the low-level typed readers of the credential storage. They live
// in the credential package — not in pkg/skills — so that no non-credential
// package owns how secrets are loaded. The Vault is the lifecycle authority;
// these functions are its storage adapter.
//
// Product path: .secrets.json ProviderAPIKeys[...] (sealed, 0600, excluded
// from backups). Env vars remain a deprecated developer fallback, never the
// product path.

// secretDirs returns the config directories searched for .secrets.json.
func secretDirs() []string {
	dirs := []string{}
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		dirs = append(dirs, d)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, home+"/.config/ghost", home+"/.ghost")
	}
	dirs = append(dirs, "/var/lib/ghost/config", "./config")
	return dirs
}

// providerKeyFromDisk reads one ProviderAPIKeys entry from the first secrets
// file that contains it.
func providerKeyFromDisk(key string) string {
	for _, d := range secretDirs() {
		s, err := config.LoadSecrets(d + "/.secrets.json")
		if err != nil || s == nil || s.ProviderAPIKeys == nil {
			continue
		}
		if v := strings.TrimSpace(s.ProviderAPIKeys[key]); v != "" {
			return v
		}
	}
	return ""
}

// AviationKey returns the AviationStack key (secrets-first, env fallback).
func AviationKey(cfg *config.Config) string {
	_ = cfg
	return AviationKeyFromSecrets(nil)
}

// AviationKeyFromSecrets reads the AviationStack key from loaded secrets,
// then disk, then env.
func AviationKeyFromSecrets(s *config.Secrets) string {
	if s != nil && s.ProviderAPIKeys != nil {
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviationstack"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviation"]); v != "" {
			return v
		}
	}
	if v := providerKeyFromDisk("aviationstack"); v != "" {
		return v
	}
	if v := providerKeyFromDisk("aviation"); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AVIATION_API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("AVIATIONSTACK_API_KEY"))
}

// FlightConfigured reports whether flight tracking can run.
func FlightConfigured() bool {
	return AviationKey(nil) != "" || AeroDataBoxKey() != ""
}

// AeroDataBoxKey returns the fallback flight credential.
func AeroDataBoxKey() string {
	if v := providerKeyFromDisk("aerodatabox"); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("AERODATABOX_API_KEY"))
}

// OpenWeatherKey returns the weather-fallback credential.
func OpenWeatherKey() string {
	if v := providerKeyFromDisk("openweather"); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("OPENWEATHER_API_KEY"))
}

// HassEndpoint returns the Home Assistant URL and token (empty when not
// connected — never fake).
func HassEndpoint() (url, token string) {
	return hassSecret("hass_url"), hassSecret("hass_token")
}

// HassConfigured reports whether Home Assistant credentials exist.
func HassConfigured() bool {
	if hassSecret("hass_url") != "" && hassSecret("hass_token") != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("HASS_URL")) != "" && strings.TrimSpace(os.Getenv("HASS_TOKEN")) != ""
}

func hassSecret(key string) string {
	if v := providerKeyFromDisk(key); v != "" {
		return v
	}
	return ""
}
