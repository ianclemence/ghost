package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/logger"
)

// TimeoutTool lets a tool declare its own time budget. Tools that can hang
// (network, exec, subprocess) should implement this with a bounded duration so
// a stuck call can't stall the whole turn.
type TimeoutTool interface {
	Timeout() time.Duration
}

// RetryableTool lets a read-only, idempotent tool opt in to bounded retries.
// Tools that cause side effects must NOT implement this — retrying a write
// could duplicate an action. A tool returns (0, 0) to effectively disable retry
// for a given call.
type RetryableTool interface {
	RetryPolicy() (maxRetries int, wait time.Duration)
}

// defaultToolTimeout is a generous safety net so a genuinely hung tool never
// blocks a turn forever. It sits above the shorter per-tool timeouts.
const defaultToolTimeout = 5 * time.Minute

// executeWithReliability runs a tool under a per-attempt timeout and applies a
// bounded, opt-in retry policy for idempotent tools. It surfaces distinct
// outcomes (success / error / timed out) and never silently retries by default.
func executeWithReliability(ctx context.Context, tool Tool, args map[string]interface{}) *ToolResult {
	timeout := defaultToolTimeout
	if tt, ok := tool.(TimeoutTool); ok {
		if d := tt.Timeout(); d > 0 {
			timeout = d
		}
	}

	maxRetries, wait := 0, time.Duration(0)
	if rt, ok := tool.(RetryableTool); ok {
		maxRetries, wait = rt.RetryPolicy()
	}
	if maxRetries < 0 {
		maxRetries = 0
	}

	var result *ToolResult
	var lastErr error
	for attempt := 0; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		result = tool.Execute(attemptCtx, args)
		if attemptCtx.Err() != nil {
			// The attempt was cancelled/timed out; prefer that as the failure so
			// a stale result isn't reported as success.
			result = &ToolResult{ForLLM: "tool timed out", IsError: true, TimedOut: true, Err: attemptCtx.Err()}
		}
		cancel()

		if result == nil {
			result = ErrorResult(fmt.Sprintf("tool %s returned a nil result", tool.Name())).WithError(fmt.Errorf("nil result"))
		}
		if result.Err != nil {
			result.IsError = true
			lastErr = result.Err
		}
		if errors.Is(lastErr, context.DeadlineExceeded) {
			result.TimedOut = true
		}
		if result.TimedOut {
			result.IsError = true
		}

		if !result.IsError || attempt >= maxRetries {
			break
		}
		logger.WarnCF("tool", "tool failed, retrying",
			map[string]interface{}{"tool": tool.Name(), "attempt": attempt + 1, "max_retries": maxRetries, "wait_ms": wait.Milliseconds()})
		select {
		case <-ctx.Done():
			return result
		case <-time.After(wait):
		}
	}

	// Make the failure actionable for the model and the user.
	if result.TimedOut {
		result.ForLLM = fmt.Sprintf("tool %q timed out after %s", tool.Name(), timeout)
	}
	return result
}
