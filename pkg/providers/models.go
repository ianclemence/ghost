package providers

import (
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// KnownProviderModels is the recommended model per provider, mirrored by
// the web console and the gateway. Single source: previously three copies
// drifted (cmd/ghost duplicated it), so every surface reads this one.
var KnownProviderModels = map[string][]string{
	"openai":       {"gpt-5.4", "gpt-5.4-mini", "gpt-5", "gpt-5-mini", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-nano", "o3", "o4-mini", "gpt-4o", "gpt-4o-mini"},
	"anthropic":    {"claude-fable-5", "claude-opus-5", "claude-sonnet-5", "claude-haiku-4-5", "claude-sonnet-4-6", "claude-opus-4-6"},
	"moonshot":     {"kimi-k3", "kimi-k2.7-code", "kimi-k2.6"},
	"groq":         {"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "openai/gpt-oss-120b", "openai/gpt-oss-20b", "qwen/qwen3.6-27b"},
	"deepseek":     {"deepseek-flash", "deepseek-v4-pro"},
	"qwen":         {"qwen3.8-max", "qwen3.7-plus", "qwen3.8-flash", "qwen3.5-omni-plus"},
	"gemini":       {"gemini-3.6-flash", "gemini-3.1-pro", "gemini-3-flash"},
	"zhipu":        {"glm-5.3", "glm-5.3-flash", "glm-5.2", "glm-4.7", "glm-4.7-flash"},
	"openrouter":   {},
	"ollama":       {},
	"nvidia":       {"deepseek-ai/deepseek-v4-flash", "meta/llama-3.3-70b-instruct", "qwen/qwq-32b"},
	"shengsuanyun": {},
}

// ModelOption is one selectable model: a named preset, a named
// connection, or a configured provider's default model. Target is the
// exact string SetModel accepts. It never carries key material.
type ModelOption struct {
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Target    string `json:"target"`
	Kind      string `json:"kind"` // "preset" | "connection" | "provider"
	Available bool   `json:"available"`
	Reason    string `json:"unavailable_reason,omitempty"`
}

// AvailableModelOptions lists everything the user can switch to: named
// presets first, then named connections, then every provider with a
// configured credential (so a configured-but-unlisted model like deepseek
// is visible instead of silently missing). Within providers, entries
// already covered by a preset or connection are skipped. Sorted for
// stable pickers: presets and connections in config order, providers
// alphabetical.
func AvailableModelOptions(cfg *config.Config) []ModelOption {
	var out []ModelOption
	if cfg == nil {
		return out
	}
	covered := map[string]bool{} // provider+"\x00"+model already selectable
	for _, p := range cfg.Agents.ModelList {
		if p.Name == "" {
			continue
		}
		ok, reason := PresetAvailable(cfg, p.Provider, p.Model)
		out = append(out, ModelOption{
			Name: p.Name, Provider: p.Provider, Model: p.Model,
			Target: p.Name, Kind: "preset", Available: ok, Reason: reason,
		})
		covered[p.Provider+"\x00"+p.Model] = true
	}
	for _, c := range cfg.Agents.Connections {
		if c.Name == "" {
			continue
		}
		ok := true
		reason := ""
		if _, err := cfg.ResolveConnection(c.Name); err != nil {
			ok = false
			reason = err.Error()
		}
		out = append(out, ModelOption{
			Name: c.Name, Provider: c.Provider, Model: c.Model,
			Target: c.Name, Kind: "connection", Available: ok, Reason: reason,
		})
		covered[c.Provider+"\x00"+c.Model] = true
	}
	var providers []string
	for name, models := range KnownProviderModels {
		if len(models) == 0 {
			continue
		}
		provider, model := name, models[0]
		if covered[provider+"\x00"+model] {
			continue
		}
		ok, reason := PresetAvailable(cfg, provider, model)
		if !ok {
			continue // no credential (or unreachable local): not selectable
		}
		_ = reason
		providers = append(providers, name)
	}
	sort.Strings(providers)
	for _, name := range providers {
		model := KnownProviderModels[name][0]
		out = append(out, ModelOption{
			Name: name, Provider: name, Model: model,
			Target: name + ":" + model, Kind: "provider",
			Available: true,
		})
	}
	return out
}

// optionTargetBase strips provider prefixes for loose current-model
// matching ("provider:model" and "provider/model" hit the same base).
func optionTargetBase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexAny(s, ":/"); i >= 0 {
		s = s[i+1:]
	}
	return s
}
