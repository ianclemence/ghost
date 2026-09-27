package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// The budget is model-aware: a large-window model gets a larger input limit
// than the conservative default, and output is always reserved.
func TestContextBudgetForIsModelAware(t *testing.T) {
	big := ContextBudgetFor("deepseek")
	if big.ModelContext < 100000 {
		t.Fatalf("deepseek context window not reflected: %+v", big)
	}
	if big.InputLimit() <= 0 || big.InputLimit() >= big.ModelContext {
		t.Fatalf("output must be reserved: %+v", big)
	}
	unknown := ContextBudgetFor("totally-unknown-model")
	if unknown.ModelContext != 8192 {
		t.Fatalf("unknown model must fall back to 8192, got %+v", unknown)
	}
	if unknown.InputLimit() <= 0 {
		t.Fatalf("fallback budget must still allow input: %+v", unknown)
	}
}

// Context reduction must preserve the behavioural core (permission and
// evidence rules) and the current request, while dropping history.
func TestFitContextPreservesConstraintsAndCurrentRequest(t *testing.T) {
	core := providers.Message{Role: "system", Content: strings.Repeat(
		"Permission rules: never bypass the broker. Evidence rules: no success claim without evidence. ", 40)}
	msgs := []providers.Message{core}
	for i := 0; i < 60; i++ {
		msgs = append(msgs, providers.Message{Role: "assistant", Content: strings.Repeat("old history detail ", 60)})
	}
	current := providers.Message{Role: "user", Content: "Send the invite for tomorrow 2pm and confirm delivery."}
	msgs = append(msgs, current)

	total := 0
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	budget := total / 3 // force real reduction
	got := FitContext(msgs, budget)

	if len(got) >= len(msgs) {
		t.Fatalf("expected reduction, got %d of %d", len(got), len(msgs))
	}
	if got[0].Content != core.Content {
		t.Fatal("behavioural core (permission/evidence rules) must survive reduction")
	}
	if got[len(got)-1].Content != current.Content {
		t.Fatal("current request must survive reduction")
	}
	after := 0
	for _, m := range got {
		after += estimateMessageTokens(m)
	}
	if after > budget+estimateMessageTokens(got[0]) {
		t.Fatalf("reduced context still over budget: %d > %d", after, budget)
	}
	// Constraints are still present verbatim after reduction.
	if !strings.Contains(got[0].Content, "Permission rules") || !strings.Contains(got[0].Content, "Evidence rules") {
		t.Fatal("permission/evidence rules must remain in the request")
	}
}

// A context already inside budget is returned untouched.
func TestFitContextNoopWithinBudget(t *testing.T) {
	msgs := []providers.Message{
		{Role: "system", Content: "core"},
		{Role: "user", Content: "hi"},
	}
	got := FitContext(msgs, 100000)
	if len(got) != len(msgs) {
		t.Fatalf("in-budget context must be untouched, got %d", len(got))
	}
}
