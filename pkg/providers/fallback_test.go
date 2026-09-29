package providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
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

// When every candidate is cooling down, the turn must report why (here:
// out of credit) instead of "no available providers" — and the cause must
// stay classifiable so the retry layer stops retrying a billing failure.
func TestFallbackReportsCooldownCause(t *testing.T) {
	chain := NewFallbackChain(time.Minute)
	cands := []FallbackCandidate{{Name: "deepseek-flash"}}
	billing := fmt.Errorf("API request failed:\n  Status: 402\n  Body:   {\"error\":{\"message\":\"Insufficient Balance\"}}")
	_, err := chain.Execute(context.Background(), cands, func(FallbackCandidate) (*LLMResponse, error) { return nil, billing })
	if err == nil {
		t.Fatal("want failure")
	}
	calls := 0
	_, err = chain.Execute(context.Background(), cands, func(FallbackCandidate) (*LLMResponse, error) { calls++; return nil, billing })
	if calls != 0 {
		t.Fatal("cooling candidate must be skipped")
	}
	if err == nil || !strings.Contains(err.Error(), "Insufficient Balance") {
		t.Fatalf("cooldown must carry the cause, got %v", err)
	}
	if c := provider.ClassifyError(err); c != provider.FailBilling || c.Retryable() {
		t.Fatalf("wrapped cause must classify as non-retryable billing, got %q", c)
	}
}
