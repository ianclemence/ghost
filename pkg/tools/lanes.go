package tools

import (
	"context"
	"fmt"
)

// LaneTool allows the agent to switch to a different conversation "lane" (isolated context).
// Inspired by OpenClaw's lanes skill.
type LaneTool struct {
	onSwitch func(lane string)
}

func NewLaneTool(onSwitch func(lane string)) *LaneTool {
	return &LaneTool{
		onSwitch: onSwitch,
	}
}

func (t *LaneTool) Name() string {
	return "switch_lane"
}

func (t *LaneTool) Description() string {
	return "Switch to a different conversation lane. Each lane is an isolated context. Use this to separate different tasks (e.g., 'coding', 'personal', 'research')."
}

func (t *LaneTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"lane": map[string]interface{}{
				"type":        "string",
				"description": "The name of the lane to switch to (e.g., 'coding', 'default').",
			},
		},
		"required": []string{"lane"},
	}
}

func (t *LaneTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	lane, _ := args["lane"].(string)
	if lane == "" {
		return ErrorResult("lane name is required")
	}

	if t.onSwitch != nil {
		t.onSwitch(lane)
	}

	return SilentResult(fmt.Sprintf("Switched to lane: %s. Your context is now isolated to this lane.", lane))
}
