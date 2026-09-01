package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// stubTool is a minimal controllable tool for reliability tests.
type stubTool struct {
	name      string
	callCount atomic.Int32
	// behaviour per call
	errsBefore int32         // number of initial calls that return an error
	hang       time.Duration // if > 0, sleep ignoring context (simulates a stuck tool)
	retryable  bool
	timeout    time.Duration
}

func (s *stubTool) Name() string { return s.name }
func (s *stubTool) Description() string {
	return "stub"
}
func (s *stubTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}

func (s *stubTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	n := s.callCount.Add(1)
	if s.hang > 0 {
		time.Sleep(s.hang) // deliberately ignores ctx cancellation
	}
	if n <= s.errsBefore {
		return ErrorResult("boom").WithError(errors.New("boom"))
	}
	return NewToolResult("ok")
}

func (s *stubTool) Timeout() time.Duration { return s.timeout }
func (s *stubTool) RetryPolicy() (int, time.Duration) {
	if !s.retryable {
		return 0, 0
	}
	return 1, time.Millisecond
}

func TestExecuteReliabilityTimeout(t *testing.T) {
	// A tool that hangs past its short timeout must return TimedOut+IsError.
	tool := &stubTool{name: "hang", hang: time.Second, timeout: 50 * time.Millisecond}
	res := executeWithReliability(context.Background(), tool, nil)
	if !res.IsError || !res.TimedOut {
		t.Fatalf("expected timed-out error result, got IsError=%v TimedOut=%v", res.IsError, res.TimedOut)
	}
}

func TestExecuteReliabilityRetriesIdempotent(t *testing.T) {
	// A read-only/retryable tool that fails once then succeeds → allowed to
	// retry (2 attempts) and end successful.
	tool := &stubTool{name: "search", errsBefore: 1, retryable: true, timeout: time.Second}
	res := executeWithReliability(context.Background(), tool, nil)
	if res.IsError {
		t.Fatalf("expected success after retry, got error: %s", res.ForLLM)
	}
	if got := tool.callCount.Load(); got != 2 {
		t.Fatalf("expected 2 attempts after one retry, got %d", got)
	}
}

func TestExecuteReliabilityNoRetryByDefault(t *testing.T) {
	// A failing tool that is not RetryableTool must NOT be retried.
	tool := &stubTool{name: "write", errsBefore: 100, timeout: time.Second}
	res := executeWithReliability(context.Background(), tool, nil)
	if !res.IsError {
		t.Fatalf("expected error")
	}
	if got := tool.callCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry for non-idempotent), got %d", got)
	}
}

func TestExecuteReliabilityGivesUpAfterMaxRetries(t *testing.T) {
	tool := &stubTool{name: "search", errsBefore: 100, retryable: true, timeout: time.Second}
	res := executeWithReliability(context.Background(), tool, nil)
	if !res.IsError {
		t.Fatalf("expected error after exhausting retries")
	}
	if got := tool.callCount.Load(); got != 2 {
		t.Fatalf("expected 2 attempts (1 retry), got %d", got)
	}
}

func TestExecuteReliabilityNilResult(t *testing.T) {
	tool := &nilTool{}
	res := executeWithReliability(context.Background(), tool, nil)
	if !res.IsError {
		t.Fatalf("expected error for nil result")
	}
}

type nilTool struct {
	stubTool
}

func (s *nilTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return nil
}
