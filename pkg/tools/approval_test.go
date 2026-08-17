package tools

import (
	"context"
	"testing"
)

func TestApprovalTool_RequestApproval(t *testing.T) {
	tool := NewApprovalTool()
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"action":           "request",
		"description":      "Delete production database",
		"action_to_approve": "DROP TABLE users",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !contains(result.ForLLM, "Approval request created") {
		t.Errorf("unexpected result: %s", result.ForLLM)
	}
}

func TestApprovalTool_DenyRequest(t *testing.T) {
	tool := NewApprovalTool()
	ctx := context.Background()

	// Create request
	tool.Execute(ctx, map[string]interface{}{
		"action":           "request",
		"description":      "Test action",
		"action_to_approve": "test_command",
	})

	// Get pending requests
	pending := tool.GetPendingRequests()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}

	// Deny request
	result := tool.Execute(ctx, map[string]interface{}{
		"action":     "deny",
		"request_id": pending[0].ID,
		"result":     "Not allowed",
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !contains(result.ForLLM, "denied") {
		t.Errorf("unexpected result: %s", result.ForLLM)
	}

	// Check it's no longer pending
	pending = tool.GetPendingRequests()
	if len(pending) != 0 {
		t.Errorf("expected 0 pending requests, got %d", len(pending))
	}
}

func TestApprovalTool_CheckRequest(t *testing.T) {
	tool := NewApprovalTool()
	ctx := context.Background()

	// Create request
	tool.Execute(ctx, map[string]interface{}{
		"action":           "request",
		"description":      "Test action",
		"action_to_approve": "test_command",
	})

	pending := tool.GetPendingRequests()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending request, got %d", len(pending))
	}

	// Check request
	result := tool.Execute(ctx, map[string]interface{}{
		"action":     "check",
		"request_id": pending[0].ID,
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ForLLM)
	}
	if !contains(result.ForLLM, "pending") {
		t.Errorf("expected pending status: %s", result.ForLLM)
	}
}

func TestApprovalTool_CleanupOldRequests(t *testing.T) {
	tool := NewApprovalTool()
	ctx := context.Background()

	// Create request
	tool.Execute(ctx, map[string]interface{}{
		"action":           "request",
		"description":      "Test action",
		"action_to_approve": "test_command",
	})

	// Cleanup with zero duration (all should be cleaned)
	removed := tool.CleanupOldRequests(0)
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
}
