package tools

import (
	"context"
	"fmt"
)

// CompactionTool allows the agent to explicitly trigger a session summary (compaction) to save tokens.
// Inspired by OpenClaw's compaction.ts.
type CompactionTool struct {
	onCompact func() error
}

func NewCompactionTool(onCompact func() error) *CompactionTool {
	return &CompactionTool{
		onCompact: onCompact,
	}
}

func (t *CompactionTool) Name() string {
	return "compact_context"
}

func (t *CompactionTool) Description() string {
	return "Explicitly trigger a session summarization (compaction). Use this when the conversation history is too long and you want to save tokens by summarizing everything before this point."
}

func (t *CompactionTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Optional reason for compacting (e.g., 'conversation too long', 'switching topics').",
			},
		},
	}
}

func (t *CompactionTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.onCompact != nil {
		if err := t.onCompact(); err != nil {
			return ErrorResult(fmt.Sprintf("Compaction failed: %v", err))
		}
	}

	return SilentResult("Context successfully compacted into a summary. All previous context has been archived.")
}
