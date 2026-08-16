package tools

import (
	"context"
	"fmt"
)

type SubagentReadTool struct {
	manager     *SubagentManager
	transcripts *TranscriptManager
}

func NewSubagentReadTool(manager *SubagentManager, transcripts *TranscriptManager) *SubagentReadTool {
	return &SubagentReadTool{
		manager:     manager,
		transcripts: transcripts,
	}
}

func (t *SubagentReadTool) Name() string {
	return "subagent_read"
}

func (t *SubagentReadTool) Description() string {
	return "Read the live transcript of a running or completed subagent task. Shows the most recent messages."
}

func (t *SubagentReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "The subagent task ID (e.g. subagent-1)",
			},
			"max_lines": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of recent lines to return (default: 50)",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *SubagentReadTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return ErrorResult("task_id is required")
	}

	maxLines := 50
	if ml, ok := args["max_lines"].(float64); ok && ml > 0 {
		maxLines = int(ml)
	}

	if t.transcripts == nil {
		return ErrorResult("Transcript manager not configured")
	}

	lines, err := t.transcripts.ReadLines(taskID, maxLines)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read transcript: %v", err))
	}

	if len(lines) == 0 {
		return UserResult(fmt.Sprintf("No transcript lines found for task %s.", taskID))
	}

	result := fmt.Sprintf("Transcript for %s (%d lines):\n", taskID, len(lines))
	for _, line := range lines {
		result += fmt.Sprintf("[%s] %s | %s\n",
			line.Timestamp.Format("15:04:05"),
			line.Role,
			line.Text)
	}

	return UserResult(result)
}
