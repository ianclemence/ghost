package golden

import (
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// SupportedTarget returns whether a (provider, model) is a supported Ghost
// model target. Qwen is supported but intentionally NOT run on this dev
// appliance (too slow) — the architecture must permit selecting it later.
type ModelInfo struct {
	Target    Target `json:"target"`
	Available bool   `json:"available"` // provider credential configured
	Note      string `json:"note,omitempty"`
}

// DiscoverTargets lists every model Ghost can target from the given config
// directory. Provider availability reflects configured credentials, so a
// model without a key is listed as available=false (NOT RUN on selection).
func DiscoverTargets(configDir string) []ModelInfo {
	var out []ModelInfo
	var cfg *config.Config
	var err error
	if configDir != "" {
		cfg, err = config.LoadConfig(filepath.Join(configDir, "config.json"))
	} else {
		cfg = config.DefaultConfig()
	}
	_ = err
	keyed := map[string]bool{}
	if cfg != nil {
		keyed["deepseek"] = cfg.Providers.DeepSeek.APIKey != ""
		keyed["anthropic"] = cfg.Providers.Anthropic.APIKey != ""
		keyed["openai"] = cfg.Providers.OpenAI.APIKey != ""
		keyed["openrouter"] = cfg.Providers.OpenRouter.APIKey != ""
		keyed["groq"] = cfg.Providers.Groq.APIKey != ""
		keyed["ollama"] = true // local, no key
	}
	// Default/configured model first.
	if cfg != nil {
		prov := cfg.Agents.Defaults.Provider
		mdl := cfg.Agents.Defaults.Model
		out = append(out, ModelInfo{Target: Target{Provider: prov, Model: mdl}, Available: keyed[prov], Note: qwenNote(mdl)})
		for _, p := range cfg.Agents.ModelList {
			mp := p.Provider
			if mp == "" {
				mp = prov
			}
			if mp == prov && p.Model == mdl {
				continue
			}
			out = append(out, ModelInfo{Target: Target{Provider: mp, Model: p.Model}, Available: keyed[mp], Note: qwenNote(p.Model)})
		}
	}
	// Known cloud providers with a configured key that have no explicit model
	// preset: offer their canonical default via the shared HTTP provider.
	for _, mp := range []string{"anthropic", "openai", "openrouter", "groq"} {
		if keyed[mp] {
			seen := false
			for _, o := range out {
				if o.Target.Provider == mp {
					seen = true
				}
			}
			if !seen {
				out = append(out, ModelInfo{Target: Target{Provider: mp, Model: mp + "/*"}, Available: true,
					Note: "provider configured; select an exact model for a run"})
			}
		}
	}
	return out
}

func qwenNote(model string) string {
	if isQwen(Target{Model: model}) {
		return "supported; intentionally NOT RUN on this development appliance (too slow)"
	}
	return ""
}

// Select resolves a CLI model spec (provider/model or model) to a Target.
func Select(spec string) Target {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Target{}
	}
	if i := strings.Index(spec, "/"); i > 0 {
		return Target{Provider: spec[:i], Model: spec[i+1:]}
	}
	// provider-only or model-only: prefer provider=spec if it looks like a
	// provider name.
	providers := map[string]bool{"deepseek": true, "anthropic": true, "openai": true,
		"openrouter": true, "groq": true, "ollama": true}
	if providers[spec] {
		return Target{Provider: spec}
	}
	return Target{Model: spec}
}
