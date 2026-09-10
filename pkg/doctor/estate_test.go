package doctor

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func estateRunner(e []providers.ProviderInfo) *Doctor {
	d := New(nil, &testProvider{}, nil, "")
	d.Estate = e
	return d
}

func TestCheckProviderFlagsMissingKeys(t *testing.T) {
	d := estateRunner([]providers.ProviderInfo{
		{Model: "anthropic/claude-x", Provider: "anthropic", Kind: "cloud", Role: "primary", HasCredential: true},
		{Model: "openai/gpt-x", Provider: "openai", Kind: "cloud", Role: "fallback", HasCredential: false},
		{Model: "ollama/llama3", Provider: "ollama", Kind: "local", Role: "fallback", HasCredential: true},
	})
	res := d.checkProvider(context.Background())
	if res.Status != "warning" {
		t.Fatalf("expected warning, got %s: %s", res.Status, res.Message)
	}
	for _, want := range []string{"test-model", "openai/gpt-x"} {
		if !contains(res.Message, want) {
			t.Fatalf("message must mention %q: %s", want, res.Message)
		}
	}
	// Owner-facing inventory wording must NOT leak into the message.
	for _, banned := range []string{"Estate", "primary:", "fallback:"} {
		if contains(res.Message, banned) {
			t.Fatalf("message must not contain %q: %s", banned, res.Message)
		}
	}
}

func TestCheckProviderEstateOk(t *testing.T) {
	d := estateRunner([]providers.ProviderInfo{
		{Model: "ollama/llama3", Provider: "ollama", Kind: "local", Role: "primary", HasCredential: true},
	})
	res := d.checkProvider(context.Background())
	if res.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", res.Status, res.Message)
	}
	if contains(res.Message, "Estate") || contains(res.Message, "primary:") {
		t.Fatalf("message must stay simple, got: %s", res.Message)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
