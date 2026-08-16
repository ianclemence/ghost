package tools

import (
	"context"
	"fmt"
)

type SubagentStopTool struct {
	manager *SubagentManager
}

func NewSubagentStopTool(manager *SubagentManager) *SubagentStopTool {
	return &SubagentStopTool{manager: manager}
}

func (t *SubagentStopTool) Name() string {
	return "subagent_stop"
}

func (t *SubagentStopTool) Description() string {
	return "Stop a running subagent task by its task ID. The task will be cancelled."
}

func (t *SubagentStopTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "The subagent task ID to stop (e.g. subagent-1)",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *SubagentStopTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return ErrorResult("task_id is required")
	}

	if t.manager == nil {
		return ErrorResult("Subagent manager not configured")
	}

	task, ok := t.manager.GetTask(taskID)
	if !ok {
		return ErrorResult(fmt.Sprintf("task %s not found", taskID))
	}

	if task.Status != "running" {
		return UserResult(fmt.Sprintf("Task %s is already %s.", taskID, task.Status))
	}

	t.manager.mu.Lock()
	task.Status = "cancelled"
	task.Result = "Cancelled by user"
	t.manager.mu.Unlock()

	return UserResult(fmt.Sprintf("Task %s stopped.", taskID))
}
