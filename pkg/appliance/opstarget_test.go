package appliance

import (
	"testing"
)

func envOf(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

// Non-root runs never default: dev checkouts are unaffected.
func TestOpsTargetNonRoot(t *testing.T) {
	if _, _, ok := ResolveOpsTarget(envOf(), 1000, true); ok {
		t.Fatal("non-root must keep CLI resolution")
	}
}

// No personal AI installed: root checkout runs are unaffected.
func TestOpsTargetNoAppliance(t *testing.T) {
	if _, _, ok := ResolveOpsTarget(envOf(), 0, false); ok {
		t.Fatal("missing personal AI must keep CLI resolution")
	}
}

// Root + personal AI: both defaults apply.
func TestOpsTargetDefaults(t *testing.T) {
	configDir, workspace, ok := ResolveOpsTarget(envOf(), 0, true)
	if !ok {
		t.Fatal("root on personal AI must default")
	}
	if configDir != DefaultConfigDir {
		t.Fatalf("configDir = %q, want %q", configDir, DefaultConfigDir)
	}
	if workspace != DefaultWorkspaceDir {
		t.Fatalf("workspace = %q, want %q", workspace, DefaultWorkspaceDir)
	}
}

// Explicit env always wins, per variable.
func TestOpsTargetEnvWins(t *testing.T) {
	configDir, workspace, ok := ResolveOpsTarget(envOf("GHOST_CONFIG_DIR", "/custom/cfg"), 0, true)
	if !ok || configDir != "" || workspace != DefaultWorkspaceDir {
		t.Fatalf("config override must win independently: %q %q %v", configDir, workspace, ok)
	}
	configDir, workspace, ok = ResolveOpsTarget(envOf("GHOST_WORKSPACE_DIR", "/w"), 0, true)
	if !ok || configDir != DefaultConfigDir || workspace != "" {
		t.Fatalf("workspace override must win independently: %q %q %v", configDir, workspace, ok)
	}
	// Legacy GHOST_WORKSPACE also suppresses the workspace default.
	_, workspace, ok = ResolveOpsTarget(envOf("GHOST_WORKSPACE", "/legacy"), 0, true)
	if !ok || workspace != "" {
		t.Fatalf("legacy workspace var must suppress default: %q %v", workspace, ok)
	}
	// Fully overridden: no opinion.
	if _, _, ok := ResolveOpsTarget(envOf("GHOST_CONFIG_DIR", "/c", "GHOST_WORKSPACE_DIR", "/w"), 0, true); ok {
		t.Fatal("fully overridden env must keep CLI resolution")
	}
}

func TestResolveInstalledConfig(t *testing.T) {
	noenv := func(string) string { return "" }
	readable := func(string) bool { return true }
	unreadable := func(string) bool { return false }

	// Env wins: no override when GHOST_CONFIG_DIR is set.
	env := func(k string) string {
		if k == "GHOST_CONFIG_DIR" {
			return "/custom"
		}
		return ""
	}
	if _, ok := ResolveInstalledConfig(env, true, readable); ok {
		t.Fatal("explicit GHOST_CONFIG_DIR must not be overridden")
	}
	// No installed Ghost: no opinion.
	if _, ok := ResolveInstalledConfig(noenv, false, readable); ok {
		t.Fatal("no installed Ghost means no override")
	}
	// Installed + readable: use it.
	if dir, ok := ResolveInstalledConfig(noenv, true, readable); !ok || dir != DefaultConfigDir {
		t.Fatalf("installed+readable should resolve to %s, got %q ok=%v", DefaultConfigDir, dir, ok)
	}
	// Installed but unreadable (non-root /var/ghost): no override.
	if _, ok := ResolveInstalledConfig(noenv, true, unreadable); ok {
		t.Fatal("unreadable installed config must not be used")
	}
}
