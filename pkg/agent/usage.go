package agent

import (
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/providers"
)

// usageMeasured reports whether a provider usage block carries a real
// measurement. Some providers — Anthropic streaming in particular — always
// return a non-nil Usage, sometimes with every field zero. Counting a
// zero-filled block as "measured" recorded the turn as free and skipped the
// estimate, so a streamed turn reported usage_source "measured" with zero
// tokens instead of honestly estimating. A block with no signal is not a
// measurement.
func usageMeasured(u *providers.UsageInfo) bool {
	if u == nil {
		return false
	}
	return u.PromptTokens > 0 || u.CompletionTokens > 0 || u.TotalTokens > 0 || u.CostUSD > 0
}

// recordTurnUsage persists one metered row per user turn: session,
// model, tokens, and cost. Cost resolution is measured-first
// (per-call provider figures), static-estimate second, unknown last —
// never silent zero. Best-effort by design: metering must not fail
// turns, so every error path returns silently.
// turnCache is the turn's prompt-cache usage (tokens read from and written
// to the provider's cache), zero for providers without one.
type turnCache struct{ read, write int }

func recordTurnUsage(al *AgentLoop, opts processOptions, model string, iterations, prompt, completion, total int, measuredSum float64, usageResponses, unmeasuredResponses int, usageSource string, cache turnCache) {
	if al == nil || al.governance == nil || al.governance.Events == nil {
		return
	}
	if usageResponses == 0 && total == 0 {
		return
	}
	provider, name := splitProviderModel(model)
	cost, unknown := providers.CostForTurn(provider, name, int64(prompt), int64(completion), int64(cache.read), int64(cache.write), measuredSum, usageResponses > 0 && unmeasuredResponses == 0)
	al.governance.Events.Publish(&cevents.Event{
		Type:      cevents.UsageRecorded,
		RequestID: opts.RequestID,
		SessionID: opts.SessionKey,
		GhostID:   al.governance.GhostID,
		AgentID:   al.governance.AgentID,
		Payload: map[string]interface{}{
			"session_key":        opts.SessionKey,
			"model":              name,
			"provider":           provider,
			"iterations":         iterations,
			"prompt_tokens":      prompt,
			"completion_tokens":  completion,
			"total_tokens":       total,
			"cache_read_tokens":  cache.read,
			"cache_write_tokens": cache.write,
			"usage_source":       usageSource,
			"cost_usd":           cost,
			"cost_unknown":       unknown,
			"pricing":            providers.PricingVersion,
		},
	})
}
