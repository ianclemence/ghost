package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
)

// The registry the agent loop builds must contain every provider-backed
// capability tool; otherwise the capability contracts allow tools the
// model can never invoke (cutover regression fails closed here).
func TestCreateToolRegistryHasProviderTools(t *testing.T) {
	cfg := config.DefaultConfig()
	registry := createToolRegistry(t.TempDir(), false, cfg, bus.NewMessageBus())
	for _, name := range []string{
		"weather_now", "flight_status", "aqi_now",
		"currency_convert", "crypto_price", "places_nearby",
	} {
		if _, ok := registry.Get(name); !ok {
			t.Fatalf("registry missing provider tool %s", name)
		}
	}
}

// The workspace restriction must actually reach the file tools at
// registration time: the shipped config default is restrict_to_workspace,
// and a registry built with restriction on must refuse reads and writes
// outside the allowed roots. This is the wiring regression — the config
// flag used to be passed in and then ignored.
func TestCreateToolRegistryFileToolsRestrictToWorkspace(t *testing.T) {
	cfg := config.DefaultConfig()
	if !cfg.Agents.Defaults.RestrictToWorkspace {
		t.Fatal("the shipped config default must restrict file tools to the workspace")
	}

	ws := t.TempDir()
	registry := createToolRegistry(ws, true, cfg, bus.NewMessageBus())
	ctx := context.Background()

	// A file outside the workspace, its project root, and media temp.
	escape := filepath.Join(os.TempDir(), "ghost-registry-escape.txt")
	defer os.Remove(escape)

	w, ok := registry.Get("write_file")
	if !ok {
		t.Fatal("registry missing write_file")
	}
	res := w.Execute(ctx, map[string]interface{}{"path": escape, "content": "escaped"})
	if !res.IsError {
		t.Fatalf("write_file escaped the workspace (restriction not wired): %s", res.ForLLM)
	}

	r, ok := registry.Get("read_file")
	if !ok {
		t.Fatal("registry missing read_file")
	}
	res = r.Execute(ctx, map[string]interface{}{"path": escape})
	if !res.IsError {
		t.Fatalf("read_file escaped the workspace (restriction not wired): %s", res.ForLLM)
	}

	// In-workspace writes keep working.
	res = w.Execute(ctx, map[string]interface{}{
		"path":    filepath.Join(ws, "note.txt"),
		"content": "ok",
	})
	if res.IsError {
		t.Fatalf("in-workspace write must still work: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForUser, "written") && !strings.Contains(res.ForLLM, "written") {
		t.Fatalf("expected a write confirmation, got %q", res.ForLLM)
	}
}
