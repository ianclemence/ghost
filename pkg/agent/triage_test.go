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

	if ans, ok := al.fastPathAnswer("what is my name"); !ok || !strings.Contains(ans, "Sam") {
		t.Fatalf("name fast path: got (%q, %v), want Sam", ans, ok)
	}
	if ans, ok := al.fastPathAnswer("where do i live"); !ok || !strings.Contains(ans, "Bangkok") {
		t.Fatalf("location fast path: got (%q, %v), want Bangkok", ans, ok)
	}
}

func TestFastPathAnswerHonestWhenAbsent(t *testing.T) {
	ws := t.TempDir()
	store, err := personalcontext.Open(ws)
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	al := &AgentLoop{pcStore: store}
	ans, ok := al.fastPathAnswer("what is my name")
	if !ok {
		t.Fatalf("expected fast path to handle a name question even with no data")
	}
	if !strings.Contains(strings.ToLower(ans), "don") {
		t.Fatalf("expected an honest 'I don't have that' answer, got %q", ans)
	}
	if strings.Contains(strings.ToLower(ans), "sam") {
		t.Fatalf("must not fabricate a name, got %q", ans)
	}
}

func TestFastPathAnswerNotHandledForNonFast(t *testing.T) {
	ws := t.TempDir()
	store, _ := personalcontext.Open(ws)
	al := &AgentLoop{pcStore: store}
	if _, ok := al.fastPathAnswer("what is the weather in Bangkok"); ok {
		t.Fatal("must not hand non-self-fact requests to the fast path")
	}
	if _, ok := al.fastPathAnswer("hello there"); ok {
		t.Fatal("must not handle arbitrary messages via the fast path")
	}
}
