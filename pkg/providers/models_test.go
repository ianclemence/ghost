package providers

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// A provider with a configured key but no preset must still appear as a
// selectable option (the "deepseek is configured but invisible" case);
// unkeyed providers stay hidden, and preset-covered models don't repeat.
func TestAvailableModelOptions(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.ModelList = []config.ModelPreset{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:0.6b"},
		{Name: "stale", Provider: "openai", Model: "gpt-4o"},
	}
	cfg.Providers.DeepSeek.APIKey = "test-key"

	opts := AvailableModelOptions(cfg)
	byName := map[string]ModelOption{}
	for _, o := range opts {
		byName[o.Name] = o
	}
	if _, ok := byName["local"]; !ok {
		t.Errorf("presets must be listed, got %+v", opts)
	}
	stale, ok := byName["stale"]
	if !ok || stale.Available {
		t.Errorf("keyless preset must list as unavailable, got %+v", stale)
	}
	ds, ok := byName["deepseek"]
	if !ok {
		t.Fatalf("keyed provider must appear without a preset, got %+v", opts)
	}
	if ds.Kind != "provider" || ds.Target != "deepseek:deepseek-flash" || !ds.Available {
		t.Errorf("provider option wrong: %+v", ds)
	}
	for _, o := range opts {
		if o.Kind == "provider" && o.Provider == "ollama" {
			t.Errorf("provider entries must not duplicate covered models: %+v", o)
		}
	}
}
