package providers

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// splitModelSpec splits a "provider:model" or "provider/model" spec, preferring
// the colon form because a local model name may itself contain "/" (e.g.
// "ollama/qwen3:0.6b"). The slash form is only used for the common
// "openrouter/anthropic:claude" style where the leading segment is a provider.
func splitModelSpec(spec string) (string, string) {
	if strings.Contains(spec, ":") {
		parts := strings.SplitN(spec, ":", 2)
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1]
		}
	}
	if strings.Contains(spec, "/") {
		parts := strings.SplitN(spec, "/", 2)
		if parts[0] != "" && parts[1] != "" {
			return parts[0], parts[1]
		}
	}
	return "", spec
}

func CreateProviderForModel(cfg *config.Config, model string) (LLMProvider, error) {
	c := cfg
	provider, mdl := splitModelSpec(model)
	if provider != "" {
		c.Agents.Defaults.Provider = provider
	}
	c.Agents.Defaults.Model = mdl
	return CreateProvider(c)
}
