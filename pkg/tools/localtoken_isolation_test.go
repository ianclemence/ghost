package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The local token is what stops a model-run command from approving its own
// requests, so the one thing that matters is that the command cannot read it.
func TestSandboxCannotReadLocalToken(t *testing.T) {
	if !IsolationActive() {
		t.Skip("command isolation (bubblewrap) is not available here")
	}
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	cfg := filepath.Join(root, "config")
	for _, d := range []string{ws, cfg} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const secret = "sandbox-must-never-print-this-token-value"
	if err := os.WriteFile(filepath.Join(cfg, ".local-token"), []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewExecTool(ws, true)
	for _, cmd := range []string{
		"cat ../config/.local-token",
		"cat " + filepath.Join(cfg, ".local-token"),
		"ls -a ../config 2>&1; cat ../config/* 2>&1",
	} {
		r := e.Execute(context.Background(), map[string]interface{}{"command": cmd})
		if strings.Contains(r.ForLLM, secret) {
			t.Fatalf("command %q read the local token: %q", cmd, r.ForLLM)
		}
	}
}
