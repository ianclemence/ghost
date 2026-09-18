package providers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/sting"
)

// Sting model IDs served by the local sidecar.
const (
	StingBaseModel = "sting/base"
)

// StingProvider routes tool-calling turns through the local Sting
// sidecar instead of a cloud LLM. It only ever returns tool calls —
// never prose. Anything the gate rejects surfaces as an error so the
// FallbackChain moves to the next candidate (Ollama local / cloud):
// escalation, not failure.
type StingProvider struct {
	client       *sting.Client
	threshold    float64
	defaultModel string
	// ledger is the reliability ledger the gate names. Nil means the
	// confidence-only gate (`ledger: "off"`).
	ledger *sting.Tracker
}

// NewStingProvider builds the router from Ghost config. It never
// dials out: availability is probed per turn (Available) so a stopped
// sidecar degrades to normal providers instead of failing turns.
func NewStingProvider(cfg *config.Config) *StingProvider {
	url := sting.DefaultSidecarURL
	threshold := 0.5
	timeout := 15
	weights := ""
	model := StingBaseModel
	if cfg != nil {
		n := cfg.Sting
		if strings.TrimSpace(n.SidecarURL) != "" {
			url = strings.TrimSpace(n.SidecarURL)
		}
		if n.ConfidenceThreshold > 0 {
			threshold = n.ConfidenceThreshold
		}
		if n.TimeoutSecs > 0 {
			timeout = n.TimeoutSecs
		}
		weights = strings.TrimSpace(n.Weights)
		if weights != "" {
			model = "sting/tuned"
		}
	}
	return &StingProvider{
		client:       sting.New(url, timeout, weights),
		threshold:    threshold,
		defaultModel: model,
		ledger:       sting.Ledger(stingLedgerPath(cfg)),
	}
}

// stingLedgerPath resolves the ledger from config: an explicit path, the
// workspace default, or "" for the shipped priors; "off" disables the
// ledger and restores confidence-only gating.
func stingLedgerPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	v := strings.TrimSpace(cfg.Sting.Ledger)
	if strings.EqualFold(v, "off") {
		return "off"
	}
	if v != "" {
		return v
	}
	if ws := cfg.WorkspacePath(); ws != "" {
		return filepath.Join(ws, "state", "sting-ledger.json")
	}
	return ""
}

// GetDefaultModel satisfies LLMProvider.
func (p *StingProvider) GetDefaultModel() string { return p.defaultModel }

// Chat implements LLMProvider over the sidecar. messages supply the
// query (last user turn); tools are capped to the router width with
// caller order preserved.
func (p *StingProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("sting: provider not configured (make install-sting)")
	}
	query := lastUserText(messages)
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("sting: escalate: empty query")
	}
	schemas := toStingSchemas(tools)
	if err := sting.ValidateSubset(schemasToNames(schemas)); err != nil {
		return nil, fmt.Errorf("sting: escalate: %w", err)
	}
	system := systemFacts(messages)
	resp, err := p.client.Complete(ctx, system, query, schemas)
	if err != nil {
		return nil, fmt.Errorf("sting: escalate: %w", err)
	}
	decision := sting.GateWithLedger(query, resp, p.ledger, p.threshold)
	if decision.Escalate {
		return nil, fmt.Errorf("sting: escalate: %s", decision.Reason)
	}
	out := &LLMResponse{FinishReason: "tool_calls"}
	for _, c := range decision.Act {
		args := c.Arguments
		if args == nil {
			args = map[string]interface{}{}
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{Name: c.Name, Arguments: args})
	}
	if decision.Uncalibrated {
		// Surface the fact for the turn log without inventing content.
		out.Content = ""
	}
	return out, nil
}

func lastUserText(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content
		}
	}
	return ""
}

// systemFacts carries environment facts only (date/locale/device), never
// instructions — matching the sidecar contract.
func systemFacts(messages []Message) string {
	for _, m := range messages {
		if m.Role == "system" && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

func toStingSchemas(tools []ToolDefinition) []sting.ToolSchema {
	var out []sting.ToolSchema
	for _, t := range tools {
		if len(out) >= sting.MaxRouterTools {
			break
		}
		name := strings.TrimSpace(t.Function.Name)
		if name == "" {
			continue
		}
		params := t.Function.Parameters
		if params == nil {
			params = map[string]interface{}{"type": "object"}
		}
		out = append(out, sting.ToolSchema{
			Name:        name,
			Description: t.Function.Description,
			Parameters:  params,
		})
	}
	return out
}

func schemasToNames(s []sting.ToolSchema) []string {
	out := make([]string, 0, len(s))
	for _, t := range s {
		out = append(out, t.Name)
	}
	return out
}
