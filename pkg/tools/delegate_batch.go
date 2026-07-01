package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type BatchDelegateTool struct {
	manager *SubagentManager
}

type BatchTask struct {
	Task  string `json:"task"`
	Label string `json:"label"`
}

type BatchResult struct {
	TaskIndex  int     `json:"task_index"`
	Task       string  `json:"task"`
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	Summary    string  `json:"summary"`
	Iterations int     `json:"iterations"`
	Duration   float64 `json:"duration_seconds"`
	Error      string  `json:"error,omitempty"`
}

func NewBatchDelegateTool(manager *SubagentManager) *BatchDelegateTool {
	return &BatchDelegateTool{
		manager: manager,
	}
}

func (t *BatchDelegateTool) Name() string {
	return "batch_delegate"
}

func (t *BatchDelegateTool) Description() string {
	return "Execute multiple independent tasks in parallel using subagents. Returns results for all tasks."
}

func (t *BatchDelegateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"tasks": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task": map[string]interface{}{
							"type":        "string",
							"description": "Task description for the subagent",
						},
						"label": map[string]interface{}{
							"type":        "string",
							"description": "Optional label for the task",
						},
					},
					"required": []string{"task"},
				},
				"description": "List of tasks to execute in parallel (max 5)",
			},
			"max_workers": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum parallel workers (default 3, max 5)",
				"default":     3,
			},
		},
		"required": []string{"tasks"},
	}
}

func (t *BatchDelegateTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return ErrorResult("tasks array is required")
	}

	if len(tasksRaw) > 5 {
		return ErrorResult("maximum 5 tasks allowed")
	}

	maxWorkers := 3
	if mw, ok := args["max_workers"].(float64); ok && mw > 0 {
		maxWorkers = int(mw)
		if maxWorkers > 5 {
			maxWorkers = 5
		}
	}

	var tasks []BatchTask
	for _, raw := range tasksRaw {
		taskMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		task := BatchTask{
			Task: getString(taskMap, "task"),
		}
		if task.Task == "" {
			continue
		}
		task.Label = getStringDefault(taskMap, "label", fmt.Sprintf("task-%d", len(tasks)+1))
		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return ErrorResult("no valid tasks provided")
	}

	results := t.runBatch(ctx, tasks, maxWorkers)

	payload := map[string]interface{}{
		"count":   len(results),
		"results": results,
	}
	raw, _ := json.Marshal(payload)
	return UserResult(string(raw))
}

func (t *BatchDelegateTool) runBatch(ctx context.Context, tasks []BatchTask, maxWorkers int) []BatchResult {
	results := make([]BatchResult, len(tasks))
	taskCh := make(chan int, len(tasks))
	var wg sync.WaitGroup

	for i := range tasks {
		taskCh <- i
	}
	close(taskCh)

	workerCount := maxWorkers
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				results[idx] = t.runSingle(ctx, tasks[idx], idx)
			}
		}()
	}

	wg.Wait()
	return results
}

func (t *BatchDelegateTool) runSingle(ctx context.Context, task BatchTask, index int) BatchResult {
	start := time.Now()

	result := BatchResult{
		TaskIndex: index,
		Task:      task.Task,
		Label:     task.Label,
	}

	loopResult, err := t.manager.RunSync(ctx, task.Task, task.Label, "", "")
	duration := time.Since(start).Seconds()
	result.Duration = duration

	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		return result
	}

	if loopResult != nil {
		result.Status = "completed"
		result.Summary = loopResult.Content
		result.Iterations = loopResult.Iterations
	} else {
		result.Status = "completed"
		result.Summary = ""
	}

	return result
}

func (t *BatchDelegateTool) SetContext(channel, chatID string) {
	t.manager.mu.Lock()
	t.manager.mu.Unlock()
}
