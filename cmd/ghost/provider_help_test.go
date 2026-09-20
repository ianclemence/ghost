package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// The setup-help mapping must match what pkg/config actually reads, or the
// CLI hint points at an env var that does nothing.
func TestProviderEnvVar(t *testing.T) {
	cases := map[string]string{
		"deepseek":   "DEEPSEEK_API_KEY",
		"anthropic":  "ANTHROPIC_API_KEY",
		"openai":     "OPENAI_API_KEY",
		"openrouter": "OPENROUTER_API_KEY",
		"groq":       "GROQ_API_KEY",
		"gemini":     "GEMINI_API_KEY",
		"zhipu":      "ZHIPU_API_KEY",
		"moonshot":   "KIMI_API_KEY",
		"kimi":       "KIMI_API_KEY",
		"ollama":     "",
	}
	for provider, want := range cases {
		if got := providerEnvVar(provider); got != want {
			t.Errorf("providerEnvVar(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestProviderKeyFor(t *testing.T) {
	c := config.DefaultConfig()
	c.Providers.DeepSeek.APIKey = "sk-ds"
	c.Providers.OpenAI.APIKey = "sk-oa"
	if got := providerKeyFor(c, "deepseek"); got != "sk-ds" {
		t.Errorf("deepseek key = %q", got)
	}
	if got := providerKeyFor(c, "openai"); got != "sk-oa" {
		t.Errorf("openai key = %q", got)
	}
	if got := providerKeyFor(c, "groq"); got != "" {
		t.Errorf("unset key must be empty, got %q", got)
	}
	if got := providerKeyFor(nil, "deepseek"); got != "" {
		t.Errorf("nil config must be empty, got %q", got)
	}
}

// getConfigPath must honor GHOST_CONFIG_DIR first — the single switch that
// keeps the console, the daemon, and the CLI pointed at the same config.
func TestGetConfigPathPrefersEnv(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", "/srv/somewhere/config")
	if got := getConfigPath(); got != "/srv/somewhere/config/config.json" {
		t.Fatalf("getConfigPath() = %q, want env-derived path", got)
	}
}
