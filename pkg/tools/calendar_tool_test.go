package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCalendarToolOperations(t *testing.T) {
	tool := NewCalendarTool(t.TempDir())
	var lastArgs []string
	tool.run = func(ctx context.Context, args []string) (string, error) {
		lastArgs = args
		return "ok-output", nil
	}

	// list: read-only, no evidence required.
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if res.IsError {
		t.Fatalf("list failed: %s", res.ForLLM)
	}
	if len(res.Evidence) != 0 {
		t.Fatalf("read must not require evidence, got %v", res.Evidence)
	}
	if !strings.Contains(strings.Join(lastArgs, " "), "agenda") {
		t.Fatalf("list must invoke agenda, got %v", lastArgs)
	}

	// create: consequential, evidence required.
	res = tool.Execute(context.Background(), map[string]interface{}{"action": "create", "title": "Dentist", "when": "Friday 15:00"})
	if res.IsError {
		t.Fatalf("create failed: %s", res.ForLLM)
	}
	if res.Evidence["type"] != "acknowledgement" || res.Evidence["operation"] != "create" {
		t.Fatalf("create must carry acknowledgement evidence, got %v", res.Evidence)
	}

	// delete: evidence required.
	res = tool.Execute(context.Background(), map[string]interface{}{"action": "delete", "query": "Dentist"})
	if res.IsError {
		t.Fatalf("delete failed: %s", res.ForLLM)
	}
	if res.Evidence["operation"] != "delete" {
		t.Fatalf("delete must carry evidence, got %v", res.Evidence)
	}
}

func TestCalendarToolHonestFailure(t *testing.T) {
	tool := NewCalendarTool(t.TempDir())
	tool.run = func(ctx context.Context, args []string) (string, error) {
		return "", errors.New("invalid_grant")
	}
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if !res.IsError {
		t.Fatal("provider failure must surface as an error, not success")
	}
	if strings.Contains(res.ForLLM, "invalid_grant") {
		t.Fatalf("raw provider error must not leak: %q", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Calendar isn't connected") {
		t.Fatalf("must be honest product language, got %q", res.ForLLM)
	}
}
