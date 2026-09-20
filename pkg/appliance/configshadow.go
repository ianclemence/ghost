package appliance

import (
	"os"
	"path/filepath"
)

// ConfigShadow describes a config-resolution mismatch that would confuse an
// operator: the CLI resolved to one config file, while an installed Ghost
// exists and appears to hold the credential the CLI is missing.
type ConfigShadow struct {
	// ResolvedPath is the config file the CLI actually loaded.
	ResolvedPath string
	// InstalledPath is the installed Ghost's config file (e.g.
	// /var/ghost/config/config.json).
	InstalledPath string
	// InstalledHasSecret reports whether the installed config carries a
	// non-empty key for the provider in question.
	InstalledHasSecret bool
}

// DetectConfigShadow reports whether a CLI run resolved to a config that is
// NOT the installed Ghost's, while an installed Ghost config exists. It is the
// honesty check behind "no API key configured": when true, the real problem is
// usually that the operator is pointed at a checkout config, not that no key
// was ever saved.
//
// providerKey returns the provider's API key from a config file (may be "").
// Returns ok=false when there is no shadowing to report (no installed Ghost,
// or the resolved path already IS the installed one).
func DetectConfigShadow(resolvedPath string, installed bool, providerKey func(path string) string) (ConfigShadow, bool) {
	if !installed || resolvedPath == "" {
		return ConfigShadow{}, false
	}
	installedPath := filepath.Join(DefaultConfigDir, "config.json")

	// Resolved path and installed path may differ only by symlink/cleaning;
	// compare cleaned absolute paths so we never warn about the same file.
	if absClean(resolvedPath) == absClean(installedPath) {
		return ConfigShadow{}, false
	}

	sh := ConfigShadow{
		ResolvedPath:       resolvedPath,
		InstalledPath:      installedPath,
		InstalledHasSecret: false,
	}
	if providerKey != nil {
		if k := providerKey(installedPath); k != "" {
			sh.InstalledHasSecret = true
		}
	}
	return sh, true
}

func absClean(p string) string {
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// FileExists is a small helper so callers can probe a path without importing
// os directly.
func FileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
