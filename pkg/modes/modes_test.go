package modes

import (
	"strings"
	"testing"
)

func TestDerivation(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("GHOST_INTELLIGENCE_MODE", "")
	if got := Resolve(ws, false); got != Local {
		t.Fatalf("no cloud → local, got %s", got)
	}
	if got := Resolve(ws, true); got != Hybrid {
		t.Fatalf("cloud keys → hybrid, got %s", got)
	}
	if err := Set(ws, Cloud); err != nil {
		t.Fatal(err)
	}
	if got := Resolve(ws, false); got != Cloud {
		t.Fatal("explicit choice wins over derivation")
	}
	if err := Set(ws, "turbo"); err == nil {
		t.Fatal("invalid mode must fail")
	}
}

func TestEnvOverride(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("GHOST_INTELLIGENCE_MODE", "local")
	if got := Resolve(ws, true); got != Local {
		t.Fatal("env must win over derivation")
	}
}

func TestCloudDetection(t *testing.T) {
	for _, local := range []string{"ollama", "Ollama:llama3", "vllm", "local-engine"} {
		if IsCloudProvider(local) {
			t.Fatalf("%s must be local", local)
		}
	}
	for _, cloud := range []string{"openai", "anthropic", "groq", "kimi"} {
		if !IsCloudProvider(cloud) {
			t.Fatalf("%s must be cloud", cloud)
		}
	}
}

func TestDescribeNoJargon(t *testing.T) {
	for _, m := range []Mode{Local, Hybrid, Cloud} {
		d := Describe(m)
		for _, banned := range []string{"ollama", "temperature", "top-p", "quantization", "embedding"} {
			if strings.Contains(strings.ToLower(d), banned) {
				t.Fatalf("mode %s leaked %q", m, banned)
			}
		}
	}
}
