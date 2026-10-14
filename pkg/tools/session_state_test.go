package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSessionStateReadsEverySourceForTheCallingSession(t *testing.T) {
	tool := NewSessionStateTool()
	var seen []string
	tool.SetSources(
		func(s string) []string { seen = append(seen, s); return []string{"Delete scratch_test.txt"} },
		func(s string) []string { return []string{"Click counter (v2)", "Doi Roast (v1)"} },
		func() []string { return nil },
	)
	res := tool.Execute(WithSessionKey(context.Background(), "main"), nil)
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	for _, want := range []string{"Waiting for the owner's approval (1)", "Delete scratch_test.txt", "Built in this conversation (2)", "Doi Roast (v1)", "Open reminders and routines: none"} {
		if !strings.Contains(res.ForLLM, want) {
			t.Errorf("missing %q in:\n%s", want, res.ForLLM)
		}
	}
	if len(seen) != 1 || seen[0] != "main" {
		t.Fatalf("pending approvals must be read for the calling session, saw %v", seen)
	}
}

func TestSessionStateUnwiredIsAnHonestError(t *testing.T) {
	res := NewSessionStateTool().Execute(context.Background(), nil)
	if !res.IsError {
		t.Fatal("an unwired tool must say so rather than report an empty state")
	}
}
