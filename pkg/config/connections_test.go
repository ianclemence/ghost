package config

import (
	"testing"
)

func connConfig() *Config {
	c := DefaultConfig()
	c.Agents.Connections = []ModelConnection{
		{Name: "local", Provider: "local", Model: "qwen3", BaseURL: "http://localhost:11434/v1", AuthType: "none"},
		{Name: "openrouter", Provider: "openrouter", Model: "openai/gpt-4.1", BaseURL: "https://openrouter.ai/api/v1", AuthEnv: "OPENROUTER_API_KEY_TEST"},
		{Name: "broken", Provider: "", Model: ""},
		{Name: "remote-anon", Provider: "x", Model: "y", BaseURL: "http://example.com/v1", AuthType: "none"},
	}
	return c
}

func TestResolveConnection(t *testing.T) {
	c := connConfig()
	t.Setenv("OPENROUTER_API_KEY_TEST", "")
	rc, err := c.ResolveConnection("local")
	if err != nil {
		t.Fatal(err)
	}
	if rc.AuthType != "none" || rc.APIKey != "" {
		t.Fatalf("%+v", rc)
	}
	if _, err := c.ResolveConnection("openrouter"); err == nil {
		t.Fatal("missing credential must fail closed")
	}
	t.Setenv("OPENROUTER_API_KEY_TEST", "sk-test")
	rc, err = c.ResolveConnection("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if rc.APIKey != "sk-test" || rc.AuthType != "bearer" {
		t.Fatalf("%+v", rc)
	}
	if _, err := c.ResolveConnection("nope"); err == nil {
		t.Fatal("unknown connection must fail")
	}
	if _, err := c.ResolveConnection("broken"); err == nil {
		t.Fatal("invalid profile must fail startup, not fall back")
	}
	if _, err := c.ResolveConnection("remote-anon"); err == nil {
		t.Fatal("anonymous non-loopback non-HTTPS must fail")
	}
}

func TestFingerprintAndResume(t *testing.T) {
	c := connConfig()
	t.Setenv("OPENROUTER_API_KEY_TEST", "sk-test")
	a, err := c.ResolveConnection("local")
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.ResolveConnection("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Fatal("distinct destinations must fingerprint distinctly")
	}
	if len(a.Fingerprint()) != 16 {
		t.Fatalf("fingerprint %q", a.Fingerprint())
	}
	// Fingerprint carries no key material.
	rc := *b
	rc.APIKey = "different-but-same-shape"
	if rc.Fingerprint() != b.Fingerprint() {
		t.Fatal("fingerprint must not embed key material")
	}
	if err := CheckResume("", b.Fingerprint()); err != nil {
		t.Fatal("unpinned sessions predate fingerprinting")
	}
	if err := CheckResume(a.Fingerprint(), a.Fingerprint()); err != nil {
		t.Fatal(err)
	}
	if err := CheckResume(a.Fingerprint(), b.Fingerprint()); err == nil {
		t.Fatal("cross-destination resume must fail")
	}
	if err := CheckResume(a.Fingerprint(), ""); err == nil {
		t.Fatal("missing current connection must fail")
	}
}
