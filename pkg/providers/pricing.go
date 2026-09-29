package providers

import "strings"

// Static price table, USD per 1M tokens. Versioned estimates for
// providers that do not report measured cost — clearly marked, never
// confused with metered spend. Unknown providers stay unknown (never
// zero). Refresh when contracts change; measured cost always wins.
const PricingVersion = "pricing/v2-2026-09"

// modelPrice holds per-1M-token input/output prices. Entries marked
// estimated come from public list prices, not metering.
type modelPrice struct {
	input, output float64
	estimated     bool
}

var priceTable = map[string]modelPrice{
	// DeepSeek V3.x lists ~$0.27/M in / $1.10/M out (cache miss).
	"deepseek:deepseek-flash": {0.27, 1.10, true},
	"deepseek:deepseek-chat":  {0.27, 1.10, true},
	// Kimi K2.5 class.
	"moonshot:kimi-k2.5": {0.60, 2.50, true},
	"moonshot:kimi-k2":   {0.60, 2.50, true},
	// GPT-4.1 class.
	"openai:gpt-4.1":     {2.00, 8.00, true},
	"openai:gpt-4o":      {2.50, 10.00, true},
	"openai:gpt-4o-mini": {0.15, 0.60, true},
	// Anthropic list prices (claude.com/pricing, 2026-09).
	"anthropic:claude-fable-5-1":  {10.00, 50.00, true},
	"anthropic:claude-fable-5":    {10.00, 50.00, true},
	"anthropic:claude-opus-5-5":   {4.00, 20.00, true},
	"anthropic:claude-opus-5":     {5.00, 25.00, true},
	"anthropic:claude-opus-4-8":   {5.00, 25.00, true},
	"anthropic:claude-opus-4-7":   {5.00, 25.00, true},
	"anthropic:claude-opus-4-6":   {5.00, 25.00, true},
	"anthropic:claude-sonnet-5-5": {2.00, 10.00, true},
	"anthropic:claude-sonnet-5":   {2.00, 10.00, true},
	"anthropic:claude-sonnet-4-6": {3.00, 15.00, true},
	"anthropic:claude-haiku-4-5":  {1.00, 5.00, true},
	"groq:llama-3.3-70b":          {0.59, 0.79, true},
	"gemini:gemini-2.5-flash":     {0.30, 2.50, true},
	// Local inference has no marginal API cost.
	"ollama:": {0, 0, false},
}

// EstimateCost prices token counts from the static table. The second
// return reports whether any number came back: false means unpriced
// (unknown, never zero). Provider prefixes match first ("ollama:" covers
// every local model); exact "provider:model" entries win.
func EstimateCost(provider, model string, prompt, completion int64) (float64, bool) {
	key := strings.ToLower(strings.TrimSpace(provider) + ":" + strings.TrimSpace(model))
	if p, ok := priceTable[key]; ok {
		return float64(prompt)*p.input/1e6 + float64(completion)*p.output/1e6, true
	}
	for prefix, p := range priceTable {
		if strings.HasSuffix(prefix, ":") && strings.HasPrefix(key, prefix) {
			return float64(prompt)*p.input/1e6 + float64(completion)*p.output/1e6, true
		}
	}
	return 0, false
}

// Prompt-cache price multipliers on the input rate (Anthropic: reads 0.1x,
// 5-minute writes 1.25x). Providers without a cache report zero cache
// tokens, so these never apply to them.
const (
	cacheReadMultiplier  = 0.10
	cacheWriteMultiplier = 1.25
)

// EstimateCostCached prices a prompt of which cacheRead tokens were served
// from the provider's prompt cache and cacheWrite tokens were written to it
// (both included in prompt). Without it a cached turn is priced as if every
// token were fresh — about 10x too high on a warm cache.
func EstimateCostCached(provider, model string, prompt, completion, cacheRead, cacheWrite int64) (float64, bool) {
	base, ok := EstimateCost(provider, model, 1_000_000, 0)
	if !ok {
		return 0, false
	}
	out, _ := EstimateCost(provider, model, 0, 1_000_000)
	fresh := prompt - cacheRead - cacheWrite
	if fresh < 0 {
		fresh = 0
	}
	in := base / 1e6
	return float64(fresh)*in + float64(cacheRead)*in*cacheReadMultiplier +
		float64(cacheWrite)*in*cacheWriteMultiplier + float64(completion)*out/1e6, true
}

// CostForTurn resolves one turn's cost: measured totals win outright;
// otherwise the static table estimates from token counts (cache-aware);
// otherwise unknown. measuredComplete must be true only when every LLM
// call in the turn reported measured cost.
func CostForTurn(provider, model string, prompt, completion, cacheRead, cacheWrite int64, measuredSum float64, measuredComplete bool) (cost float64, unknown bool) {
	if measuredComplete {
		return measuredSum, false
	}
	if est, ok := EstimateCostCached(provider, model, prompt, completion, cacheRead, cacheWrite); ok {
		return est, false
	}
	return 0, true
}
