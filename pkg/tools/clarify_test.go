package tools

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
)

func TestClarifyToolName(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	if tool.Name() != "clarify" {
		t.Fatalf("expected name 'clarify', got %s", tool.Name())
	}
}

func TestClarifyToolDescription(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestClarifyToolParameters(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["question"]; !ok {
		t.Fatal("expected question property")
	}
	if _, ok := props["choices"]; !ok {
		t.Fatal("expected choices property")
	}
}

func TestClarifyMissingQuestion(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{})
	if !result.IsError {
		t.Fatal("expected error for missing question")
	}
}

func TestClarifyEmptyQuestion(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"question": "",
	})
	if !result.IsError {
		t.Fatal("expected error for empty question")
	}
}

func TestClarifyHandleResponse(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)

	questionID := "test-123"
	tool.mu.Lock()
	tool.pending[questionID] = make(chan string, 1)
	tool.mu.Unlock()

	go func() {
		time.Sleep(10 * time.Millisecond)
		tool.HandleResponse(questionID, "my answer")
	}()

	select {
	case response := <-tool.pending[questionID]:
		if response != "my answer" {
			t.Fatalf("expected 'my answer', got %s", response)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestClarifyHandleResponseUnknownID(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)

	ok := tool.HandleResponse("unknown-id", "answer")
	if ok {
		t.Fatal("expected false for unknown question ID")
	}
}

func TestClarifyPendingCount(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)

	if tool.PendingCount() != 0 {
		t.Fatal("expected 0 pending questions")
	}

	tool.mu.Lock()
	tool.pending["q1"] = make(chan string, 1)
	tool.pending["q2"] = make(chan string, 1)
	tool.mu.Unlock()

	if tool.PendingCount() != 2 {
		t.Fatalf("expected 2 pending questions, got %d", tool.PendingCount())
	}
}

func TestClarifyWithChoices(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	ctx := context.Background()

	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	go func() {
		time.Sleep(10 * time.Millisecond)
		tool.HandleResponse("q-1", "Option A")
	}()

	result := tool.Execute(ctx, map[string]interface{}{
		"question": "Which option?",
		"choices": []interface{}{
			"Option A",
			"Option B",
			"Option C",
		},
	})

	if !result.IsError {
		t.Log("Result:", result.ForLLM)
	}
}

func TestClarifyTimeout(t *testing.T) {
	b := bus.NewMessageBus()
	tool := NewClarifyTool(b)
	tool.timeout = 50 * time.Millisecond

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]interface{}{
		"question": "This will timeout",
	})

	if !result.IsError {
		t.Fatal("expected timeout error")
	}
}
