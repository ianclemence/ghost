package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHookManager_BasicOperations(t *testing.T) {
	hm := NewHookManager()

	if hm.HasApprovers() {
		t.Error("Expected no approvers initially")
	}

	// Register an approver
	approver := ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	})
	hm.RegisterToolApprover("test-approver", 10, approver)

	if !hm.HasApprovers() {
		t.Error("Expected approvers after registration")
	}

	names := hm.ListApprovers()
	if len(names) != 1 || names[0] != "test-approver" {
		t.Errorf("Expected [test-approver], got %v", names)
	}

	// Remove it
	hm.RemoveToolApprover("test-approver")
	if hm.HasApprovers() {
		t.Error("Expected no approvers after removal")
	}
}

func TestHookManager_PriorityOrdering(t *testing.T) {
	hm := NewHookManager()

	var order []string

	hm.RegisterToolApprover("low-priority", 100, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		order = append(order, "low")
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	hm.RegisterToolApprover("high-priority", 1, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		order = append(order, "high")
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	hm.RegisterToolApprover("mid-priority", 50, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		order = append(order, "mid")
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	req := ToolApprovalRequest{
		ToolName:  "test_tool",
		Args:      map[string]interface{}{},
		Timestamp: time.Now(),
	}

	hm.CheckToolApproval(context.Background(), req)

	expected := "high,mid,low"
	got := strings.Join(order, ",")
	if got != expected {
		t.Errorf("Expected priority order %s, got %s", expected, got)
	}
}

func TestHookManager_FirstDenyStops(t *testing.T) {
	hm := NewHookManager()

	var evaluated []string

	hm.RegisterToolApprover("approver-1", 1, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		evaluated = append(evaluated, "approver-1")
		return ToolApprovalDecision{Action: ToolApprovalDeny, Reason: "blocked"}
	}))

	hm.RegisterToolApprover("approver-2", 2, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		evaluated = append(evaluated, "approver-2")
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	req := ToolApprovalRequest{
		ToolName:  "dangerous_tool",
		Args:      map[string]interface{}{},
		Timestamp: time.Now(),
	}

	decision := hm.CheckToolApproval(context.Background(), req)

	if decision.Action != ToolApprovalDeny {
		t.Errorf("Expected deny, got %s", decision.Action)
	}

	if len(evaluated) != 1 {
		t.Errorf("Expected only first approver to be evaluated, got %v", evaluated)
	}
}

func TestHookManager_AllAllow(t *testing.T) {
	hm := NewHookManager()

	hm.RegisterToolApprover("a", 1, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	hm.RegisterToolApprover("b", 2, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		return ToolApprovalDecision{Action: ToolApprovalAllow}
	}))

	req := ToolApprovalRequest{
		ToolName:  "safe_tool",
		Args:      map[string]interface{}{},
		Timestamp: time.Now(),
	}

	decision := hm.CheckToolApproval(context.Background(), req)
	if decision.Action != ToolApprovalAllow {
		t.Errorf("Expected allow, got %s", decision.Action)
	}
}

func TestDenyPatternToolApprover(t *testing.T) {
	approver := NewDenyPatternToolApprover([]string{"rm -rf", "drop table", "sudo"})

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		expected ToolApprovalAction
	}{
		{
			name:     "safe command allowed",
			toolName: "exec",
			args:     map[string]interface{}{"command": "ls -la"},
			expected: ToolApprovalAllow,
		},
		{
			name:     "rm -rf denied",
			toolName: "exec",
			args:     map[string]interface{}{"command": "rm -rf /tmp/test"},
			expected: ToolApprovalDeny,
		},
		{
			name:     "drop table denied",
			toolName: "exec",
			args:     map[string]interface{}{"command": "drop table users"},
			expected: ToolApprovalDeny,
		},
		{
			name:     "sudo denied",
			toolName: "exec",
			args:     map[string]interface{}{"command": "sudo apt install something"},
			expected: ToolApprovalDeny,
		},
		{
			name:     "read file allowed",
			toolName: "read_file",
			args:     map[string]interface{}{"path": "/tmp/file.txt"},
			expected: ToolApprovalAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ToolApprovalRequest{
				ToolName:  tt.toolName,
				Args:      tt.args,
				Timestamp: time.Now(),
			}
			decision := approver.ApproveTool(context.Background(), req)
			if decision.Action != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, decision.Action)
			}
		})
	}
}

func TestToolDenyList(t *testing.T) {
	denyList := NewToolDenyList(map[string]string{
		"exec":       "shell execution not allowed",
		"write_file": "file writes disabled",
	})

	tests := []struct {
		name     string
		toolName string
		expected ToolApprovalAction
	}{
		{"exec denied", "exec", ToolApprovalDeny},
		{"write_file denied", "write_file", ToolApprovalDeny},
		{"read_file allowed", "read_file", ToolApprovalAllow},
		{"web_search allowed", "web_search", ToolApprovalAllow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ToolApprovalRequest{
				ToolName:  tt.toolName,
				Args:      map[string]interface{}{},
				Timestamp: time.Now(),
			}
			decision := denyList.ApproveTool(context.Background(), req)
			if decision.Action != tt.expected {
				t.Errorf("Expected %s, got %s (reason: %s)", tt.expected, decision.Action, decision.Reason)
			}
		})
	}
}

func TestAuditLogToolApprover(t *testing.T) {
	auditor := NewAuditLogToolApprover()

	// Should always allow
	req := ToolApprovalRequest{
		ToolName:   "exec",
		Args:       map[string]interface{}{"command": "ls"},
		Channel:    "telegram",
		ChatID:     "12345",
		SessionKey: "telegram:12345",
		Timestamp:  time.Now(),
	}

	decision := auditor.ApproveTool(context.Background(), req)
	if decision.Action != ToolApprovalAllow {
		t.Errorf("Expected allow, got %s", decision.Action)
	}

	if auditor.Count() != 1 {
		t.Errorf("Expected count 1, got %d", auditor.Count())
	}

	logs := auditor.GetLogs()
	if len(logs) != 1 {
		t.Errorf("Expected 1 log entry, got %d", len(logs))
	}

	if logs[0].ToolName != "exec" {
		t.Errorf("Expected tool name 'exec', got '%s'", logs[0].ToolName)
	}
}

func TestAuditLogToolApprover_MultipleEntries(t *testing.T) {
	auditor := NewAuditLogToolApprover()

	for i := 0; i < 5; i++ {
		req := ToolApprovalRequest{
			ToolName:  "tool_" + string(rune('a'+i)),
			Args:      map[string]interface{}{},
			Timestamp: time.Now(),
		}
		auditor.ApproveTool(context.Background(), req)
	}

	if auditor.Count() != 5 {
		t.Errorf("Expected count 5, got %d", auditor.Count())
	}

	logs := auditor.GetLogs()
	if len(logs) != 5 {
		t.Errorf("Expected 5 log entries, got %d", len(logs))
	}
}

func TestHookManager_ReplaceDuplicateName(t *testing.T) {
	hm := NewHookManager()

	hm.RegisterToolApprover("same-name", 1, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		return ToolApprovalDecision{Action: ToolApprovalDeny, Reason: "first"}
	}))

	hm.RegisterToolApprover("same-name", 2, ToolApproverFunc(func(ctx context.Context, req ToolApprovalRequest) ToolApprovalDecision {
		return ToolApprovalDecision{Action: ToolApprovalAllow, Reason: "second"}
	}))

	names := hm.ListApprovers()
	if len(names) != 1 {
		t.Errorf("Expected 1 approver, got %d", len(names))
	}

	req := ToolApprovalRequest{
		ToolName:  "test",
		Args:      map[string]interface{}{},
		Timestamp: time.Now(),
	}

	decision := hm.CheckToolApproval(context.Background(), req)
	if decision.Action != ToolApprovalAllow {
		t.Errorf("Expected allow from replacement, got %s", decision.Action)
	}
}

func TestToolApprovalAction_String(t *testing.T) {
	if ToolApprovalAllow.String() != "allow" {
		t.Errorf("Expected 'allow', got '%s'", ToolApprovalAllow.String())
	}
	if ToolApprovalDeny.String() != "deny" {
		t.Errorf("Expected 'deny', got '%s'", ToolApprovalDeny.String())
	}
	if ToolApprovalAction(99).String() != "unknown" {
		t.Errorf("Expected 'unknown', got '%s'", ToolApprovalAction(99).String())
	}
}
