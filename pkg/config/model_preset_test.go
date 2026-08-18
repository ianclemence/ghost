package config

import "testing"

func TestFindModelPreset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Agents.ModelList = []ModelPreset{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:4b"},
		{Name: "claude", Provider: "anthropic", Model: "claude-sonnet-4"},
	}

	if p := cfg.FindModelPreset("local"); p == nil {
		t.Fatal("expected 'local' preset to be found")
	} else if p.Provider != "ollama" || p.Model != "ollama/qwen3:4b" {
		t.Errorf("local preset mismatch: %+v", p)
	}

	if p := cfg.FindModelPreset("claude"); p == nil {
		t.Fatal("expected 'claude' preset to be found")
	} else if p.Provider != "anthropic" || p.Model != "claude-sonnet-4" {
		t.Errorf("claude preset mismatch: %+v", p)
	}

	if p := cfg.FindModelPreset("missing"); p != nil {
		t.Errorf("expected nil for missing preset, got %+v", p)
	}
}

func TestSetActiveModel(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SetActiveModel("openai", "gpt-4o")
	if cfg.Agents.Defaults.Provider != "openai" {
		t.Errorf("provider = %q, want openai", cfg.Agents.Defaults.Provider)
	}
	if cfg.Agents.Defaults.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", cfg.Agents.Defaults.Model)
	}
}
