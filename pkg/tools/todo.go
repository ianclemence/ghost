package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type TodoItem struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type TodoStore struct {
	items []TodoItem
	mu    sync.RWMutex
}

type TodoTool struct {
	store *TodoStore
}

func NewTodoTool() *TodoTool {
	return &TodoTool{
		store: &TodoStore{},
	}
}

func (t *TodoTool) Name() string {
	return "todo"
}

func (t *TodoTool) Description() string {
	return "Manage a task list for decomposing complex work. Track progress through pending, in_progress, completed, and cancelled states."
}

func (t *TodoTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"write", "read", "clear"},
				"description": "write: create/update items, read: list all items, clear: reset list",
			},
			"items": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type":        "string",
							"description": "Unique identifier for the item",
						},
						"content": map[string]interface{}{
							"type":        "string",
							"description": "Task description",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
							"description": "Task status",
						},
					},
					"required": []string{"id", "content"},
				},
				"description": "Items to write (required for write action)",
			},
			"merge": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, update existing items by ID. If false, replace all items.",
				"default":     true,
			},
		},
		"required": []string{"action"},
	}
}

func (t *TodoTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("action is required")
	}

	switch action {
	case "write":
		return t.handleWrite(args)
	case "read":
		return t.handleRead()
	case "clear":
		return t.handleClear()
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

func (t *TodoTool) handleWrite(args map[string]interface{}) *ToolResult {
	itemsRaw, ok := args["items"].([]interface{})
	if !ok || len(itemsRaw) == 0 {
		return ErrorResult("items array is required for write action")
	}

	merge := true
	if m, ok := args["merge"].(bool); ok {
		merge = m
	}

	var items []TodoItem
	for _, raw := range itemsRaw {
		itemMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		item := TodoItem{
			ID:        getString(itemMap, "id"),
			Content:   getString(itemMap, "content"),
			Status:    getStringDefault(itemMap, "status", "pending"),
			CreatedAt: time.Now().UnixMilli(),
			UpdatedAt: time.Now().UnixMilli(),
		}
		if item.ID == "" || item.Content == "" {
			continue
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return ErrorResult("no valid items provided")
	}

	result := t.store.Write(items, merge)

	payload := map[string]interface{}{
		"action": "write",
		"count":  len(result),
		"items":  result,
	}
	raw, _ := json.Marshal(payload)
	return UserResult(string(raw))
}

func (t *TodoTool) handleRead() *ToolResult {
	items := t.store.Read()

	payload := map[string]interface{}{
		"action": "read",
		"count":  len(items),
		"items":  items,
	}
	raw, _ := json.Marshal(payload)
	return UserResult(string(raw))
}

func (t *TodoTool) handleClear() *ToolResult {
	t.store.Clear()

	payload := map[string]interface{}{
		"action": "clear",
		"count":  0,
	}
	raw, _ := json.Marshal(payload)
	return UserResult(string(raw))
}

func (s *TodoStore) Write(items []TodoItem, merge bool) []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	if !merge {
		for i := range items {
			if items[i].CreatedAt == 0 {
				items[i].CreatedAt = now
			}
			items[i].UpdatedAt = now
		}
		s.items = items
		return s.items
	}

	for _, newItem := range items {
		found := false
		for i, existing := range s.items {
			if existing.ID == newItem.ID {
				s.items[i].Content = newItem.Content
				s.items[i].Status = newItem.Status
				s.items[i].UpdatedAt = now
				found = true
				break
			}
		}
		if !found {
			if newItem.CreatedAt == 0 {
				newItem.CreatedAt = now
			}
			newItem.UpdatedAt = now
			s.items = append(s.items, newItem)
		}
	}

	return s.items
}

func (s *TodoStore) Read() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]TodoItem, len(s.items))
	copy(result, s.items)
	return result
}

func (s *TodoStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = nil
}

func (s *TodoStore) FormatForPrompt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.items) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Active Tasks]\n")

	for _, item := range s.items {
		if item.Status == "completed" || item.Status == "cancelled" {
			continue
		}
		statusIcon := "o"
		if item.Status == "in_progress" {
			statusIcon = "*"
		}
		sb.WriteString(fmt.Sprintf("  %s %s: %s\n", statusIcon, item.ID, item.Content))
	}

	completed := 0
	for _, item := range s.items {
		if item.Status == "completed" {
			completed++
		}
	}
	if completed > 0 {
		sb.WriteString(fmt.Sprintf("  [%d completed]\n", completed))
	}

	return sb.String()
}

func (s *TodoStore) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, item := range s.items {
		if item.Status == "pending" || item.Status == "in_progress" {
			count++
		}
	}
	return count
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStringDefault(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}
