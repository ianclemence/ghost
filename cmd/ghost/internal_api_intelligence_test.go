package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

func TestMaskDeviceKey(t *testing.T) {
	if got := maskDeviceKey(""); got != "" {
		t.Fatalf("empty key must stay empty, got %q", got)
	}
	if got := maskDeviceKey("short"); got != "••••••••" {
		t.Fatalf("short key must be fully masked, got %q", got)
	}
	if got := maskDeviceKey("sk-ant-1234567890"); got != "••••••••7890" {
		t.Fatalf("long key must keep last 4 chars, got %q", got)
	}
}

func TestIntelligenceProviderConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.OpenAI.APIKey = "x"
	if pc := intelligenceProviderConfig(cfg, "openai"); pc == nil || pc.APIKey != "x" {
		t.Fatalf("expected openai config, got %+v", pc)
	}
	if pc := intelligenceProviderConfig(cfg, " OpenAI "); pc == nil {
		t.Fatalf("provider names must match case-insensitively with whitespace")
	}
	if pc := intelligenceProviderConfig(cfg, "qwen"); pc == nil {
		t.Fatalf("expected qwen config to resolve")
	}
	if pc := intelligenceProviderConfig(cfg, "nope"); pc != nil {
		t.Fatalf("unknown provider must return nil, got %+v", pc)
	}
	if pc := intelligenceProviderConfig(nil, "openai"); pc != nil {
		t.Fatalf("nil config must return nil")
	}
}

func TestKnownProviderModels(t *testing.T) {
	if len(knownProviderModels["openai"]) == 0 || len(knownProviderModels["anthropic"]) == 0 {
		t.Fatalf("cloud providers must ship recommended models")
	}
	if len(knownProviderModels["ollama"]) != 0 {
		t.Fatalf("ollama models come from the daemon, not the static list")
	}
}

// The phone's providers list says where each model list came from: a provider
// that answered is "live", one that could not be reached keeps the built-in
// list and is labelled "catalog" with the reason, and an unreachable provider
// that is not even configured says nothing (it is not a problem yet).
func TestProviderListPayloadSaysWhereModelsCameFrom(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.OpenAI.APIKey = "sk-test"
	cfg.Providers.Anthropic.APIKey = "sk-ant"
	discovered := map[string]providers.ProviderModels{
		"openai":    {Provider: "openai", Models: []string{"gpt-live-1", "gpt-live-2"}, Source: "live"},
		"anthropic": {Provider: "anthropic", Source: "catalog", Error: "401 from the provider"},
		"groq":      {Provider: "groq", Source: "catalog", Error: "no key"},
	}
	out := providerListPayload(cfg, discovered, []string{"qwen3:4b"})

	openai := out["openai"].(map[string]interface{})
	if openai["source"] != "live" || len(openai["models"].([]string)) != 2 || openai["configured"] != true {
		t.Fatalf("a live answer must be shown as live: %+v", openai)
	}
	anthropic := out["anthropic"].(map[string]interface{})
	if anthropic["source"] != "catalog" || anthropic["error"] != "401 from the provider" {
		t.Fatalf("an unreachable configured provider must say why: %+v", anthropic)
	}
	if len(anthropic["models"].([]string)) == 0 {
		t.Fatal("it must keep the built-in list so the owner can still choose")
	}
	if _, has := out["groq"].(map[string]interface{})["error"]; has {
		t.Fatal("an unconfigured provider is not an error")
	}
	if ollama := out["ollama"].(map[string]interface{}); ollama["source"] != "live" || ollama["models"].([]string)[0] != "qwen3:4b" {
		t.Fatalf("ollama's list is what is installed here: %+v", ollama)
	}
}
