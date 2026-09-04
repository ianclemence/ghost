package skills

import (
	"os"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// Integration credential model (general, not per-skill hacks).
//
// Integration -> Provider -> Credentials -> Readiness -> Capability.
// States: READY / NEEDS_CONFIGURATION / NEEDS_AUTHORIZATION / EXPIRED /
// REVOKED / UNAVAILABLE. Credentials live in .secrets.json (0600, excluded
// from backups); .env remains a deprecated developer fallback, never the
// product path.

// AviationKey returns the AviationStack key from secrets-first, env fallback.
// Empty means NEEDS_CONFIGURATION (never fake live data).
// Product path: .secrets.json ProviderAPIKeys["aviationstack"] (0600,
// excluded from backups). Developer fallback: AVIATION_API_KEY env.
func AviationKey(cfg *config.Config) string {
	if v := AviationKeyFromSecrets(nil); v != "" {
		// Secrets-file hit (or env fallback inside).
		// When cfg is available its overlay is already reflected in env
		// resolution below; keep single source here.
		_ = cfg
		return v
	}
	return ""
}

// AviationKeyFromSecrets reads the product-path key from loaded secrets.
func AviationKeyFromSecrets(s *config.Secrets) string {
	if s != nil && s.ProviderAPIKeys != nil {
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviationstack"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviation"]); v != "" {
			return v
		}
	}
	// Fall back to secrets file on disk (standard locations), then env.
	if v := aviationKeyFromDisk(); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AVIATION_API_KEY")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AVIATIONSTACK_API_KEY")); v != "" {
		return v
	}
	return ""
}

// aviationKeyFromDisk loads .secrets.json from GHOST_CONFIG_DIR or the
// default config dir. Never logs or returns file contents on error.
func aviationKeyFromDisk() string {
	dirs := []string{}
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		dirs = append(dirs, d)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, home+"/.config/ghost", home+"/.ghost")
	}
	dirs = append(dirs, "/var/lib/ghost/config", "./config")
	for _, d := range dirs {
		s, err := config.LoadSecrets(d + "/.secrets.json")
		if err != nil || s == nil || s.ProviderAPIKeys == nil {
			continue
		}
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviationstack"]); v != "" {
			return v
		}
		if v := strings.TrimSpace(s.ProviderAPIKeys["aviation"]); v != "" {
			return v
		}
	}
	return ""
}

// HassConfigured reports whether Home Assistant credentials exist
// (secrets-first via ProviderAPIKeys hass_url/hass_token, env fallback).
func HassConfigured() bool {
	if v := hassSecret("hass_url"); v != "" {
		if t := hassSecret("hass_token"); t != "" {
			return true
		}
	}
	if strings.TrimSpace(os.Getenv("HASS_URL")) != "" && strings.TrimSpace(os.Getenv("HASS_TOKEN")) != "" {
		return true
	}
	return false
}

func hassSecret(key string) string {
	dirs := []string{}
	if d := strings.TrimSpace(os.Getenv("GHOST_CONFIG_DIR")); d != "" {
		dirs = append(dirs, d)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, home+"/.config/ghost", home+"/.ghost")
	}
	dirs = append(dirs, "/var/lib/ghost/config", "./config")
	for _, d := range dirs {
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
