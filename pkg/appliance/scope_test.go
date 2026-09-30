package appliance

import (
	"os"
	"path/filepath"
	"testing"
)

// A user-local ghost binary means user scope: no root needed.
func TestDetectScopeUserBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "ghost"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "") // ensure no systemctl discovery interferes
	// Isolate from any real /usr/local/bin install on the test machine: a
	// system binary now (correctly) outranks a lone user binary.
	t.Setenv("GHOST_SYSTEM_BIN_DIR", t.TempDir())

	p := DetectScope()
	if p.Scope != ScopeUser {
		t.Fatalf("expected user scope, got %v", p.Scope)
	}
	if p.BinDir != bin {
		t.Fatalf("bin dir = %s, want %s", p.BinDir, bin)
	}
	if p.NeedsRoot() {
		t.Fatal("user scope must not need root")
	}
}

// With nothing installed, default to the unprivileged user layout.
func TestDetectScopeFreshDefaultsToUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")
	// Point the system-bin probe at an empty dir so a real /usr/local/bin
	// install on the test machine does not force system scope.
	t.Setenv("GHOST_SYSTEM_BIN_DIR", t.TempDir())

	p := DetectScope()
	if p.Scope != ScopeUser {
		t.Fatalf("fresh machine should default to user scope, got %v", p.Scope)
	}
	if p.NeedsRoot() {
		t.Fatal("fresh install must not need root")
	}
	if p.UserUnitPath() != filepath.Join(home, ".config", "systemd", "user", "ghost.service") {
		t.Fatalf("user unit path wrong: %s", p.UserUnitPath())
	}
}

func TestScopeNeedsRootAndPaths(t *testing.T) {
	p := ScopePaths{Scope: ScopeSystem, SystemBinDir: "/usr/local/bin", UserBinDir: "/home/u/.local/bin", Home: "/home/u"}
	if !p.NeedsRoot() {
		t.Fatal("system scope must need root")
	}
	if p.SystemBinaryPath() != "/usr/local/bin/ghost" {
		t.Fatalf("system bin path: %s", p.SystemBinaryPath())
	}
	u := ScopePaths{Scope: ScopeUser, UserBinDir: "/home/u/.local/bin", Home: "/home/u"}
	if u.NeedsRoot() {
		t.Fatal("user scope must not need root")
	}
	if u.UserBinaryPath() != "/home/u/.local/bin/ghost" {
		t.Fatalf("user bin path: %s", u.UserBinaryPath())
	}
	if u.Scope.String() != "user" || p.Scope.String() != "system" {
		t.Fatalf("scope strings wrong: %s %s", u.Scope, p.Scope)
	}
}

// The bug: a system service serves Ghost from /usr/local/bin, but a stray
// ~/.local/bin/ghost (created by an earlier user-scope update) made every
// later update land in the wrong place. The serving daemon must win.
func TestDecideScopeServingDaemonWins(t *testing.T) {
	cases := []struct {
		name string
		p    scopeProbes
		want InstallScope
	}{
		{"running system service beats a stray user binary", scopeProbes{sysUnit: true, sysUnitLive: true, sysBin: true, userBin: true}, ScopeSystem},
		{"running system service beats an idle user unit", scopeProbes{sysUnit: true, sysUnitLive: true, userUnit: true, userBin: true}, ScopeSystem},
		{"running user service beats an idle system unit", scopeProbes{sysUnit: true, sysBin: true, userUnit: true, userUnitLive: true}, ScopeUser},
		{"system binary and unit, user binary only: system", scopeProbes{sysUnit: true, sysBin: true, userBin: true}, ScopeSystem},
		{"system binary alone beats a stray user binary", scopeProbes{sysBin: true, userBin: true}, ScopeSystem},
		{"user binary alone: user", scopeProbes{userBin: true}, ScopeUser},
		{"explicit user unit: user", scopeProbes{userUnit: true}, ScopeUser},
		{"nothing installed: user", scopeProbes{}, ScopeUser},
	}
	for _, tc := range cases {
		if got := decideScope(tc.p); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
