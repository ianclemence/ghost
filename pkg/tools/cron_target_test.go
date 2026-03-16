package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/cron"
)

type countingExecutor struct {
	calls int
}

func (c *countingExecutor) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error) {
	c.calls++
	return "ok", nil
}

func TestCronExecuteJobTargetRouting(t *testing.T) {
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	exec := &countingExecutor{}
	tool := NewCronTool(cs, exec, nil, t.TempDir())

	localJob := &cron.CronJob{
		ID: "j-local",
		Payload: cron.CronPayload{
			Message: "local message",
			Target:  "local",
			Deliver: false,
			Channel: "cli",
			To:      "direct",
		},
	}
	if _, err := tool.ExecuteJob(context.Background(), localJob); err != nil {
		t.Fatalf("local target should execute: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected 1 execution for local target, got %d", exec.calls)
	}

	originMismatch := &cron.CronJob{
		ID: "j-origin-skip",
		Payload: cron.CronPayload{
			Message:  "origin message",
			Target:   "origin",
			OriginID: "other-instance",
			Deliver:  false,
			Channel:  "cli",
			To:       "direct",
		},
	}
	if _, err := tool.ExecuteJob(context.Background(), originMismatch); err != nil {
		t.Fatalf("origin mismatch should be skipped without error: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected no additional execution for origin mismatch, got %d", exec.calls)
	}

	originMatch := &cron.CronJob{
		ID: "j-origin-run",
		Payload: cron.CronPayload{
			Message:  "origin match",
			Target:   "origin",
			OriginID: tool.instanceID,
			Deliver:  false,
			Channel:  "cli",
			To:       "direct",
		},
	}
	if _, err := tool.ExecuteJob(context.Background(), originMatch); err != nil {
		t.Fatalf("origin match should execute: %v", err)
	}
	if exec.calls != 2 {
		t.Fatalf("expected second execution for origin match, got %d", exec.calls)
	}
}

func TestCronExecuteJobInvalidTarget(t *testing.T) {
	cs := cron.NewCronService(filepath.Join(t.TempDir(), "jobs.json"), nil)
	exec := &countingExecutor{}
	tool := NewCronTool(cs, exec, nil, t.TempDir())

	job := &cron.CronJob{
		ID: "j-invalid",
		Payload: cron.CronPayload{
			Message: "invalid target",
			Target:  "invalid",
			Deliver: false,
		},
	}

	if _, err := tool.ExecuteJob(context.Background(), job); err == nil {
		t.Fatalf("expected error for invalid target")
	}
	if exec.calls != 0 {
		t.Fatalf("expected no execution for invalid target, got %d", exec.calls)
	}
}
