package tools

import (
	"context"
	"testing"
)

func TestBatchDelegateToolParameters(t *testing.T) {
	tool := &BatchDelegateTool{}
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["tasks"]; !ok {
		t.Fatal("expected tasks property")
	}
	if _, ok := props["max_workers"]; !ok {
		t.Fatal("expected max_workers property")
	}
}

func TestBatchDelegateMissingTasks(t *testing.T) {
	tool := &BatchDelegateTool{}
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{})
	if !result.IsError {
		t.Fatal("expected error for missing tasks")
	}
}

func TestBatchDelegateEmptyTasks(t *testing.T) {
	tool := &BatchDelegateTool{}
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"tasks": []interface{}{},
	})
	if !result.IsError {
		t.Fatal("expected error for empty tasks")
	}
}

func TestBatchDelegateTooManyTasks(t *testing.T) {
	tool := &BatchDelegateTool{}
	ctx := context.Background()

	tasks := make([]interface{}, 6)
	for i := range tasks {
		tasks[i] = map[string]interface{}{
			"task":  "test task",
			"label": "test",
		}
	}

	result := tool.Execute(ctx, map[string]interface{}{
		"tasks": tasks,
	})
	if !result.IsError {
		t.Fatal("expected error for too many tasks")
	}
}

func TestResultBudgetNew(t *testing.T) {
	budget := NewResultBudget("/tmp")
	if budget.MaxSummaryChars != 24000 {
		t.Fatalf("expected 24000, got %d", budget.MaxSummaryChars)
	}
	if budget.HeadPercent != 75 {
		t.Fatalf("expected 75, got %d", budget.HeadPercent)
	}
	if budget.TailPercent != 25 {
		t.Fatalf("expected 25, got %d", budget.TailPercent)
	}
}

func TestResultBudgetWithinBudget(t *testing.T) {
	budget := NewResultBudget("/tmp")
	if !budget.WithinBudget("short", 1000) {
		t.Fatal("expected short text to be within budget")
	}
	if budget.WithinBudget("this is a longer text that exceeds the budget", 10) {
		t.Fatal("expected long text to exceed budget")
	}
}

func TestResultBudgetTrimResult(t *testing.T) {
	budget := NewResultBudget("/tmp")
	shortText := "short"
	trimmed, spill := budget.TrimResult(shortText, 1000)
	if trimmed != shortText {
		t.Fatalf("expected unchanged text, got %s", trimmed)
	}
	if spill != nil {
		t.Fatal("expected no spill file for short text")
	}
}

func TestResultBudgetSummary(t *testing.T) {
	budget := NewResultBudget("/tmp")
	short := budget.Summary("hello world", 100)
	if short != "hello world" {
		t.Fatalf("expected unchanged, got %s", short)
	}
	long := budget.Summary("this is a long text that should be truncated", 20)
	if len(long) > 30 {
		t.Fatalf("expected truncated, got %d chars", len(long))
	}
}

func TestResultBudgetEstimateTokens(t *testing.T) {
	budget := NewResultBudget("/tmp")
	tokens := budget.EstimateTokens("hello world")
	if tokens != 2 {
		t.Fatalf("expected 2 tokens, got %d", tokens)
	}
}

func TestProgressTrackerNew(t *testing.T) {
	tracker := NewProgressTracker(nil)
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
}

func TestProgressTrackerEmit(t *testing.T) {
	tracker := NewProgressTracker(nil)
	tracker.Start("sub-1", "test task")
	if tracker.Count() != 1 {
		t.Fatalf("expected 1 event, got %d", tracker.Count())
	}
}

func TestProgressTrackerGetRecent(t *testing.T) {
	tracker := NewProgressTracker(nil)
	for i := 0; i < 5; i++ {
		tracker.Start("sub-1", "task")
	}
	recent := tracker.GetRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3 recent events, got %d", len(recent))
	}
}

func TestProgressTrackerClear(t *testing.T) {
	tracker := NewProgressTracker(nil)
	tracker.Start("sub-1", "task")
	tracker.Clear()
	if tracker.Count() != 0 {
		t.Fatal("expected 0 events after clear")
	}
}

func TestProgressTrackerToolCall(t *testing.T) {
	tracker := NewProgressTracker(nil)
	tracker.ToolCall("sub-1", "bash")
	events := tracker.GetRecent(1)
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	if events[0].Type != "tool_call" {
		t.Fatalf("expected tool_call, got %s", events[0].Type)
	}
	if events[0].Detail != "bash" {
		t.Fatalf("expected bash, got %s", events[0].Detail)
	}
}

func TestProgressTrackerComplete(t *testing.T) {
	tracker := NewProgressTracker(nil)
	tracker.Complete("sub-1", "completed", 1.5)
	events := tracker.GetRecent(1)
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	if events[0].Type != "completed" {
		t.Fatalf("expected completed, got %s", events[0].Type)
	}
}

func TestProgressTrackerError(t *testing.T) {
	tracker := NewProgressTracker(nil)
	tracker.Error("sub-1", "something went wrong")
	events := tracker.GetRecent(1)
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	if events[0].Type != "error" {
		t.Fatalf("expected error, got %s", events[0].Type)
	}
}

func TestProgressTrackerEventLimit(t *testing.T) {
	tracker := NewProgressTracker(nil)
	for i := 0; i < 150; i++ {
		tracker.Start("sub-1", "task")
	}
	if tracker.Count() != 100 {
		t.Fatalf("expected 100 events (limit), got %d", tracker.Count())
	}
}
