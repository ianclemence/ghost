package moa

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tools"
)

// MoATool wraps the MoA system as a Ghost tool.
// When invoked, it fans out the current conversation to multiple advisor
// models and aggregates the results.
type MoATool struct {
	moa      *MoA
	messages []providers.Message // current conversation context
}

// NewMoATool creates a new MoA tool.
func NewMoATool(moa *MoA) *MoATool {
	return &MoATool{moa: moa}
}

// SetMessages provides the current conversation context to the tool.
func (t *MoATool) SetMessages(msgs []providers.Message) {
	t.messages = msgs
}

func (t *MoATool) Name() string { return "moa" }

func (t *MoATool) Description() string {
	return "Mixture of Agents — consults multiple AI models in parallel and aggregates the best advice. Use for complex reasoning tasks where multiple perspectives improve quality."
}

func (t *MoATool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "The specific question or task to consult the MoA panel about",
			},
		},
		"required": []string{"query"},
	}
}

func (t *MoATool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return tools.ErrorResult("query is required")
	}

	if !t.moa.ShouldUseMoA() {
		return tools.ErrorResult("MoA is not enabled or has fewer than 2 advisors configured")
	}

	// Build messages: inject the query as a user message on top of context
	var messages []providers.Message
	messages = append(messages, t.messages...)

	// Add the specific query if not already the last user message
	if len(messages) == 0 || messages[len(messages)-1].Content != query {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: query,
		})
	}

	result, err := t.moa.Run(ctx, messages)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("MoA failed: %s", err.Error()))
	}

	// Format output
	output := fmt.Sprintf("%s\n\n## Aggregated Response\n\n%s",
		result.FormatResult(), result.Aggregated)

	return tools.NewToolResult(output)
}

// MoAStatusTool shows the current MoA configuration and status.
type MoAStatusTool struct {
	moa *MoA
}

// NewMoAStatusTool creates a new MoA status tool.
func NewMoAStatusTool(moa *MoA) *MoAStatusTool {
	return &MoAStatusTool{moa: moa}
}

func (t *MoAStatusTool) Name() string { return "moa_status" }

func (t *MoAStatusTool) Description() string {
	return "Show the Mixture of Agents configuration: active advisors, aggregator model, and status."
}

func (t *MoAStatusTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *MoAStatusTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	t.moa.mu.RLock()
	defer t.moa.mu.RUnlock()

	status := map[string]interface{}{
		"enabled":             t.moa.config.Enabled,
		"advisor_count":       len(t.moa.config.Advisors),
		"aggregator_provider": t.moa.config.AggregatorProvider,
		"aggregator_model":    t.moa.config.AggregatorModel,
		"timeout_seconds":     t.moa.config.TimeoutSeconds,
		"max_advisors":        t.moa.config.MaxAdvisors,
		"advisors":            t.moa.config.Advisors,
	}

	data, _ := json.MarshalIndent(status, "", "  ")
	return tools.NewToolResult(string(data))
}
