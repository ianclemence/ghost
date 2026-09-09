package providers

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

func testEstateConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Model = "anthropic/claude-test"
	cfg.Agents.Defaults.FallbackModels = []string{"ollama/llama3", "openai/gpt-test"}
	cfg.Agents.Routing.LightModel = "ollama/llama3"
	cfg.Providers.Anthropic.APIKey = "sk-test"
	return cfg
}

func TestDescribeEstate(t *testing.T) {
	estate := DescribeEstate(testEstateConfig())
	if len(estate) != 4 {
		t.Fatalf("expected 4 entries, got %d: %+v", len(estate), estate)
	}
	byRole := map[string]ProviderInfo{}
	for _, e := range estate {
		byRole[e.Role+"|"+e.Model] = e
	}
	primary := byRole["primary|anthropic/claude-test"]
	if primary.Kind != "cloud" || !primary.HasCredential {
		t.Fatalf("primary wrong: %+v", primary)
	}
	local := byRole["fallback|ollama/llama3"]
	if local.Kind != "local" || !local.HasCredential {
		t.Fatalf("local fallback wrong: %+v", local)
	}
	cloudNoKey := byRole["fallback|openai/gpt-test"]
	if cloudNoKey.Kind != "cloud" || cloudNoKey.HasCredential {
		t.Fatalf("keyless cloud fallback must be flagged: %+v", cloudNoKey)
	}
	light := byRole["light|ollama/llama3"]
	if light.Role != "light" {
		t.Fatalf("light role wrong: %+v", light)
	}
}

func TestDescribeEstateNil(t *testing.T) {
	if DescribeEstate(nil) != nil {
		t.Fatal("nil config must yield nil estate")
	}
	if len(DescribeEstate(&config.Config{})) != 0 {
		t.Fatal("empty config must yield empty estate")
	}
}
