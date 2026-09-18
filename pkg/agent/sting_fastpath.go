package agent

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/sting"
)

// tryStingTurn is the offline tool-router fast-path: Ghost's own
// read-only tools, routed locally through the Sting sidecar with zero
// LLM calls. It sits after tryDeterministicNetworkDispatch and before
// effort triage.
//
// Safety contract (why this is read-only only):
//   - Sting proposes calls; the gate enforces confidence + strict
//     grounding (no negated/missing-slot/ungrounded calls through).
//   - Only capability.Risk == read_only calls execute here. Anything
//     else escalates to the full governed loop, where the Permission
//     Broker allows/asks/denies and runtime evidence decides.
//   - Actuation through Sting is still available, but only inside the
//     normal loop via StingProvider (pkg/providers/sting_provider.go),
//     where every call passes AuthorizeTool. The fast-path never mints
//     approvals, so it can never duplicate or orphan a pending card.
//
// Returns (answer, handled). Unavailable sidecar, empty subset, gate
// escalate, or a non-read-only call all return handled=false: the turn
// falls through to effort triage and the normal provider path.
func (al *AgentLoop) tryStingTurn(msg, session string) (string, bool) {
	if al == nil || al.cfg == nil || !al.cfg.Sting.Enabled {
		return "", false
	}
	if al.tools == nil {
		return "", false
	}
	if strings.TrimSpace(msg) == "" || isSecurityProbe(msg) {
		return "", false
	}
	names := sting.Subset(al.tools.List(), sting.DefaultRouterTools, sting.MaxRouterTools)
	if err := sting.ValidateSubset(names); err != nil {
		return "", false
	}
	// Keep only read-only capabilities: resolve each candidate against
	// the capability registry with empty args (action-level splits like
	// calendar.read/modify resolve conservatively at execution time, and
	// anything not provably read-only escalates).
	defs := al.tools.ToProviderDefs()
	byName := map[string]providers.ToolDefinition{}
	for _, d := range defs {
		byName[d.Function.Name] = d
	}
	var schemas []sting.ToolSchema
	for _, n := range names {
		d, ok := byName[n]
		if !ok {
			continue
		}
		spec, found := capability.ForToolAction(n, nil)
		if !found || spec.Risk != capability.RiskReadOnly {
			continue
		}
		params := d.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object"}
		}
		schemas = append(schemas, sting.ToolSchema{
			Name:        n,
			Description: d.Function.Description,
			Parameters:  params,
		})
	}
	if err := sting.ValidateSubset(schemaNames(schemas)); err != nil {
		return "", false
	}
	// Rollout-evidence logging: the (proposal, gate, evidence) triple
	// future improvement loops consume. Nil-safe no-op unless enabled;
	// deferred Flush covers every exit below.
	rec := sting.NewRecorder(stingRolloutPath(al), stingWeightsTag(al))
	defer rec.Flush()
	rec.Begin(msg, schemaNames(schemas))
	// Triage first (Jev-shaped closed set): Ask/Refuse skip the
	// generative call and return the turn — readiness owns the
	// question, the loop owns the rest. Only Act proceeds.
	tri := sting.Triage(msg, schemas)
	rec.Triaged(tri)
	if tri.Verdict != sting.TriageAct {
		return "", false
	}
	timeout := al.cfg.Sting.TimeoutSecs
	if timeout <= 0 {
		timeout = 15
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	client := sting.New(al.cfg.Sting.SidecarURL, timeout, strings.TrimSpace(al.cfg.Sting.Weights))
	resp, err := client.Complete(ctx, stingDateFact(), msg, schemas)
	if err != nil {
		return "", false // sidecar down: escalate, never fail the turn
	}
	threshold := al.cfg.Sting.ConfidenceThreshold
	if threshold <= 0 {
		threshold = 0.5
	}
	decision := sting.GateWithLedger(msg, resp, stingLedger(al), threshold)
	rec.Propose(resp, decision)
	if decision.Escalate || len(decision.Act) == 0 {
		return "", false
	}
	// Re-check risk with real args (action splits), then execute each
	// read-only call through the same deterministic executor — validated
	// output or honest product-language failure, never a fabricated claim.
	var parts []string
	for _, call := range decision.Act {
		spec, found := capability.ForToolAction(call.Name, call.Arguments)
		if !found || spec.Risk != capability.RiskReadOnly {
			return "", false
		}
		ans, ok := al.execDeterministicTool(call.Name, call.Arguments, session)
		// Grade routing, not luck: ok=false means the call never ran
		// (router fault). A runtime-produced answer — even an honest
		// failure message — means the mapping query→call was correct.
		rec.Acted(call, ok, false)
		if !ok {
			return "", false
		}
		if strings.TrimSpace(ans) != "" {
			parts = append(parts, ans)
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

// stingRolloutPath resolves the rollout log: explicit path, workspace
// default, or "" (disabled) for "off".
func stingRolloutPath(al *AgentLoop) string {
	if al == nil || al.cfg == nil {
		return ""
	}
	if v := strings.TrimSpace(al.cfg.Sting.RolloutLog); v != "" {
		if v == "off" {
			return ""
		}
		return v
	}
	ws := ""
	if al.workspace != "" {
		ws = al.workspace
	} else {
		ws = al.cfg.WorkspacePath()
	}
	if ws == "" {
		return ""
	}
	return filepath.Join(ws, "state", "sting-rollouts.jsonl")
}

// stingLedger resolves the reliability ledger the gate names: explicit
// path, workspace default, or "" for the shipped priors; "off" disables
// the ledger and restores confidence-only gating.
func stingLedger(al *AgentLoop) *sting.Tracker {
	if al == nil || al.cfg == nil {
		return nil
	}
	if v := strings.TrimSpace(al.cfg.Sting.Ledger); v != "" {
		return sting.Ledger(v)
	}
	ws := al.workspace
	if ws == "" {
		ws = al.cfg.WorkspacePath()
	}
	if ws == "" {
		return sting.Ledger("")
	}
	return sting.Ledger(filepath.Join(ws, "state", "sting-ledger.json"))
}

// stingWeightsTag labels which head produced a rollout so ledgers never
// mix base and tuned populations.
func stingWeightsTag(al *AgentLoop) string {
	if al == nil || al.cfg == nil {
		return "base"
	}
	if w := strings.TrimSpace(al.cfg.Sting.Weights); w != "" {
		return "tuned:" + filepath.Base(w)
	}
	return "base"
}

// the Sting python client does: a system fact, never an instruction.
// stingDateFact licenses relative dates ("tomorrow at 7") the same way
// the Sting python client does: a system fact, never an instruction.
func stingDateFact() string {
	return time.Now().Format("date: 2006-01-02 Mon 15:04")
}

func schemaNames(s []sting.ToolSchema) []string {
	out := make([]string, 0, len(s))
	for _, t := range s {
		out = append(out, t.Name)
	}
	return out
}
