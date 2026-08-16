package moa

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tools"
)

func TestMoATool_Name(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}
	moa := New(DefaultConfig(), resolver)
	tool := NewMoATool(moa)

	if tool.Name() != "moa" {
		t.Errorf("expected name 'moa', got %s", tool.Name())
	}
}

func TestMoATool_Description(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}
	moa := New(DefaultConfig(), resolver)
	tool := NewMoATool(moa)

	if tool.Description() == "" {
		t.Error("description should not be empty")
	}
}

func TestMoATool_Parameters(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}
	moa := New(DefaultConfig(), resolver)
	tool := NewMoATool(moa)

	params := tool.Parameters()
	if params == nil {
		t.Fatal("parameters should not be nil")
	}

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := props["query"]; !ok {
		t.Error("should have 'query' property")
	}
}

func TestMoATool_Execute_MoADisabled(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}
	moa := New(DefaultConfig(), resolver) // disabled by default
	tool := NewMoATool(moa)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"query": "test",
	})

	if !result.IsError {
		t.Error("expected error when MoA is disabled")
	}
}

func TestMoATool_Execute_EmptyQuery(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}
	config := DefaultConfig()
	config.Enabled = true
	config.Advisors = []AdvisorConfig{{Provider: "a", Model: "m1"}, {Provider: "b", Model: "m2"}}
	moa := New(config, resolver)
	tool := NewMoATool(moa)

	result := tool.Execute(context.Background(), map[string]interface{}{})

	if !result.IsError {
		t.Error("expected error for empty query")
	}
}

func TestMoATool_Execute_Success(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "advice from " + name}, true
	}
	config := DefaultConfig()
	config.Enabled = true
	config.Advisors = []AdvisorConfig{
		{Provider: "p1", Model: "m1", Label: "Advisor1"},
		{Provider: "p2", Model: "m2", Label: "Advisor2"},
	}
	config.AggregatorProvider = "agg"
	config.AggregatorModel = "agg-model"
	moa := New(config, resolver)
	tool := NewMoATool(moa)

	tool.SetMessages([]providers.Message{
		{Role: "user", Content: "What is Go?"},
	})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"query": "What is Go?",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM == "" {
		t.Error("result should not be empty")
	}
}

func TestMoAStatusTool_Execute(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{}, true
	}
	config := DefaultConfig()
	config.Enabled = true
	config.Advisors = []AdvisorConfig{
		{Provider: "p1", Model: "m1"},
		{Provider: "p2", Model: "m2"},
	}
	config.AggregatorProvider = "agg"
	config.AggregatorModel = "agg-model"
	moa := New(config, resolver)
	statusTool := NewMoAStatusTool(moa)

	result := statusTool.Execute(context.Background(), map[string]interface{}{})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM == "" {
		t.Error("status result should not be empty")
	}
	// Should contain "enabled"
	if !contains(result.ForLLM, "true") {
		t.Error("status should contain enabled=true")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Verify MoATool implements tools.Tool
var _ tools.Tool = (*MoATool)(nil)
var _ tools.Tool = (*MoAStatusTool)(nil)
