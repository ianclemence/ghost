package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cron"
)

func commandJob() *cron.CronJob {
	return &cron.CronJob{
		ID: "j-cmd",
		Payload: cron.CronPayload{
			Command: "echo hello",
			Channel: "cli",
			To:      "direct",
		},
	}
}

// A scheduled command with no authorization boundary must not run.
func TestCronCommandRefusedWithoutAuthorizer(t *testing.T) {
	cs := cron.NewCronService(t.TempDir()+"/jobs.json", nil, nil)
	tool := NewCronTool(cs, &countingExecutor{}, nil, t.TempDir())
	status, err := tool.ExecuteJob(context.Background(), commandJob())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "blocked" {
		t.Fatalf("command without an authorizer must be blocked, got %q", status)
	}
}

// A refusal from the authorization boundary blocks execution.
func TestCronCommandRefusedByAuthorizer(t *testing.T) {
	cs := cron.NewCronService(t.TempDir()+"/jobs.json", nil, nil)
	tool := NewCronTool(cs, &countingExecutor{}, nil, t.TempDir())
	tool.SetCommandAuthorizer(func(ctx context.Context, command string) (context.Context, error) {
		return ctx, errors.New("denied by policy")
	})
	status, err := tool.ExecuteJob(context.Background(), commandJob())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "blocked" {
		t.Fatalf("authorizer refusal must block, got %q", status)
	}
}

// An authorizer that grants returns a context carrying the exec grant, and
// the command runs.
func TestCronCommandRunsWhenAuthorized(t *testing.T) {
	cs := cron.NewCronService(t.TempDir()+"/jobs.json", nil, nil)
	tool := NewCronTool(cs, &countingExecutor{}, nil, t.TempDir())
	reg := NewToolRegistry()
	reg.Register(&stubGrantTool{name: "exec"})
	tool.SetRegistry(reg)
	authorized := false
	tool.SetCommandAuthorizer(func(ctx context.Context, command string) (context.Context, error) {
		authorized = true
		if !strings.Contains(command, "echo hello") {
			t.Errorf("authorizer must receive the command, got %q", command)
		}
		return GrantExec(ctx, "exec"), nil
	})
	status, err := tool.ExecuteJob(context.Background(), commandJob())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !authorized {
		t.Fatal("authorizer must be consulted")
	}
	if status != "ok" {
		t.Fatalf("authorized command must run, got %q", status)
	}
}
