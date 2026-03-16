package tools

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

type noopProvider struct{}

func (n *noopProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "ok"}, nil
}
func (n *noopProvider) GetDefaultModel() string { return "test-model" }
func (n *noopProvider) SupportsTools() bool     { return true }
func (n *noopProvider) GetContextWindow() int   { return 4096 }

type namedTool struct{ name string }

func (n namedTool) Name() string        { return n.name }
func (n namedTool) Description() string { return "test tool" }
func (n namedTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (n namedTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return UserResult("ok")
}

func TestSubagentPolicyDepthLimit(t *testing.T) {
	sm := NewSubagentManager(&noopProvider{}, "test-model", t.TempDir(), nil)
	sm.SetPolicy(SubagentPolicy{
		MaxDepth:          1,
		MaxConcurrency:    2,
		BlockedTools:      []string{"subagent", "spawn"},
		AllowMessageWrite: false,
	})

	ctx := context.WithValue(context.Background(), subagentDepthKey{}, 1)
	_, err := sm.RunSync(ctx, "nested task", "nested", "cli", "direct")
	if err == nil {
		t.Fatalf("expected depth limit error for nested subagent call")
	}
}

func TestSubagentPolicyConcurrencyLimit(t *testing.T) {
	sm := NewSubagentManager(&noopProvider{}, "test-model", t.TempDir(), nil)
	sm.SetPolicy(SubagentPolicy{
		MaxDepth:          2,
		MaxConcurrency:    1,
		BlockedTools:      []string{"subagent", "spawn"},
		AllowMessageWrite: false,
	})

	if err := sm.acquireSlot(); err != nil {
		t.Fatalf("first slot acquire should succeed: %v", err)
	}
	defer sm.releaseSlot()

	if err := sm.acquireSlot(); err == nil {
		t.Fatalf("expected concurrency limit error on second acquire")
	}
}

func TestSubagentPolicyBlockedToolsFilter(t *testing.T) {
	sm := NewSubagentManager(&noopProvider{}, "test-model", t.TempDir(), nil)
	reg := NewToolRegistry()
	reg.Register(namedTool{name: "read_file"})
	reg.Register(namedTool{name: "subagent"})
	reg.Register(namedTool{name: "spawn"})
	reg.Register(namedTool{name: "message"})
	sm.SetTools(reg)
	sm.SetPolicy(DefaultSubagentPolicy)

	filtered := sm.filteredTools()
	if _, ok := filtered.Get("read_file"); !ok {
		t.Fatalf("expected allowed tool read_file to remain")
	}
	if _, ok := filtered.Get("subagent"); ok {
		t.Fatalf("subagent tool must be blocked in subagent context")
	}
	if _, ok := filtered.Get("spawn"); ok {
		t.Fatalf("spawn tool must be blocked in subagent context")
	}
	if _, ok := filtered.Get("message"); ok {
		t.Fatalf("message tool must be blocked when AllowMessageWrite is false")
	}
}
