package appliance

import (
	"os"
	"path/filepath"
)

// ApplianceInstalled reports whether this host carries an installed
// personal AI: the setup-complete flag plus a Ghost config. Ops
// commands use it to decide whether root invocations should default to
// Ghost paths instead of the current checkout.
func ApplianceInstalled() bool {
	if _, err := os.Stat(filepath.Join(DefaultGhostDir, SetupCompleteFlag)); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(DefaultConfigDir, "config.json")); err != nil {
		return false
	}
	return true
}

// ResolveOpsTarget returns Ghost config/workspace defaults for ops
// commands (reset, verify, status, migrate, reset-password) when running
// as root on an installed Ghost and the operator did not override
// them via env. ok=false means "no opinion — keep normal CLI
// resolution", which covers non-root runs, dev checkouts without an
// personal AI, and fully-overridden environments.
//
// Explicit GHOST_CONFIG_DIR always wins; GHOST_WORKSPACE_DIR wins unless
// the legacy GHOST_WORKSPACE is set instead (both feed the same
// workspace resolution downstream).
func ResolveOpsTarget(getenv func(string) string, euid int, installed bool) (configDir, workspace string, ok bool) {
	if euid != 0 || !installed {
		return "", "", false
	}
	if getenv("GHOST_CONFIG_DIR") == "" {
		configDir = DefaultConfigDir
	}
	if getenv("GHOST_WORKSPACE_DIR") == "" && getenv("GHOST_WORKSPACE") == "" {
		workspace = DefaultWorkspaceDir
	}
	if configDir == "" && workspace == "" {
		return "", "", false
	}
	return configDir, workspace, true
}

// ResolveInstalledConfig returns the installed Ghost's config directory when
// it exists and is readable BY THIS USER, and the operator has not set
// GHOST_CONFIG_DIR. It exists so interactive CLI commands (`agent`, `serve`,
// `status`) read the SAME config the Web Console and the daemon use, instead
// of silently falling back to a source checkout's config — the cause of
// "configured in the console but the CLI says no key" confusion.
//
// ok=false when: env already set, no installed Ghost, or the installed config
// is not readable (e.g. a non-root user and a root-only /var/ghost). In that
// last case the CLI keeps normal resolution and the provider diagnostic
// explains the situation rather than guessing.
func ResolveInstalledConfig(getenv func(string) string, installed bool, readable func(dir string) bool) (string, bool) {
	if !installed {
		return "", false
	}
	if getenv("GHOST_CONFIG_DIR") != "" {
		return "", false
	}
	if readable != nil && !readable(DefaultConfigDir) {
		return "", false
	}
	return DefaultConfigDir, true
}
