package commands

import (
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// After a factory reset the default AI provider must return to a LOCAL model
// so the appliance doesn't boot pointed at a cloud provider without keys
// (which is what produced the spurious "missing credentials for deepseek"
// state the owner saw).
func TestResetModelDefaultRestoresLocal(t *testing.T) {
	dir := t.TempDir()
	ws := dir // ws/config/config.json will be our candidate
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek/deepseek-v4-flash"
	cfg.Agents.Defaults.FallbackModels = []string{"deepseek/deepseek-v4-flash"}
	cfg.Agents.ModelList = []config.ModelPreset{{Name: "target", Provider: "ollama", Model: "qwen3:0.6b"}}
	if err := config.SaveConfig(filepath.Join(ws, "config", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}

	msg, err := resetModelDefault(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.LoadConfig(filepath.Join(ws, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Agents.Defaults.Provider != "ollama" {
		t.Fatalf("expected local default provider, got %q", got.Agents.Defaults.Provider)
	}
	if got.Agents.Defaults.Model != "ollama/qwen3:0.6b" {
		t.Fatalf("expected local default model, got %q", got.Agents.Defaults.Model)
	}
	if len(got.Agents.Defaults.FallbackModels) != 0 {
		t.Fatalf("fallback cloud models must be cleared after reset: %v", got.Agents.Defaults.FallbackModels)
	}
	if msg == "" {
		t.Fatal("expected a result message")
	}
}

// When no local preset exists the default is left untouched (never invented).
func TestResetModelDefaultLeavesUnknownProviderAlone(t *testing.T) {
	dir := t.TempDir()
	ws := dir
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek/deepseek-v4-flash"
	cfg.Agents.ModelList = nil
	if err := config.SaveConfig(filepath.Join(ws, "config", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	msg, err := resetModelDefault(ws)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := config.LoadConfig(filepath.Join(ws, "config", "config.json"))
	if got.Agents.Defaults.Provider != "deepseek" {
		t.Fatalf("provider must be left unchanged without a local preset, got %q", got.Agents.Defaults.Provider)
	}
	if msg == "" {
		t.Fatal("expected a result message")
	}
}

// A preset whose model already includes the provider prefix must not be
// double-prefixed (regression: "ollama/ollama/qwen3:0.6b").
func TestResetModelDefaultNoDoublePrefix(t *testing.T) {
	dir := t.TempDir()
	ws := dir
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek/deepseek-v4-flash"
	cfg.Agents.ModelList = []config.ModelPreset{{Name: "target", Provider: "ollama", Model: "ollama/qwen3:0.6b"}}
	if err := config.SaveConfig(filepath.Join(ws, "config", "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := resetModelDefault(ws); err != nil {
		t.Fatal(err)
	}
	got, _ := config.LoadConfig(filepath.Join(ws, "config", "config.json"))
	if got.Agents.Defaults.Model != "ollama/qwen3:0.6b" {
		t.Fatalf("model must not be double-prefixed, got %q", got.Agents.Defaults.Model)
	}
}
