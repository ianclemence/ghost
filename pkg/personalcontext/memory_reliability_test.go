package personalcontext

import (
	"context"
	"errors"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// The phrases that historically depended on the LLM classifier (and were
// intermittently lost to parse_error) must now be captured by the
// deterministic grammar — no model involved.
func TestReliabilityDeterministicGrammar(t *testing.T) {
	cases := []struct {
		text      string
		predicate string
		value     string
	}{
		{"remember that my sister's name is Ana.", "relationship/family", "Ana"},
		{"My brother's name is Sam", "relationship/family", "Sam"},
		{"remember that my mother's name is Grace", "relationship/family", "Grace"},
		{"I prefer the colour teal", "preference/favorite_color", "teal"},
		{"remember that I prefer the color blue", "preference/favorite_color", "blue"},
		{"remember that I prefer green tea.", "preference/prefers", "green tea"},
		{"remember that my favourite colour is periwinkle", "preference/favorite_color", "periwinkle"},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			actions, err := Extract(Input{Text: tc.text, Current: nil})
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			for _, a := range actions {
				if a.Entry.Predicate == tc.predicate && actionValue(t, a) == tc.value {
					return
				}
			}
			t.Fatalf("no deterministic action %s=%q in %+v", tc.predicate, tc.value, actions)
		})
	}
}

// Repeated extraction of the same fact reinforces; it never creates a
// duplicate current entry.
func TestReliabilityDedupRestatement(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := func(text string) Input {
		return Input{Text: text, Current: store.Current()}
	}
	if _, err := Apply(store, in("remember that my sister's name is Ana.")); err != nil {
		t.Fatal(err)
	}
	second, err := Apply(store, in("remember that my sister's name is Ana."))
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range second {
		if a.Mode == ActionCreate {
			t.Fatalf("restatement must not create a duplicate, got create: %+v", a)
		}
	}
	n := 0
	for _, e := range store.Current() {
		if e.Predicate == "relationship/family" && e.Status == StatusCurrent {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly one current family entry, got %d", n)
	}
}

// A correction of the same fact supersedes the old value.
func TestReliabilityCorrectionUpdates(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := func(text string) Input { return Input{Text: text, Current: store.Current()} }
	if _, err := Apply(store, in("remember that my sister's name is Ana.")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(store, in("Actually, my sister's name is Bea.")); err != nil {
		t.Fatal(err)
	}
	got := store.Current()
	if len(got) != 1 || entryValueString(got[0]) != "Bea" {
		t.Fatalf("current = %+v, want only Bea", got)
	}
}

// Malformed model output is a clean parse error, never a crash and never a
// silently accepted value.
func TestReliabilityParseRejectsMalformed(t *testing.T) {
	bad := []string{
		"", "not json at all", "{}", `{"kind":"fact"}`,
		`{"should_remember": true, "kind": "fact"`, // truncated
		`{"should_remember": true, "kind": fact}`,  // unquoted
	}
	for _, s := range bad {
		if _, err := ParseClassificationOutput(s); err == nil {
			t.Fatalf("must reject %q", s)
		}
	}
}

// Tolerated but still strict: valid JSON embedded in fences/prose parses,
// while a JSON document missing its required field still fails.
func TestReliabilityParseToleratesSurroundings(t *testing.T) {
	good := []string{
		`{"should_remember": false, "kind": "fact", "domain": "other", "confidence": 0.5}`,
		"Here you go:\n```json\n{\"should_remember\": true, \"kind\": \"preference\", \"domain\": \"food\", \"confidence\": 0.9}\n```",
		"Sure!\n{\"should_remember\": true, \"kind\": \"fact\", \"domain\": \"work\", \"confidence\": 0.8}\nHope that helps.",
		`Some prose {"should_remember": false, "kind": "fact", "domain": "other", "confidence": 0.4} trailing`,
		`{"should_remember": true, "kind": "goal", "domain": "other", "confidence": 0.6, "summary": "wants {a} literal {brace} here"}`,
	}
	for _, s := range good {
		if _, err := ParseClassificationOutput(s); err != nil {
			t.Fatalf("must accept %q: %v", s, err)
		}
	}
	if _, err := ParseClassificationOutput(`{"kind":"fact","domain":"other","confidence":0.5}`); err == nil {
		t.Fatal("missing should_remember must fail")
	}
}

type queuedProvider struct {
	contents []string
	calls    int
}

func (q *queuedProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	if q.calls >= len(q.contents) {
		return &providers.LLMResponse{Content: "{}"}, nil
	}
	c := q.contents[q.calls]
	q.calls++
	return &providers.LLMResponse{Content: c}, nil
}
func (q *queuedProvider) GetDefaultModel() string { return "test" }

// Retry is bounded and deterministic: a malformed first response is retried
// once; a valid second response is used.
func TestReliabilityLLMRetryRecovers(t *testing.T) {
	q := &queuedProvider{contents: []string{
		"this is not json",
		`{"should_remember": true, "kind": "fact", "domain": "lifestyle", "confidence": 0.9}`,
	}}
	se := NewSemanticExtractor(q, "test")
	// Deliberately a message the deterministic grammar does not capture, so
	// the semantic path runs.
	res := se.Extract(context.Background(), "Every weekend I volunteer at the animal shelter.", nil)
	if !res.ShouldRemember || len(res.Entries) != 1 {
		t.Fatalf("expected recovered extraction, got %+v", res)
	}
	if q.calls != 2 {
		t.Fatalf("expected exactly one retry (2 calls), got %d", q.calls)
	}
	if res.Reason != "semantic_extraction" {
		t.Fatalf("reason = %q", res.Reason)
	}
}

// Two malformed responses exhaust the bounded retries and report a clean,
// observable parse_error — never a crash, never a fabricated memory.
func TestReliabilityLLMRetryExhaustedIsObservable(t *testing.T) {
	q := &queuedProvider{contents: []string{"not json", "still not json"}}
	se := NewSemanticExtractor(q, "test")
	res := se.Extract(context.Background(), "Every weekend I volunteer at the animal shelter.", nil)
	if res.ShouldRemember {
		t.Fatal("must not claim a memory on persistent parse failure")
	}
	if res.Reason != "parse_error" {
		t.Fatalf("reason = %q, want parse_error", res.Reason)
	}
	if q.calls != 2 {
		t.Fatalf("retries must be bounded at 2 calls, got %d", q.calls)
	}
}

// Provider outage is distinct from a parse error.
func TestReliabilityLLMUnavailable(t *testing.T) {
	se := NewSemanticExtractor(failProvider{}, "test")
	res := se.Extract(context.Background(), "Every weekend I volunteer at the animal shelter.", nil)
	if res.ShouldRemember || res.Reason != "llm_unavailable" {
		t.Fatalf("got %+v, want llm_unavailable", res)
	}
}

var errBoom = errors.New("boom")

type failProvider struct{}

func (failProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return nil, errBoom
}
func (failProvider) GetDefaultModel() string { return "test" }
