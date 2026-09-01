package providers

import (
	"context"
	"errors"
	"testing"
)

func TestFallbackTriesNextOnEmptyResponse(t *testing.T) {
	fc := NewFallbackChain(0)
	failed := &stubProvider{} // empty response → triggers fallback
	working := &stubProvider{reply: "ok-from-backup"}
	candidates := []FallbackCandidate{
		{Name: "prime", Provider: failed, Model: "prime"},
		{Name: "backup", Provider: working, Model: "backup"},
	}

	var ran []string
	run := func(c FallbackCandidate) (*LLMResponse, error) {
		ran = append(ran, c.Name)
		return c.Provider.Chat(context.Background(), nil, nil, c.Model, map[string]interface{}{})
	}

	resp, err := fc.Execute(context.Background(), candidates, run)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Content != "ok-from-backup" {
		t.Fatalf("expected fallback response, got %q", resp.Content)
	}
	if len(ran) != 2 || ran[0] != "prime" || ran[1] != "backup" {
		t.Fatalf("expected both candidates tried in order, got %v", ran)
	}
}

func TestFallbackErrorOnlyFallsThroughOnErrorOrEmpty(t *testing.T) {
	fc := NewFallbackChain(0)
	// Prime errors; backup returns content → success.
	_, err := fc.Execute(context.Background(), []FallbackCandidate{
		{Name: "a", Provider: &stubProvider{err: errors.New("boom")}, Model: "a"},
		{Name: "b", Provider: &stubProvider{reply: "b"}, Model: "b"},
	}, func(c FallbackCandidate) (*LLMResponse, error) {
		return c.Provider.Chat(context.Background(), nil, nil, c.Model, nil)
	})
	if err != nil {
		t.Fatalf("expected success via backup, got %v", err)
	}

	// All empty → error surfaced, no silent empty reply.
	_, err = fc.Execute(context.Background(), []FallbackCandidate{
		{Name: "a", Provider: &stubProvider{}, Model: "a"},
		{Name: "b", Provider: &stubProvider{}, Model: "b"},
	}, func(c FallbackCandidate) (*LLMResponse, error) {
		return c.Provider.Chat(context.Background(), nil, nil, c.Model, nil)
	})
	if err == nil {
		t.Fatal("expected error when every candidate returns empty")
	}
}

type stubProvider struct {
	reply string
	err   error
}

func (s *stubProvider) GetDefaultModel() string { return "stub" }

func (s *stubProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, opts map[string]interface{}) (*LLMResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.reply == "" {
		return &LLMResponse{Content: "", FinishReason: "stop"}, nil
	}
	return &LLMResponse{Content: s.reply, FinishReason: "stop"}, nil
}
