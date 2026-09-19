package main

import (
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// The Memory/files API must read the workspace the agent writes to. The bug
// this guards against: it resolved from $HOME and ignored the configured
// workspace, so the owner's Memory screen showed a different (empty) memory
// than the agent actually had.
func TestResolveApiWorkspacePrefersConfiguredWorkspace(t *testing.T) {
	t.Setenv("GHOST_WORKSPACE_DIR", "")
	t.Setenv("MEMORY_DIR", "")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = "/var/lib/ghost/workspace"

	ws, mem := resolveApiWorkspace(cfg)
	if ws != "/var/lib/ghost/workspace" {
		t.Fatalf("workspace = %q, want configured /var/lib/ghost/workspace", ws)
	}
	wantMem := filepath.Join("/var/lib/ghost/workspace", "memory")
	if mem != wantMem {
		t.Fatalf("memory = %q, want %q (derived from workspace, not $HOME)", mem, wantMem)
	}
}

func TestResolveApiWorkspaceHonorsExplicitOverride(t *testing.T) {
	t.Setenv("GHOST_WORKSPACE_DIR", "/srv/custom-ws")
	t.Setenv("MEMORY_DIR", "")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = "/var/lib/ghost/workspace"

	ws, mem := resolveApiWorkspace(cfg)
	if ws != "/srv/custom-ws" {
		t.Fatalf("explicit override must win; got %q", ws)
	}
	if mem != filepath.Join("/srv/custom-ws", "memory") {
		t.Fatalf("memory must derive from the overridden workspace; got %q", mem)
	}
}

func TestResolveApiWorkspaceMemoryOverride(t *testing.T) {
	t.Setenv("GHOST_WORKSPACE_DIR", "")
	t.Setenv("MEMORY_DIR", "/srv/journal")

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = "/var/lib/ghost/workspace"

	ws, mem := resolveApiWorkspace(cfg)
	if ws != "/var/lib/ghost/workspace" {
		t.Fatalf("workspace = %q", ws)
	}
	if mem != "/srv/journal" {
		t.Fatalf("explicit MEMORY_DIR must win; got %q", mem)
	}
}

func TestResolveApiWorkspaceNilConfigFallsBack(t *testing.T) {
	t.Setenv("GHOST_WORKSPACE_DIR", "")
	t.Setenv("MEMORY_DIR", "")
	t.Setenv("HOME", "/home/testuser")

	ws, mem := resolveApiWorkspace(nil)
	want := filepath.Join("/home/testuser", "ghost", "workspace")
	if ws != want {
		t.Fatalf("nil config should fall back to $HOME; got %q want %q", ws, want)
	}
	if mem != filepath.Join(want, "memory") {
		t.Fatalf("memory = %q", mem)
	}
}
