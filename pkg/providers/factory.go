package providers

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

func CreateProviderForModel(cfg *config.Config, model string) (LLMProvider, error) {
	c := *cfg
	c.Agents = cfg.Agents
	c.Agents.Defaults = cfg.Agents.Defaults
	if strings.Contains(model, "/") {
		parts := strings.SplitN(model, "/", 2)
		c.Agents.Defaults.Provider = parts[0]
		c.Agents.Defaults.Model = parts[1]
	} else {
		c.Agents.Defaults.Model = model
	}
	return CreateProvider(&c)
}
