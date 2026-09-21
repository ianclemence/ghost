package providers

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// AllModelOptions is the full catalog (unusable entries included, marked
// unavailable); AvailableModelOptions is the selection set (usable only).
// A provider with a configured key but no preset must appear as a selectable
// option (the "deepseek is configured but invisible" case); unkeyed providers
// stay out of the selection set, and preset-covered models don't repeat.
func TestAvailableModelOptions(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.ModelList = []config.ModelPreset{
		{Name: "local", Provider: "ollama", Model: "ollama/qwen3:0.6b"},
		{Name: "stale", Provider: "openai", Model: "gpt-4o"},
	}
	cfg.Providers.DeepSeek.APIKey = "test-key"

	// Full catalog: the keyless preset is present but marked unavailable.
	all := AllModelOptions(cfg)
	allByName := map[string]ModelOption{}
	for _, o := range all {
		allByName[o.Name] = o
	}
	if _, ok := allByName["local"]; !ok {
		t.Errorf("presets must be listed in the full catalog, got %+v", all)
	}
	if stale, ok := allByName["stale"]; !ok || stale.Available {
		t.Errorf("keyless preset must appear unavailable in the full catalog, got %+v", stale)
	}

	// Selection set: only usable entries.
	opts := AvailableModelOptions(cfg)
	byName := map[string]ModelOption{}
	for _, o := range opts {
		byName[o.Name] = o
	}
	if _, ok := byName["local"]; !ok {
		t.Errorf("usable presets must be listed, got %+v", opts)
	}
	if _, ok := byName["stale"]; ok {
		t.Errorf("keyless preset must not be selectable, got %+v", byName["stale"])
	}
	ds, ok := byName["deepseek"]
	if !ok {
		t.Fatalf("keyed provider must appear without a preset, got %+v", opts)
	}
	if ds.Kind != "provider" || ds.Target != "deepseek:deepseek-flash" || !ds.Available {
		t.Errorf("provider option wrong: %+v", ds)
	}
	for _, o := range opts {
		if o.Available == false {
			t.Errorf("selection set leaked an unavailable entry: %+v", o)
		}
		if o.Kind == "provider" && o.Provider == "ollama" {
			t.Errorf("provider entries must not duplicate covered models: %+v", o)
		}
	}
}

// FindModelOption resolves exact references against the full catalog, so a
// model whose provider is not yet configured can still be named explicitly.
func TestFindModelOptionResolvesUnconfigured(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.ModelList = []config.ModelPreset{{Name: "stale", Provider: "openai", Model: "gpt-4o"}}

	if o, ok := FindModelOption(cfg, "stale"); !ok || o.Target != "stale" {
		t.Fatalf("preset name should resolve, got %+v ok=%v", o, ok)
	}
	if o, ok := FindModelOption(cfg, "openai:gpt-4o"); !ok || o.Provider != "openai" {
		t.Fatalf("unconfigured provider target should resolve, got %+v ok=%v", o, ok)
	}
	if _, ok := FindModelOption(cfg, "nonexistent"); ok {
		t.Fatal("unknown target must not resolve")
	}
}
