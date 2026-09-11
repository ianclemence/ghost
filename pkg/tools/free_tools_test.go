package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func providersTC(name string) providers.ToolCall {
	return providers.ToolCall{ID: "tc-1", Name: name, Arguments: map[string]interface{}{}}
}

func TestFreeConsequentialTable(t *testing.T) {
	for _, want := range []string{"message", "exec", "sandbox", "i2c", "spi", "hass", "schedule", "update", "image_generate"} {
		ft, ok := FreeToolCapability(want)
		if !ok || ft.Capability == "" || ft.Risk == "" {
			t.Fatalf("%s must map to a capability + risk", want)
		}
	}
	for _, not := range []string{"read_file", "context_get", "remember", "memory_recall", "web_fetch", "list_dir"} {
		if IsFreeConsequentialTool(not) {
			t.Fatalf("%s must not be a standalone-consequential tool", not)
		}
	}
	// Unknown/forged names never map onto a capability.
	for _, forged := range []string{"exec --x", "shell", "run_command", "browser_transact", "device_write"} {
		if _, ok := FreeToolCapability(forged); ok {
			t.Fatalf("forged name %q must not map to a capability", forged)
		}
	}
}

type recordedTool struct{}

func (recordedTool) Name() string        { return "exec" }
func (recordedTool) Description() string { return "exec" }
func (recordedTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (recordedTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return &ToolResult{ForLLM: "ran " + SessionKeyFromContext(ctx), ForUser: "ran"}
}

// Without a governing hook a subagent standalone consequential call is
// refused outright.
func TestSubagentConsequentialRefusedWithoutAuth(t *testing.T) {
	res := executeSubagentConsequential(context.Background(), ToolLoopConfig{}, providersTC("exec"), "", "")
	if res == nil || !res.IsError || !strings.Contains(res.ForLLM, "not authorized") {
		t.Fatalf("must refuse without auth: %+v", res)
	}
}

// A governing hook replaces execution with its result (approval wait/deny).
func TestSubagentConsequentialAuthReplacement(t *testing.T) {
	cfg := ToolLoopConfig{ConsequentialAuth: func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult {
		return &ToolResult{ForLLM: "approval required for " + tool}
	}}
	res := executeSubagentConsequential(context.Background(), cfg, providersTC("exec"), "", "")
	if res == nil || res.IsError || res.ForLLM != "approval required for exec" {
		t.Fatalf("hook result must replace execution: %+v", res)
	}
}

// When the hook allows (returns nil), the tool executes and receives the
// subagent session binding.
func TestSubagentConsequentialAuthAllowsRun(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(recordedTool{})
	cfg := ToolLoopConfig{
		Tools:             reg,
		ConsequentialAuth: func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult { return nil },
	}
	ctx := WithSessionKey(context.Background(), "sub-sess-1")
	res := executeSubagentConsequential(ctx, cfg, providersTC("exec"), "test", "chat")
	if res == nil || res.IsError || !strings.Contains(res.ForLLM, "sub-sess-1") {
		t.Fatalf("allowed run must execute with session: %+v", res)
	}
}

// MCP tools are dynamic third-party code and must resolve to a governed
// capability identity with high_impact risk (broker bypass regression).
func TestMCPToolsAreGoverned(t *testing.T) {
	// Unknown/unmapped MCP tools are governed as high-impact mcp.execute.
	for _, name := range []string{"mcp_filesystem_read", "mcp_x", "mcp_calendar_delete"} {
		if !IsFreeConsequentialTool(name) {
			t.Errorf("%s must be a governed free consequential tool", name)
		}
		ft, ok := FreeToolCapability(name)
		if !ok || ft.Capability != "mcp.execute" || ft.Risk != RiskHighImpact {
			t.Errorf("%s must map to mcp.execute/high_impact, got %+v ok=%v", name, ft, ok)
		}
	}
	// A well-known read-oriented MCP tool maps to its semantic capability.
	ft, ok := FreeToolCapability("mcp_github_search")
	if !ok || ft.Capability != "repository.search" {
		t.Errorf("known MCP tool must map to repository.search, got %+v ok=%v", ft, ok)
	}
	if IsFreeConsequentialTool("mcp") {
		t.Error("bare 'mcp' is not a tool")
	}
}
