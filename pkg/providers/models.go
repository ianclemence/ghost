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

// AllModelOptions lists every switchable entry the runtime knows about:
// named presets first, then named connections, then every provider that has
// a recommended model. Unusable entries (no credential, unreachable local)
// are included and marked Available=false with a Reason, so callers can show
// what exists and resolve an explicitly requested target. Selection surfaces
// use AvailableModelOptions instead. Within providers, entries already
// covered by a preset or connection are skipped. Sorted for stable pickers:
// presets and connections in config order, providers alphabetical.
func AllModelOptions(cfg *config.Config) []ModelOption {
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
		if covered[name+"\x00"+models[0]] {
			continue
		}
		providers = append(providers, name)
	}
	sort.Strings(providers)
	for _, name := range providers {
		model := KnownProviderModels[name][0]
		ok, reason := PresetAvailable(cfg, name, model)
		out = append(out, ModelOption{
			Name: name, Provider: name, Model: model,
			Target: name + ":" + model, Kind: "provider",
			Available: ok, Reason: reason,
		})
	}
	return out
}

// AvailableModelOptions lists only the entries that can actually serve right
// now — a configured credential, or a reachable local engine. This is the
// picker's set, so the terminal never offers a model that would fail on
// selection. Use AllModelOptions when you need the full catalog
// (exact-reference resolution).
func AvailableModelOptions(cfg *config.Config) []ModelOption {
	all := AllModelOptions(cfg)
	out := make([]ModelOption, 0, len(all))
	for _, o := range all {
		if o.Available {
			out = append(out, o)
		}
	}
	return out
}

// FindModelOption resolves an exact target (preset/connection name or
// "provider:model" / "provider/model") against the full catalog, so an
// explicitly requested model is accepted even when its provider is not yet
// configured. Matching uses the same loose base comparison the picker uses.
func FindModelOption(cfg *config.Config, target string) (ModelOption, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return ModelOption{}, false
	}
	base := optionTargetBase(target)
	for _, o := range AllModelOptions(cfg) {
		if o.Target == target || o.Name == target || optionTargetBase(o.Target) == base || optionTargetBase(o.Model) == base {
			return o, true
		}
	}
	return ModelOption{}, false
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

// CycleOption returns the next option after the current one, wrapping.
// ok is false when there is nothing to cycle to (fewer than two options).
func CycleOption(options []ModelOption, cur string, delta int) (ModelOption, bool) {
	if len(options) < 2 {
		return ModelOption{}, false
	}
	base := optionTargetBase(cur)
	idx := -1
	for i, o := range options {
		if o.Target == cur || o.Name == cur || optionTargetBase(o.Target) == base || optionTargetBase(o.Model) == base {
			idx = i
			break
		}
	}
	if idx == -1 {
		idx = 0
		// First press with no known current lands on the first candidate.
		if delta > 0 {
			return options[0], true
		}
		return options[len(options)-1], true
	}
	next := options[((idx+delta)%len(options)+len(options))%len(options)]
	return next, true
}
