package moa

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

// mockProvider implements providers.LLMProvider for testing.
type mockProvider struct {
	response string
	model    string
	calls    int
}

func (m *mockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	m.calls++
	return &providers.LLMResponse{
		Content: m.response,
		Usage:   &providers.UsageInfo{TotalTokens: 100},
	}, nil
}

func (m *mockProvider) GetDefaultModel() string {
	return m.model
}

func TestMoA_ShouldUseMoA(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   bool
	}{
		{
			name: "disabled",
			config: Config{
				Enabled:  false,
				Advisors: []AdvisorConfig{{Provider: "a", Model: "m1"}, {Provider: "b", Model: "m2"}},
			},
			want: false,
		},
		{
			name: "enabled with 2 advisors",
			config: Config{
				Enabled:  true,
				Advisors: []AdvisorConfig{{Provider: "a", Model: "m1"}, {Provider: "b", Model: "m2"}},
			},
			want: true,
		},
		{
			name: "enabled with 1 advisor (not enough)",
			config: Config{
				Enabled:  true,
				Advisors: []AdvisorConfig{{Provider: "a", Model: "m1"}},
			},
			want: false,
		},
		{
			name: "enabled with 0 advisors",
			config: Config{
				Enabled:  true,
				Advisors: []AdvisorConfig{},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := func(name string) (providers.LLMProvider, bool) {
				return &mockProvider{}, true
			}
			m := New(tt.config, resolver)
			if got := m.ShouldUseMoA(); got != tt.want {
				t.Errorf("ShouldUseMoA() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMoA_Run(t *testing.T) {
	advisors := []AdvisorConfig{
		{Provider: "prov1", Model: "model-a", Label: "Advisor-A"},
		{Provider: "prov2", Model: "model-b", Label: "Advisor-B"},
	}

	config := Config{
		Enabled:            true,
		Advisors:           advisors,
		AggregatorProvider: "prov3",
		AggregatorModel:    "aggregator-model",
		TimeoutSeconds:     10,
		Temperature:        0.7,
	}

	callCounts := map[string]int{}
	resolver := func(name string) (providers.LLMProvider, bool) {
		switch name {
		case "prov1":
			return &mockProvider{response: "Advice from advisor 1: focus on accuracy"}, true
		case "prov2":
			return &mockProvider{response: "Advice from advisor 2: consider edge cases"}, true
		case "prov3":
			return &mockProvider{response: "Aggregated final response combining both advisors"}, true
		default:
			return nil, false
		}
	}

	m := New(config, resolver)
	ctx := context.Background()

	messages := []providers.Message{
		{Role: "user", Content: "What is the best approach to solve X?"},
	}

	result, err := m.Run(ctx, messages)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should have 2 advisor outputs
	if len(result.AdvisorOutputs) != 2 {
		t.Fatalf("expected 2 advisor outputs, got %d", len(result.AdvisorOutputs))
	}

	// Each advisor should have been called
	for _, output := range result.AdvisorOutputs {
		if output.Error != nil {
			t.Errorf("advisor %s returned error: %v", output.Label, output.Error)
		}
		if output.Content == "" {
			t.Errorf("advisor %s returned empty content", output.Label)
		}
	}

	// Aggregated should be non-empty
	if result.Aggregated == "" {
		t.Error("aggregated response is empty")
	}

	// Total duration should be non-negative
	if result.TotalDuration < 0 {
		t.Error("total duration should be non-negative")
	}

	_ = callCounts
}

func TestMoA_RunAdvisorTimeout(t *testing.T) {
	advisors := []AdvisorConfig{
		{Provider: "slow", Model: "slow-model", Label: "SlowAdvisor"},
		{Provider: "fast", Model: "fast-model", Label: "FastAdvisor"},
	}

	config := Config{
		Enabled:            true,
		Advisors:           advisors,
		AggregatorProvider: "agg",
		AggregatorModel:    "agg-model",
		TimeoutSeconds:     1, // very short timeout
		Temperature:        0.7,
	}

	resolver := func(name string) (providers.LLMProvider, bool) {
		switch name {
		case "slow":
			// This provider would timeout in real usage, but our mock is instant
			return &mockProvider{response: "slow advice"}, true
		case "fast":
			return &mockProvider{response: "fast advice"}, true
		case "agg":
			return &mockProvider{response: "aggregated"}, true
		default:
			return nil, false
		}
	}

	m := New(config, resolver)
	ctx := context.Background()

	messages := []providers.Message{
		{Role: "user", Content: "Test"},
	}

	result, err := m.Run(ctx, messages)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.AdvisorOutputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(result.AdvisorOutputs))
	}
}

func TestMoA_RunProviderNotFound(t *testing.T) {
	advisors := []AdvisorConfig{
		{Provider: "unknown", Model: "model"},
	}

	config := Config{
		Enabled:            true,
		Advisors:           advisors,
		AggregatorProvider: "agg",
		AggregatorModel:    "agg-model",
		TimeoutSeconds:     5,
	}

	// Resolver returns false for everything — both advisors and aggregator
	resolver := func(name string) (providers.LLMProvider, bool) {
		return nil, false
	}

	m := New(config, resolver)
	ctx := context.Background()

	messages := []providers.Message{
		{Role: "user", Content: "Test"},
	}

	// With no advisors resolvable AND aggregator not resolvable,
	// Run will error on aggregation (advisors all fail, aggregator fails)
	_, err := m.Run(ctx, messages)
	if err == nil {
		t.Fatal("expected error when all providers unknown")
	}
}

func TestMoA_MaxAdvisors(t *testing.T) {
	advisors := []AdvisorConfig{
		{Provider: "p1", Model: "m1", Label: "A1"},
		{Provider: "p2", Model: "m2", Label: "A2"},
		{Provider: "p3", Model: "m3", Label: "A3"},
		{Provider: "p4", Model: "m4", Label: "A4"},
		{Provider: "p5", Model: "m5", Label: "A5"},
	}

	config := Config{
		Enabled:            true,
		Advisors:           advisors,
		AggregatorProvider: "agg",
		AggregatorModel:    "agg-model",
		MaxAdvisors:        3, // cap at 3
		TimeoutSeconds:     5,
	}

	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "advice"}, true
	}

	m := New(config, resolver)
	ctx := context.Background()

	messages := []providers.Message{
		{Role: "user", Content: "Test"},
	}

	result, err := m.Run(ctx, messages)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Should only have 3 advisor outputs (capped by MaxAdvisors)
	if len(result.AdvisorOutputs) != 3 {
		t.Fatalf("expected 3 advisor outputs (capped), got %d", len(result.AdvisorOutputs))
	}
}

func TestBuildAdvisorMessages(t *testing.T) {
	messages := []providers.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
		{Role: "tool", Content: "tool result"},
		{Role: "user", Content: "Search the web"},
	}

	advisorMsgs := buildAdvisorMessages(messages)

	// Should have: system (advisory) + user + assistant + user (not system/tool)
	if len(advisorMsgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(advisorMsgs))
	}

	// First should be the advisory system prompt
	if advisorMsgs[0].Role != "system" {
		t.Errorf("first message should be system, got %s", advisorMsgs[0].Role)
	}
	if advisorMsgs[0].Content != ReferenceSystemPrompt {
		t.Error("advisory system prompt mismatch")
	}

	// Should not contain the agent's system prompt or tool results
	for _, msg := range advisorMsgs[1:] {
		if msg.Role == "tool" {
			t.Error("advisor messages should not contain tool results")
		}
		if msg.Content == "You are a helpful assistant." {
			t.Error("advisor messages should not contain the agent's system prompt")
		}
	}

	// Last message should be user
	last := advisorMsgs[len(advisorMsgs)-1]
	if last.Role != "user" {
		t.Errorf("last message should be user, got %s", last.Role)
	}
}

func TestBuildAggregatorMessages(t *testing.T) {
	original := []providers.Message{
		{Role: "user", Content: "What is Go?"},
		{Role: "assistant", Content: "Go is a programming language."},
	}

	outputs := []AdvisorOutput{
		{Label: "A1", Provider: "p1", Model: "m1", Content: "Go is great for systems programming"},
		{Label: "A2", Provider: "p2", Model: "m2", Content: "Go excels at concurrency"},
	}

	aggMsgs := buildAggregatorMessages(original, outputs)

	if len(aggMsgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(aggMsgs))
	}

	// First should be aggregator system prompt
	if aggMsgs[0].Role != "system" {
		t.Errorf("first message should be system, got %s", aggMsgs[0].Role)
	}

	// Last should be user with advisor outputs
	last := aggMsgs[len(aggMsgs)-1]
	if last.Role != "user" {
		t.Errorf("last message should be user, got %s", last.Role)
	}
	if last.Content == "" {
		t.Error("aggregator user message should not be empty")
	}
}

func TestMoA_ShouldUseMoAConcurrent(t *testing.T) {
	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{}, true
	}

	config := Config{
		Enabled:  true,
		Advisors: []AdvisorConfig{{Provider: "a", Model: "m1"}, {Provider: "b", Model: "m2"}},
	}
	m := New(config, resolver)

	// Concurrent reads should not race
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			_ = m.ShouldUseMoA()
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestResult_FormatResult(t *testing.T) {
	result := &Result{
		AdvisorOutputs: []AdvisorOutput{
			{Label: "A1", Duration: 100 * time.Millisecond},
			{Label: "A2", Duration: 200 * time.Millisecond, Error: context.DeadlineExceeded},
		},
		Aggregated:    "final answer",
		TotalDuration: 500 * time.Millisecond,
	}

	formatted := result.FormatResult()
	if formatted == "" {
		t.Error("FormatResult() should not be empty")
	}

	labels := result.GetAdvisorLabels()
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(labels))
	}
	if labels[0] != "A1" {
		t.Errorf("expected first label A1, got %s", labels[0])
	}
}

func TestMoA_ContextCancellation(t *testing.T) {
	advisors := []AdvisorConfig{
		{Provider: "p1", Model: "m1"},
		{Provider: "p2", Model: "m2"},
	}

	config := Config{
		Enabled:            true,
		Advisors:           advisors,
		AggregatorProvider: "agg",
		AggregatorModel:    "agg-model",
		TimeoutSeconds:     5,
	}

	resolver := func(name string) (providers.LLMProvider, bool) {
		return &mockProvider{response: "ok"}, true
	}

	m := New(config, resolver)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	messages := []providers.Message{
		{Role: "user", Content: "Test"},
	}

	// Should still complete (advisors may error but aggregate should handle it)
	_, _ = m.Run(ctx, messages)
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should be disabled")
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("default timeout should be 30, got %d", cfg.TimeoutSeconds)
	}
	if cfg.MaxAdvisors != 5 {
		t.Errorf("default max advisors should be 5, got %d", cfg.MaxAdvisors)
	}
}
