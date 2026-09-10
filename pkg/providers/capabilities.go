package providers

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/modes"
)

// Capability describes what a model can do, so routing can decide by ability
// rather than by model name alone. Unknown models get conservative defaults
// (text + tools, modest context) — never an optimistic guess.
type Capability struct {
	ID             string `json:"id"`
	Provider       string `json:"provider"`
	Local          bool   `json:"local"`
	ToolCalling    bool   `json:"tool_calling"`
	Vision         bool   `json:"vision"`
	Reasoning      bool   `json:"reasoning"`
	MaxContext     int    `json:"max_context"`
	EstimatedRAMMB int    `json:"estimated_ram_mb,omitempty"`
}

// Describe returns the capability descriptor for a "provider/model" (or bare
// model) spec. It is deterministic and requires no network call.
func Describe(spec string) Capability {
	provider, model := splitSpec(spec)
	m := strings.ToLower(model)

	c := Capability{ID: model, Provider: provider, Local: !modes.IsCloudProvider(provider),
		ToolCalling: true, MaxContext: 8192}

	switch {
	case strings.Contains(m, "deepseek"):
		c.MaxContext = 1_000_000
		c.Reasoning = true
		c.Vision = strings.Contains(m, "flash") // V4.1-Flash is multimodal
	case strings.Contains(m, "qwen"), strings.Contains(m, "kimi"):
		c.MaxContext = 131_072
		c.Reasoning = strings.Contains(m, "reason") || strings.Contains(m, "thinking")
	case strings.Contains(m, "claude"), strings.Contains(m, "gpt"), strings.Contains(m, "gemini"):
		c.MaxContext = 200_000
		c.Reasoning = true
		c.Vision = true
	}

	if c.Local {
		// Rough resident-memory estimate for local models, used only to
		// avoid loading several large models at once on the Pi.
		switch {
		case strings.Contains(m, "70b"), strings.Contains(m, "72b"):
			c.EstimatedRAMMB = 40_000
		case strings.Contains(m, "27b"), strings.Contains(m, "32b"):
			c.EstimatedRAMMB = 18_000
		case strings.Contains(m, "7b"), strings.Contains(m, "8b"):
			c.EstimatedRAMMB = 6_000
		case strings.Contains(m, "3b"), strings.Contains(m, "0.6b"), strings.Contains(m, "1.5b"):
			c.EstimatedRAMMB = 2_000
		}
	}
	return c
}

// splitSpec separates "provider/model" or "provider:model". A bare model name
// has an empty provider.
func splitSpec(spec string) (string, string) {
	spec = strings.TrimSpace(spec)
	for _, sep := range []string{"/", ":"} {
		if i := strings.Index(spec, sep); i >= 0 {
			return spec[:i], spec[i+1:]
		}
	}
	return "", spec
}
