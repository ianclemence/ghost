package providers

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// Resolving a provider for a fallback or light model must not rewrite the
// live config's active model: the runtime reads (and persists) that config,
// so a pointer-copy mutation silently moved the owner's primary model to
// whichever candidate was resolved last.
func TestCreateProviderForModelDoesNotMutateConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek-flash"
	cfg.Providers.DeepSeek.APIKey = "sk-test"
	cfg.Providers.Anthropic.APIKey = "sk-ant-test"

	if _, err := CreateProviderForModel(cfg, "anthropic:claude-opus-5-5"); err != nil {
		t.Fatalf("resolve anthropic: %v", err)
	}
	if got := cfg.Agents.Defaults.Provider + ":" + cfg.Agents.Defaults.Model; got != "deepseek:deepseek-flash" {
		t.Fatalf("live config mutated to %q", got)
	}
}

// An Anthropic API key must reach the native Messages API provider (prompt
// caching, native tool_use, refusal handling), not the OpenAI-compat shim.
func TestAnthropicAPIKeyUsesNativeProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Providers.Anthropic.APIKey = "sk-ant-test"
	for _, spec := range []string{"anthropic:claude-opus-5-5", "claude-sonnet-5-5"} {
		p, err := CreateProviderForModel(cfg, spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if _, ok := p.(*AnthropicProvider); !ok {
			t.Fatalf("%s: got %T, want *AnthropicProvider", spec, p)
		}
		if _, ok := p.(StreamingProvider); !ok {
			t.Fatalf("%s: native provider must stream", spec)
		}
	}
	if _, ok := interface{}(&ClaudeProvider{}).(StreamingProvider); !ok {
		t.Fatal("OAuth ClaudeProvider must implement StreamingProvider")
	}
}

func TestNormalizeAnthropicBase(t *testing.T) {
	for in, want := range map[string]string{
		"":                             "",
		"https://api.anthropic.com/v1": "https://api.anthropic.com/",
		"https://api.anthropic.com/":   "https://api.anthropic.com/",
		"https://proxy.local/v1/":      "https://proxy.local/",
	} {
		if got := normalizeAnthropicBase(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
