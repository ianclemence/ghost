package providers

import (
	"testing"
)

func TestEstimateCost(t *testing.T) {
	cost, ok := EstimateCost("deepseek", "deepseek-flash", 1_000_000, 1_000_000)
	if !ok || cost <= 0 {
		t.Fatalf("priced model must estimate positive: %v %v", cost, ok)
	}
	if _, ok := EstimateCost("unknown-provider", "mystery-9", 100, 100); ok {
		t.Fatal("unpriced provider must stay unknown, never zero")
	}
	// Local inference: $0 known (no marginal API cost), not unknown.
	cost, ok = EstimateCost("ollama", "qwen3:0.6b", 1000, 500)
	if !ok || cost != 0 {
		t.Fatalf("ollama must be $0 known: %v %v", cost, ok)
	}
}

func TestCostForTurn(t *testing.T) {
	// Measured totals win outright.
	if cost, unknown := CostForTurn("deepseek", "x", 100, 100, 0, 0, 0.05, true); unknown || cost != 0.05 {
		t.Fatalf("measured must win: %v %v", cost, unknown)
	}
	// Incomplete measurement falls back to estimates.
	if cost, unknown := CostForTurn("deepseek", "deepseek-flash", 1_000_000, 0, 0, 0, 0.01, false); unknown || cost <= 0 {
		t.Fatalf("must estimate: %v %v", cost, unknown)
	}
	// Unpriced stays unknown.
	if _, unknown := CostForTurn("mystery", "m9", 100, 100, 0, 0, 0, false); !unknown {
		t.Fatal("unpriced must stay unknown")
	}
}

// A warm prompt cache is priced at the cache-read rate, not as fresh input.
func TestEstimateCostCachedAnthropic(t *testing.T) {
	// 1M prompt tokens, 900k of them cache reads, on Opus 5.5 ($4 in).
	got, ok := EstimateCostCached("anthropic", "claude-opus-5-5", 1_000_000, 0, 900_000, 0)
	want := 0.1*4.00 + 0.9*4.00*0.10
	if !ok || got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("got %v want %v", got, want)
	}
	// No cache tokens: identical to the plain estimate.
	plain, _ := EstimateCost("anthropic", "claude-opus-5-5", 1000, 500)
	cached, _ := EstimateCostCached("anthropic", "claude-opus-5-5", 1000, 500, 0, 0)
	if plain != cached {
		t.Fatalf("plain %v != cached %v", plain, cached)
	}
}
