package appliance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallScope describes where Ghost is installed and which privilege the
// update needs. Scout and Pi both install user-locally and use per-user
// services, so the default Ghost path should need no root either; the
// system-wide appliance layout is opt-in and only then requires sudo.
type InstallScope int

const (
	// ScopeUnknown means neither layout is detected.
	ScopeUnknown InstallScope = iota
	// ScopeUser: binary under ~/.local/bin, per-user systemd units
	// (`systemctl --user`). No root required.
	ScopeUser
	// ScopeSystem: binary under /usr/local/bin, system units. Root required
	// only to replace the binary and restart the system services.
	ScopeSystem
)

func (s InstallScope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeSystem:
		return "system"
	default:
		return "unknown"
	}
}

// ScopePaths is the resolved install layout for an update.
type ScopePaths struct {
	Scope InstallScope
	// BinDir is where the ghost binary lives and where an update writes it.
	BinDir string
	// UserBinDir / SystemBinDir are candidates considered during detection.
	UserBinDir   string
	SystemBinDir string
	// Home is the invoking user's home.
	Home string
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// DetectScope inspects the machine and returns the install layout, preferring
// the user layout so updates stay unprivileged.
//
// Rules:
//   - A per-user ghost.service (`systemctl --user`) or a ghost binary in
//     ~/.local/bin => ScopeUser.
//   - Otherwise a system ghost.service or /usr/local/bin/ghost => ScopeSystem.
//   - On a fresh machine with neither, default to user scope so the first
//     install is unprivileged.
func DetectScope() ScopePaths {
	home := homeDir()
	userBin := filepath.Join(home, ".local", "bin")
	sysBin := DefaultBinDir
	// Test/advanced override so detection can be exercised without a real
	// /usr/local/bin install on the machine.
	if v := strings.TrimSpace(os.Getenv("GHOST_SYSTEM_BIN_DIR")); v != "" {
		sysBin = v
	}

	p := ScopePaths{
		UserBinDir:   userBin,
		SystemBinDir: sysBin,
		Home:         home,
	}

	userUnit := userServiceExists("ghost")
	userBinPresent := fileExists(filepath.Join(userBin, "ghost"))
	if userUnit || userBinPresent {
		p.Scope = ScopeUser
		p.BinDir = userBin
		return p
	}

	sysUnit := systemServiceExists("ghost")
	sysBinPresent := fileExists(filepath.Join(sysBin, "ghost"))
	if sysUnit || sysBinPresent {
		p.Scope = ScopeSystem
		p.BinDir = sysBin
		return p
	}

	// Nothing installed yet: default to the unprivileged layout.
	p.Scope = ScopeUser
	p.BinDir = userBin
	return p
}

// NeedsRoot reports whether an update of this layout requires root.
func (p ScopePaths) NeedsRoot() bool { return p.Scope == ScopeSystem }

// UserUnitDir is where the per-user ghost.service unit lives.
func (p ScopePaths) UserUnitDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user")
	}
	return filepath.Join(p.Home, ".config", "systemd", "user")
}

// UserUnitPath is the per-user ghost.service path.
func (p ScopePaths) UserUnitPath() string { return filepath.Join(p.UserUnitDir(), "ghost.service") }

// SystemUnitPath is the system ghost.service path.
func (p ScopePaths) SystemUnitPath() string { return "/etc/systemd/system/ghost.service" }

// UserBinaryPath / SystemBinaryPath are the candidate install targets.
func (p ScopePaths) UserBinaryPath() string   { return filepath.Join(p.UserBinDir, "ghost") }
func (p ScopePaths) SystemBinaryPath() string { return filepath.Join(p.SystemBinDir, "ghost") }

// userServiceExists reports whether a per-user systemd unit is known.
func userServiceExists(name string) bool {
	if !hasSystemctl() {
		return false
	}
	return exec.Command("systemctl", "--user", "cat", name).Run() == nil
}

// systemServiceExists reports whether a system systemd unit is known.
func systemServiceExists(name string) bool {
	if !hasSystemctl() {
		return false
	}
	return exec.Command("systemctl", "cat", name).Run() == nil
}

func hasSystemctl() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// GhostDirEnv returns an override directory for the checkout, if set.
func GhostDirEnv() string { return strings.TrimSpace(os.Getenv("GHOST_DIR")) }
