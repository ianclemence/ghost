package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
)

func TestClassifyEffort(t *testing.T) {
	cases := []struct {
		msg  string
		want Effort
	}{
		// Clear self-fact recall → fast.
		{"what is my name", EffortFast},
		{"What's my name?", EffortFast},
		{"who am i", EffortFast},
		{"where do i live", EffortFast},
		{"where am i", EffortFast},
		{"what is my email", EffortFast},
		{"what is my phone number", EffortFast},
		// "my favorite" / "what's my favorite" must NOT be fast — these can be
		// declarations ("my favorite color is blue") and the digest system uses
		// them to probe the LLM path. Keeping them fast would skip extraction
		// and test intra-loop digest injection.
		{"my favorite color is blue", EffortDeliberate},
		{"what's my favorite color?", EffortDeliberate},
		// Anything not clearly fast stays at the default (deliberate) — never
		// starve a real request.
		{"find me a chicken pasta recipe", EffortDeliberate},
		{"add milk and eggs to my shopping list", EffortDeliberate},
		{"what is the weather in Bangkok", EffortDeliberate},
		{"what do i prefer to drink", EffortFast},
		{"what do i like", EffortFast},
		{"", EffortUnknown},
	}
	for _, c := range cases {
		if got := classifyEffort(c.msg); got != c.want {
			t.Errorf("classifyEffort(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

func TestEffortString(t *testing.T) {
	if EffortFast.String() != "fast" || EffortUnknown.String() != "unknown" || EffortDeliberate.String() != "deliberate" {
		t.Fatalf("unexpected effort strings")
	}
}

func TestFastPathAnswerFromMemory(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	now := time.Now().UTC()
	in := func(text, ref string) personalcontext.Input {
		return personalcontext.Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now}
	}
	if _, err := personalcontext.Apply(store, in("my name is Sam", "m1")); err != nil {
		t.Fatalf("apply name: %v", err)
	}
	if _, err := personalcontext.Apply(store, in("I live in Bangkok", "m2")); err != nil {
		t.Fatalf("apply location: %v", err)
	}
	al := &AgentLoop{pcStore: store}

	if ans, ok := al.fastPathAnswer("what is my name", ""); !ok || !strings.Contains(ans, "Sam") {
		t.Fatalf("name fast path: got (%q, %v), want Sam", ans, ok)
	}
	if ans, ok := al.fastPathAnswer("where do i live", ""); !ok || !strings.Contains(ans, "Bangkok") {
		t.Fatalf("location fast path: got (%q, %v), want Bangkok", ans, ok)
	}
}

func TestFastPathPreferenceRecall(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	raw, _ := personalcontext.RawValue("green tea")
	_, err = al.pcStore.Create(personalcontext.Entry{
		ID: "fastpref", Kind: personalcontext.KindPreference,
		Subject: "user", Predicate: "preference/prefers", Value: raw,
		Status: personalcontext.StatusCurrent,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand,
			Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ans, ok := al.fastPathAnswer("what do i prefer to drink", "")
	if !ok || !strings.Contains(ans, "green tea") {
		t.Fatalf("preference recall must be local+deterministic, got ok=%v ans=%q", ok, ans)
	}
}

func TestFastPathFallsThroughWhenNarrowLookupMisses(t *testing.T) {
	// A narrow exact-predicate lookup is not proof of absence (the
	// extractor's vocabulary is wider than any allowlist, and other sinks
	// are invisible here). On a miss the fast path must NOT claim "not
	// stored" — it falls through so the full loop retrieves semantically.
	// Regression guard for cor-02, where "coffee is my favourite drink" was
	// stored yet the fast path answered "I don't have that stored yet".
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	al := &AgentLoop{pcStore: store}
	if ans, ok := al.fastPathAnswer("what is my name", ""); ok {
		t.Fatalf("must fall through on empty store, got %q", ans)
	}
}

func TestFastPathNeverClaimsAbsenceForLikingFamily(t *testing.T) {
	// cor-02 shape: belief stored under a liking-family predicate the old
	// allowlist missed must either be answered or fall through — never met
	// with a false "not stored".
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatal(err)
	}
	al := &AgentLoop{pcStore: store}
	raw, _ := personalcontext.RawValue("Coffee is now their favourite drink")
	_, err = al.pcStore.Create(personalcontext.Entry{
		ID: "liking", Kind: personalcontext.KindPreference,
		Subject: "user", Predicate: "preference/favorite_drink", Value: raw,
		Status: personalcontext.StatusCurrent,
		Sources: []personalcontext.Source{{Type: personalcontext.SourceCommand,
			Kind: personalcontext.SourceUserDeclared, Ref: "t:1", Timestamp: time.Now().UTC()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ans, ok := al.fastPathAnswer("what do i like to drink", "")
	if !ok || !strings.Contains(ans, "Coffee") {
		t.Fatalf("liking-family belief must be answered fast, got ok=%v ans=%q", ok, ans)
	}
}

func TestFastPathAnswerNotHandledForNonFast(t *testing.T) {
	ws := t.TempDir()
	store, _ := personalcontext.Open(ws)
	al := &AgentLoop{pcStore: store}
	if _, ok := al.fastPathAnswer("what is the weather in Bangkok", ""); ok {
		t.Fatal("must not hand non-self-fact requests to the fast path")
	}
	if _, ok := al.fastPathAnswer("hello there", ""); ok {
		t.Fatal("must not handle arbitrary messages via the fast path")
	}
}
