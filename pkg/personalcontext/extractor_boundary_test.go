package personalcontext

import (
	"encoding/json"
	"testing"
	"time"
)

// Maps the boundary of the deterministic extractor: which plainly stated
// durable facts the fast path captures, and which it does not. The point is
// not to cover one example but to record where the line is, so the semantic
// extractor's role is explicit.
//
// The fast path is a closed, high-precision grammar. Everything it does not
// capture is the semantic extractor's job (which needs a model, so it is not
// exercised here).
func TestExtractorBoundary_DurableFacts(t *testing.T) {
	now := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	captured := map[string]string{
		"my favorite color is teal":            "preference/favorite_color",
		"my name is Ian":                       "identity/name",
		"i live in Bangkok":                    "fact/location",
		"my favorite food is sushi":            "preference/favorite_food",
		"i work as a designer":                 "fact/work",
		"my favorite football team is Chelsea": "preference/favorite_team",
	}
	for text, wantPredicate := range captured {
		acts, err := Extract(Input{SessionID: "s", MessageID: "m", Text: text, Timestamp: now})
		if err != nil {
			t.Fatalf("extract %q: %v", text, err)
		}
		found := false
		for _, a := range acts {
			if a.Entry.Predicate == wantPredicate {
				found = true
			}
		}
		if !found {
			t.Errorf("deterministic grammar no longer captures %q (want %s); if this is deliberate, move it to the semantic-only list", text, wantPredicate)
		}
	}

	// Recorded as NOT captured by the fast path: the semantic extractor's
	// job. These assertions pin the boundary so a future widening is a
	// deliberate decision, not an accident — and they must be updated, with
	// the reason, if a pattern is added.
	uncovered := []string{
		"my dentist is Dr. Somchai",
		"I go to Dr. Somchai for my teeth",
		"Dr. Somchai is my dentist",
	}
	for _, text := range uncovered {
		acts, err := Extract(Input{SessionID: "s", MessageID: "m", Text: text, Timestamp: now})
		if err != nil {
			t.Fatalf("extract %q: %v", text, err)
		}
		if len(acts) != 0 {
			t.Errorf("%q is now captured by the fast path (%d actions); update this boundary test and its rationale", text, len(acts))
		}
	}
}

// UNMET by design: open-ended durable statements ("my dentist is X") rely on
// the semantic extractor, which requires a model. Proving capture here needs a
// real model wired in; a stub that passes would be the bug the gap is about.
func TestExtractorBoundary_OpenEndedNeedsTheModel(t *testing.T) {
	t.Skip("open-ended durable facts rely on the semantic extractor, which needs a real model; not runnable in CI")
}

func rawString(t *testing.T, e Entry) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(e.Value, &s); err != nil {
		return string(e.Value)
	}
	return s
}
