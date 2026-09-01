package tools

import (
	"context"
	"testing"
)

func TestTodoToolName(t *testing.T) {
	tool := NewTodoTool()
	if tool.Name() != "todo" {
		t.Fatalf("expected name 'todo', got %s", tool.Name())
	}
}

func TestTodoToolDescription(t *testing.T) {
	tool := NewTodoTool()
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestTodoToolParameters(t *testing.T) {
	tool := NewTodoTool()
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["action"]; !ok {
		t.Fatal("expected action property")
	}
	if _, ok := props["items"]; !ok {
		t.Fatal("expected items property")
	}
}

func TestTodoWrite(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Implement feature X",
				"status":  "pending",
			},
			map[string]interface{}{
				"id":      "task-2",
				"content": "Write tests",
				"status":  "in_progress",
			},
		},
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestTodoRead(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Test task",
				"status":  "pending",
			},
		},
	})

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "read",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestTodoClear(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Test task",
				"status":  "pending",
			},
		},
	})

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "clear",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	readResult := tool.Execute(ctx, map[string]interface{}{
		"action": "read",
	})
	if readResult.IsError {
		t.Fatalf("unexpected error: %s", readResult.ForLLM)
	}
}

func TestTodoMerge(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Original content",
				"status":  "pending",
			},
		},
	})

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"merge":  true,
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Updated content",
				"status":  "completed",
			},
		},
	})

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "read",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestTodoReplace(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Old task",
				"status":  "pending",
			},
		},
	})

	tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"merge":  false,
		"items": []interface{}{
			map[string]interface{}{
				"id":      "task-2",
				"content": "New task",
				"status":  "pending",
			},
		},
	})

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "read",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}

func TestTodoWriteInvalidItems(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items": []interface{}{
			map[string]interface{}{
				"id": "",
			},
		},
	})

	if !result.IsError {
		t.Fatal("expected error for invalid items")
	}
}

func TestTodoWriteEmptyItems(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "write",
		"items":  []interface{}{},
	})

	if !result.IsError {
		t.Fatal("expected error for empty items")
	}
}

func TestTodoInvalidAction(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"action": "invalid",
	})

	if !result.IsError {
		t.Fatal("expected error for invalid action")
	}
}

func TestTodoMissingAction(t *testing.T) {
	tool := NewTodoTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{})

	if !result.IsError {
		t.Fatal("expected error for missing action")
	}
}

func TestTodoStoreFormatForPrompt(t *testing.T) {
	store := &TodoStore{}
	store.Write([]TodoItem{
		{ID: "t1", Content: "Task 1", Status: "pending"},
		{ID: "t2", Content: "Task 2", Status: "in_progress"},
		{ID: "t3", Content: "Task 3", Status: "completed"},
	}, false)

	prompt := store.FormatForPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
}

func TestTodoStoreFormatForPromptEmpty(t *testing.T) {
	store := &TodoStore{}
	prompt := store.FormatForPrompt()
	if prompt != "" {
		t.Fatal("expected empty prompt for empty store")
	}
}

func TestTodoStoreActiveCount(t *testing.T) {
	store := &TodoStore{}
	store.Write([]TodoItem{
		{ID: "t1", Content: "Task 1", Status: "pending"},
		{ID: "t2", Content: "Task 2", Status: "in_progress"},
		{ID: "t3", Content: "Task 3", Status: "completed"},
	}, false)

	count := store.ActiveCount()
	if count != 2 {
		t.Fatalf("expected 2 active items, got %d", count)
	}
}
