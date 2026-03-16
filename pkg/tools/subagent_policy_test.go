package tools

import (
	"context"
	"strings"
	"testing"
	"time"

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

type slowProvider struct{}

func (s *slowProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
		return &providers.LLMResponse{Content: "late"}, nil
	}
}
func (s *slowProvider) GetDefaultModel() string { return "test-model" }
func (s *slowProvider) SupportsTools() bool     { return true }
func (s *slowProvider) GetContextWindow() int   { return 4096 }

func TestSubagentTimeoutReleasesConcurrencySlot(t *testing.T) {
	oldTimeout := subagentTimeout
	subagentTimeout = 40 * time.Millisecond
	defer func() { subagentTimeout = oldTimeout }()

	sm := NewSubagentManager(&slowProvider{}, "test-model", t.TempDir(), nil)
	sm.SetPolicy(SubagentPolicy{
		MaxDepth:          1,
		MaxConcurrency:    1,
		BlockedTools:      []string{"subagent", "spawn"},
		AllowMessageWrite: false,
	})

	_, err := sm.RunSync(context.Background(), "slow task", "slow", "cli", "direct")
	if err == nil {
		t.Fatalf("expected timeout error from RunSync")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Fatalf("expected deadline timeout error, got: %v", err)
	}

	if err := sm.acquireSlot(); err != nil {
		t.Fatalf("expected slot to be released after timeout, got: %v", err)
	}
	sm.releaseSlot()
}
