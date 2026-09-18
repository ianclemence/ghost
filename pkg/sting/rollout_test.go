package sting

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRewardAsymmetry(t *testing.T) {
	if Reward(VerdictRouterFault) != -1 {
		t.Fatal("fabrication must be -1")
	}
	if Reward(VerdictEscalated) != 0 {
		t.Fatal("abstention must be 0")
	}
	if Reward(VerdictRoutedOK) != 1 || Reward(VerdictWorldFailed) != 1 {
		t.Fatal("good routing must be +1 even when the world fails")
	}
}

func TestRecorderNilSafe(t *testing.T) {
	var r *Recorder
	r.Begin("q", []string{"a"}) // must not panic
	r.Propose(nil, Decision{Escalate: true})
	r.Acted(FunctionCall{Name: "a"}, true, false)
	r.Flush()
}

func TestRecorderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollouts.jsonl")
	r := NewRecorder(path, "base")
	if r == nil {
		t.Fatal("expected recorder")
	}
	r.Begin("search for x", []string{"web_search"})
	conf := 0.9
	r.Propose(&CompleteResponse{Type: "call", Confidence: &conf,
		FunctionCalls: []FunctionCall{{Name: "web_search"}}},
		Decision{Act: []FunctionCall{{Name: "web_search"}}, Reason: "ok"})
	r.Acted(FunctionCall{Name: "web_search"}, true, false)
	r.Flush()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var roll Rollout
	if err := json.Unmarshal(raw[:len(raw)-1], &roll); err != nil {
		t.Fatal(err)
	}
	if roll.Query != "search for x" || len(roll.Acted) != 1 {
		t.Fatalf("unexpected %+v", roll)
	}
	if roll.Acted[0].Verdict != VerdictRoutedOK || roll.Acted[0].Reward != 1 {
		t.Fatalf("unexpected verdict %+v", roll.Acted[0])
	}
}

func TestAggregateRollouts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollouts.jsonl")
	rows := []Rollout{
		{Scope: ScopePositiveSingle, Acted: []ActedCall{{Verdict: VerdictRoutedOK}}},
		{Scope: ScopePositiveSingle, Acted: []ActedCall{{Verdict: VerdictWorldFailed}}},
		{Scope: ScopePositiveSingle, Acted: []ActedCall{{Verdict: VerdictRouterFault}}},
		// Parallel with one fault is incorrect for the whole turn.
		{Scope: ScopeParallel, Acted: []ActedCall{{Verdict: VerdictRoutedOK}, {Verdict: VerdictRouterFault}}},
		// No scope or no executed call carries no routing evidence.
		{Scope: "", Acted: []ActedCall{{Verdict: VerdictRoutedOK}}},
		{Scope: ScopePositiveSingle, Acted: nil},
	}
	var buf bytes.Buffer
	for i, r := range rows {
		if i == 3 {
			buf.WriteString("{not json\n") // a corrupt line must be skipped
		}
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(nil)
	n, err := AggregateRollouts(tr, path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("expected 4 graded turns, got %d", n)
	}
	if p, cnt := tr.Precision(ScopePositiveSingle); cnt != 3 || p != 2.0/3.0 {
		t.Fatalf("positive-single got %.3f/%d", p, cnt)
	}
	if p, cnt := tr.Precision(ScopeParallel); cnt != 1 || p != 0 {
		t.Fatalf("parallel got %.3f/%d", p, cnt)
	}
}

func TestRecorderRotatesPastCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollouts.jsonl")
	if err := os.WriteFile(path, make([]byte, MaxRolloutBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(path, "base")
	r.Begin("q", nil)
	r.Propose(nil, Decision{Escalate: true, Reason: "no-call"})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > MaxRolloutBytes {
		t.Fatalf("log not rotated: %d bytes", st.Size())
	}
}
