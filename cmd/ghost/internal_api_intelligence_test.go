package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
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
