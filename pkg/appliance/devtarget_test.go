package appliance

import (
	"path/filepath"
	"testing"
)

func TestResolveDevTargetDefaults(t *testing.T) {
	noenv := func(string) string { return "" }
	got := ResolveDevTarget(noenv, "/home/dev", 0)
	if got.GhostDir != "/home/dev/.ghost-dev" {
		t.Errorf("ghost dir = %q", got.GhostDir)
	}
	if got.ConfigDir != "/home/dev/.ghost-dev/config" {
		t.Errorf("config dir = %q", got.ConfigDir)
	}
	if got.Workspace != "/home/dev/.ghost-dev/workspace" {
		t.Errorf("workspace = %q", got.Workspace)
	}
	if got.Port != DefaultDevPort {
		t.Errorf("port = %d, want %d", got.Port, DefaultDevPort)
	}
}

func TestResolveDevTargetEnvOverrides(t *testing.T) {
	env := func(k string) string {
		switch k {
		case "GHOST_DEV_DIR":
			return "/srv/dev-ghost"
		case "GHOST_DEV_PORT":
			return "9999"
		}
		return ""
	}
	got := ResolveDevTarget(env, "/home/dev", 0)
	if got.GhostDir != "/srv/dev-ghost" {
		t.Errorf("ghost dir = %q", got.GhostDir)
	}
	if got.Port != 9999 {
		t.Errorf("port = %d, want 9999", got.Port)
	}
}

func TestResolveDevTargetExplicitPortWins(t *testing.T) {
	env := func(k string) string {
		if k == "GHOST_DEV_PORT" {
			return "9999"
		}
		return ""
	}
	got := ResolveDevTarget(env, "/home/dev", 1234)
	if got.Port != 1234 {
		t.Errorf("explicit port must win, got %d", got.Port)
	}
}

// A dev instance must never adopt the production port from the production
// .env (GHOST_API_PORT=8766); it uses its own dev port instead.
func TestResolveDevTargetIgnoresProductionPort(t *testing.T) {
	env := func(k string) string {
		if k == "GHOST_API_PORT" {
			return "8766"
		}
		return ""
	}
	got := ResolveDevTarget(env, "/home/dev", 0)
	if got.Port != DefaultDevPort {
		t.Errorf("dev must ignore GHOST_API_PORT; port = %d, want %d", got.Port, DefaultDevPort)
	}
}

func TestResolveDevTargetRelativeEnvIsAnchoredToHome(t *testing.T) {
	env := func(k string) string {
		if k == "GHOST_DEV_DIR" {
			return "dev-instance"
		}
		return ""
	}
	got := ResolveDevTarget(env, "/home/dev", 0)
	if got.GhostDir != "/home/dev/dev-instance" {
		t.Errorf("relative dev dir must anchor to home, got %q", got.GhostDir)
	}
}

// The dev target must never resolve onto the production paths — that is the
// whole point of the separation.
func TestDevTargetConflictsWithProduction(t *testing.T) {
	safe := DevTarget{GhostDir: "/home/dev/.ghost-dev", ConfigDir: "/home/dev/.ghost-dev/config", Workspace: "/home/dev/.ghost-dev/workspace"}
	if safe.ConflictsWithProduction() {
		t.Errorf("a normal dev target must not conflict with production")
	}
	bad := DevTarget{GhostDir: DefaultGhostDir}
	if !bad.ConflictsWithProduction() {
		t.Errorf("a dev target pointing at %s MUST be rejected", DefaultGhostDir)
	}
	badCfg := DevTarget{GhostDir: "/tmp/x", ConfigDir: DefaultConfigDir}
	if !badCfg.ConflictsWithProduction() {
		t.Errorf("overlapping config dir with production MUST be rejected")
	}
	badWs := DevTarget{GhostDir: "/tmp/x", Workspace: filepath.Clean(DefaultWorkspaceDir)}
	if !badWs.ConflictsWithProduction() {
		t.Errorf("overlapping workspace with production MUST be rejected")
	}
}
