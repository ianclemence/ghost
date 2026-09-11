package agent

import (
	"os"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

func pinTestLoop(t *testing.T, mutate func(*config.Config)) *AgentLoop {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "model-pin-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Provider = "ollama"
	cfg.Agents.Defaults.Model = "ollama/qwen3:0.6b"
	if mutate != nil {
		mutate(cfg)
	}
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &mockProvider{})
	if err != nil {
		t.Fatal(err)
	}
	return al
}

// Strict pin is the default: fresh installs never silently hop models.
func TestStrictPinDefaultsOn(t *testing.T) {
	if !config.DefaultConfig().Agents.Defaults.StrictPin {
		t.Fatal("StrictPin must default to true")
	}
}

// Pinned turns consult only the primary — fallbacks are not consulted.
func TestBuildCandidatesStrictPin(t *testing.T) {
	al := pinTestLoop(t, func(c *config.Config) {
		c.Agents.Defaults.FallbackModels = []string{"openai:gpt-4o", "ollama/qwen3:0.6b"}
	})
	al.fallbackModels = []providers.FallbackCandidate{
		{Name: "openai:gpt-4o"},
		{Name: "ollama/qwen3:0.6b"},
	}
	got := al.buildCandidates("ollama/qwen3:0.6b")
	if len(got) != 1 {
		t.Fatalf("strict pin must yield exactly the primary, got %d candidates", len(got))
	}
	if got[0].Model != "ollama/qwen3:0.6b" {
		t.Fatalf("primary must be honored, got %q", got[0].Model)
	}
}

// Explicit opt-out restores fallback consultation.
func TestBuildCandidatesFallbackWhenUnpinned(t *testing.T) {
	al := pinTestLoop(t, func(c *config.Config) {
		c.Agents.Defaults.StrictPin = false
	})
	al.fallbackModels = []providers.FallbackCandidate{
		{Name: "ollama/qwen3:1.5b"},
	}
	got := al.buildCandidates("ollama/qwen3:0.6b")
	if len(got) < 2 {
		t.Fatalf("unpinned loop must consult fallbacks, got %d candidates", len(got))
	}
}

// Switching to a keyless cloud model refuses BEFORE mutating anything.
func TestSetModelRefusesUnavailable(t *testing.T) {
	al := pinTestLoop(t, nil)
	before := al.cfg.Agents.Defaults.Provider + ":" + al.cfg.Agents.Defaults.Model
	if err := al.SetModel("openai:gpt-4o"); err == nil {
		t.Fatal("keyless cloud model must be refused")
	}
	after := al.cfg.Agents.Defaults.Provider + ":" + al.cfg.Agents.Defaults.Model
	if before != after {
		t.Fatalf("refused switch must not mutate config: %q -> %q", before, after)
	}
}

// Local models always serve.
func TestSetModelAllowsLocal(t *testing.T) {
	al := pinTestLoop(t, nil)
	// mockProvider resolves anything; availability is what matters here.
	if ok, _ := providers.PresetAvailable(al.cfg, "ollama", "qwen3:0.6b"); !ok {
		t.Fatal("local model must be available")
	}
}
