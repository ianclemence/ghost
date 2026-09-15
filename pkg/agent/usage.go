package agent

import (
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/providers"
)

// recordTurnUsage persists one metered row per user turn: session,
// model, tokens, and cost. Cost resolution is measured-first
// (per-call provider figures), static-estimate second, unknown last —
// never silent zero. Best-effort by design: metering must not fail
// turns, so every error path returns silently.
func recordTurnUsage(al *AgentLoop, opts processOptions, model string, iterations, prompt, completion, total int, measuredSum float64, usageResponses, unmeasuredResponses int) {
	if al == nil || al.governance == nil || al.governance.Events == nil {
		return
	}
	if usageResponses == 0 && total == 0 {
		return
	}
	provider, name := splitProviderModel(model)
	cost, unknown := providers.CostForTurn(provider, name, int64(prompt), int64(completion), measuredSum, usageResponses > 0 && unmeasuredResponses == 0)
	al.governance.Events.Publish(&cevents.Event{
		Type:      cevents.UsageRecorded,
		RequestID: opts.RequestID,
		SessionID: opts.SessionKey,
		GhostID:   al.governance.GhostID,
		AgentID:   al.governance.AgentID,
		Payload: map[string]interface{}{
			"session_key":       opts.SessionKey,
			"model":             name,
			"provider":          provider,
			"iterations":        iterations,
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      total,
			"cost_usd":          cost,
			"cost_unknown":      unknown,
			"pricing":           providers.PricingVersion,
		},
	})
}
