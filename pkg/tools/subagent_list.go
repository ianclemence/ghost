package tools

import (
	"context"
	"encoding/json"
)

type SubagentListTool struct {
	manager *SubagentManager
}

func NewSubagentListTool(manager *SubagentManager) *SubagentListTool {
	return &SubagentListTool{manager: manager}
}

func (t *SubagentListTool) Name() string {
	return "subagent_list"
}

func (t *SubagentListTool) Description() string {
	return "List all active subagent tasks with their status, labels, and creation time."
}

func (t *SubagentListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *SubagentListTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.manager == nil {
		return ErrorResult("Subagent manager not configured")
	}

	tasks := t.manager.ListTasks()
	if len(tasks) == 0 {
		return UserResult("No active subagent tasks.")
	}

	var result []map[string]interface{}
	for _, task := range tasks {
		entry := map[string]interface{}{
			"id":     task.ID,
			"label":  task.Label,
			"task":   task.Task,
			"status": task.Status,
		}
		if task.Created > 0 {
			entry["created"] = task.Created
		}
		if task.Result != "" {
			resultLen := len(task.Result)
			if resultLen > 200 {
				resultLen = 200
			}
			entry["result_preview"] = task.Result[:resultLen] + "..."
		}
		result = append(result, entry)
	}

	raw, _ := json.Marshal(result)
	return UserResult(string(raw))
}
