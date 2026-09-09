package providers

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/modes"
)

// ProviderInfo is one entry in the device's model estate: which models
// are configured, where they run, and whether credentials exist. It never
// carries key material — only presence.
type ProviderInfo struct {
	Model         string `json:"model"`
	Provider      string `json:"provider"`
	Kind          string `json:"kind"` // "local" | "cloud"
	Role          string `json:"role"` // "primary" | "fallback" | "light"
	HasCredential bool   `json:"has_credential"`
}

// DescribeEstate inventories the configured models from config alone: no
// network, no cost. Local providers need no key; cloud entries report
// whether their key is present so a missing credential is visible at
// startup and in doctor instead of surfacing as a mid-turn failure.
func DescribeEstate(cfg *config.Config) []ProviderInfo {
	if cfg == nil {
		return nil
	}
	primary := cfg.Agents.Defaults.Model
	light := cfg.Agents.Routing.LightModel
	seen := map[string]bool{}
	var out []ProviderInfo
	add := func(model, role string) {
		model = strings.TrimSpace(model)
		if model == "" || seen[model+"|"+role] {
			return
		}
		seen[model+"|"+role] = true
		name, _ := splitModelSpec(model)
		// Defaults.Model is stored WITHOUT the provider prefix (SetModel keeps
		// provider and model separate). Resolve the provider from the model
		// spec OR the configured default provider so credential lookups map to
		// the right entry (e.g. "deepseek-v4-flash" -> provider "deepseek").
		if name == "" {
			name = cfg.Agents.Defaults.Provider
		}
		if name == "" {
			name = model
		}
		kind := "cloud"
		if !modes.IsCloudProvider(model) {
			kind = "local"
		}
		out = append(out, ProviderInfo{
			Model:         model,
			Provider:      name,
			Kind:          kind,
			Role:          role,
			HasCredential: kind == "local" || providerKey(cfg, name) != "",
		})
	}
	add(primary, "primary")
	for _, m := range cfg.Agents.Defaults.FallbackModels {
		add(m, "fallback")
	}
	add(light, "light")
	return out
}

// providerKey returns the configured API key for a provider name, or ""
// when absent. Explicit mapping — no reflection over config structs.
func providerKey(cfg *config.Config, name string) string {
	p := cfg.Providers
	switch strings.ToLower(name) {
	case "anthropic":
		return p.Anthropic.APIKey
	case "openai":
		return p.OpenAI.APIKey
	case "openrouter":
		return p.OpenRouter.APIKey
	case "groq":
		return p.Groq.APIKey
	case "zhipu":
		return p.Zhipu.APIKey
	case "vllm":
		return p.VLLM.APIKey
	case "gemini":
		return p.Gemini.APIKey
	case "nvidia":
		return p.Nvidia.APIKey
	case "moonshot":
		return p.Moonshot.APIKey
	case "shengsuanyun":
		return p.ShengSuanYun.APIKey
	case "deepseek":
		return p.DeepSeek.APIKey
	case "qwen":
		return p.Qwen.APIKey
	case "github_copilot", "copilot", "github-copilot":
		return p.GitHubCopilot.APIKey
	case "ollama":
		return p.Ollama.APIKey
	default:
		return ""
	}
}
