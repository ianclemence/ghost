package workflow

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseStrictRejectsUnknownFields(t *testing.T) {
	raw := `{"name":"x","version":"1","root":{"id":"a","type":"escalate","reason":"r","bogus":1}}`
	if _, err := ParseStrict([]byte(raw)); err == nil {
		t.Fatal("expected unknown-field error")
	}
}

func TestValidateBounds(t *testing.T) {
	w := &Workflow{Name: "n", Version: "v1", Root: Node{ID: "l", Type: NodeLoop, Body: &Node{ID: "b", Type: NodeEscalate, Reason: "r"}, Until: "done", MaxIter: 21}}
	if err := Validate(w); err == nil {
		t.Fatal("expected max_iter bound error")
	}
	w.Root.MaxIter = 3
	if err := Validate(w); err != nil {
		t.Fatalf("valid loop rejected: %v", err)
	}
}

func TestDigestStable(t *testing.T) {
	w := &Workflow{Name: "inbox", Version: "1", Root: Node{ID: "e", Type: NodeEscalate, Reason: "r"}}
	if Digest(w) != Digest(w) {
		t.Fatal("digest unstable")
	}
}

func TestRunnerChainJudgeCode(t *testing.T) {
	w := &Workflow{Name: "t", Version: "1", Root: Node{ID: "c", Type: NodeChain, Steps: []Node{
		{ID: "j", Type: NodeJudge, Question: &Question{ID: "could_stop", Kind: QNoul, Instructions: "needs action?"}},
		{ID: "g", Type: NodeCode, Gate: "is_yes"},
	}}}
	r := &Runner{Backend: RuleBackend{}, Gates: map[string]Gate{
		"is_yes": func(s map[string]interface{}) bool {
			return s["judge_could_stop"] == "yes" || s["judge_could_stop"] == 0.96
		},
	}, NoulThreshold: 0.5}
	state := map[string]interface{}{"subject": "Invoice due Friday payment deadline"}
	final, esc, err := r.Run(context.Background(), w, state)
	if err != nil || esc {
		t.Fatalf("run failed esc=%v err=%v", esc, err)
	}
	if _, ok := final["judge_could_stop"]; !ok {
		t.Fatal("judge answer missing")
	}
}

func TestRunnerFailClosedUnknownGate(t *testing.T) {
	w := &Workflow{Name: "t", Version: "1", Root: Node{ID: "g", Type: NodeCode, Gate: "nope"}}
	r := &Runner{Backend: RuleBackend{}}
	_, esc, _ := r.Run(context.Background(), w, map[string]interface{}{})
	if !esc {
		t.Fatal("unknown gate must escalate")
	}
}

func TestSiftKeepsStoppers(t *testing.T) {
	r := &Runner{Backend: RuleBackend{}, NoulThreshold: 0.5}
	state := map[string]interface{}{"items": []map[string]interface{}{
		{"subject": "Invoice due Friday payment"},
		{"subject": "Weekly newsletter digest unsubscribe"},
	}}
	n := &Node{ID: "s", Type: NodeSift, Questions: []Question{{ID: "could_stop", Kind: QNoul, Instructions: "needs action?"}}}
	if esc, err := r.execNode(context.Background(), n, state); err != nil || esc {
		t.Fatalf("sift failed esc=%v err=%v", esc, err)
	}
	kept, _ := state["sifted"].([]interface{})
	if len(kept) != 1 {
		t.Fatalf("expected 1 kept, got %d", len(kept))
	}
}

func TestJSONCompatibleShape(t *testing.T) {
	raw := `{"name":"inbox-triage","version":"0.1.0","root":{"id":"root","type":"chain","steps":[
		{"id":"triage","type":"judge","question":{"id":"could_stop","kind":"noul","instructions":"Needs action?"}},
		{"id":"gate","type":"code","gate":"is_yes"}]}}`
	w, err := ParseStrict([]byte(raw))
	if err != nil {
		t.Fatalf("agentrun-compatible JSON rejected: %v", err)
	}
	var back []byte
	back, _ = json.Marshal(w)
	if !strings.Contains(string(back), "could_stop") {
		t.Fatal("round-trip lost question")
	}
}
