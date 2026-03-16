package tools

import (
	"context"
	"testing"
	"time"
)

type lifecycleTestTool struct{}

func (t lifecycleTestTool) Name() string                       { return "lifecycle_test_tool" }
func (t lifecycleTestTool) Description() string                { return "test tool" }
func (t lifecycleTestTool) Parameters() map[string]interface{} { return map[string]interface{}{} }
func (t lifecycleTestTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return NewToolResult("ok")
}

func TestRegistryHiddenAndPolicy(t *testing.T) {
	reg := NewToolRegistry()
	reg.RegisterHidden(lifecycleTestTool{}, time.Hour)
	if len(reg.List()) != 0 {
		t.Fatalf("hidden tool should not be listed before promotion")
	}
	reg.Promote("lifecycle_test_tool")
	if len(reg.List()) != 1 {
		t.Fatalf("promoted tool should be visible")
	}
	reg.SetToolEnabledForChannel("telegram", "lifecycle_test_tool", false)
	res := reg.ExecuteWithContext(context.Background(), "lifecycle_test_tool", map[string]interface{}{}, "telegram", "chat", "", nil)
	if !res.IsError {
		t.Fatalf("expected disabled tool error")
	}
}
