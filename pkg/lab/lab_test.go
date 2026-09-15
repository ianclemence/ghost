package lab

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFreeGateBlocksPaid(t *testing.T) {
	res := RunFreeGate([]GateCheck{
		{Name: "no-secrets-in-fixtures", Run: func() error { return nil }},
		{Name: "suite-parses", Run: func() error { return errTest("broken") }},
	})
	if res.OK() {
		t.Fatal("failing gate must block")
	}
	if len(res.Passed) != 1 || len(res.Failed) != 1 {
		t.Fatalf("got %+v", res)
	}
}

func errTest(s string) error { return &testErr{s} }

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func TestBudgetEnforcement(t *testing.T) {
	r := NewRun(Options{MaxCostUSD: 1.00})
	if err := r.Check(Usage{CostUSD: 0.60}); err != nil {
		t.Fatal(err)
	}
	r.Commit(Usage{CostUSD: 0.60})
	if err := r.Check(Usage{CostUSD: 0.50}); err == nil {
		t.Fatal("over-budget step must fail")
	} else if !strings.Contains(err.Error(), "boundary") {
		t.Fatalf("must report boundary, got %v", err)
	}
}

func TestUnknownCostFailsClosed(t *testing.T) {
	r := NewRun(Options{MaxCostUSD: 1.00})
	if err := r.Check(Usage{CostUnknown: true}); err == nil {
		t.Fatal("unknown cost with a budget must fail closed")
	}
	r2 := NewRun(Options{MaxCostUSD: 1.00, AllowUnknownCost: true})
	if err := r2.Check(Usage{CostUnknown: true}); err != nil {
		t.Fatalf("explicit opt-in must allow: %v", err)
	}
	r3 := NewRun(Options{})
	if err := r3.Check(Usage{CostUnknown: true}); err != nil {
		t.Fatalf("no budget means nothing to enforce: %v", err)
	}
}

func TestDurationBoundary(t *testing.T) {
	r := NewRun(Options{MaxDuration: time.Nanosecond})
	time.Sleep(2 * time.Nanosecond)
	if err := r.Check(Usage{}); err == nil {
		t.Fatal("expired duration must fail")
	}
}

func TestRecordAppendAndResume(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := store.Load("case-1")
	if err != nil || rec.Status != "pending" {
		t.Fatalf("missing record must load pending: %+v %v", rec, err)
	}
	rec, err = store.AppendStep("case-1", Step{Phase: "process", Model: "m", Verdict: "failed"})
	if err != nil || rec.Status != "failed" {
		t.Fatalf("failed step must mark failed: %+v %v", rec, err)
	}
	// Reruns append: history grows, status re-derives.
	rec, err = store.AppendStep("case-1", Step{Phase: "revalidate", Verdict: "passed", Revalidate: VerdictTruePositive, Triage: TriageP1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Steps) != 2 {
		t.Fatalf("history must append, got %d steps", len(rec.Steps))
	}
	if rec.Steps[0].Wave != 0 || rec.Steps[0].Model != "m" {
		t.Fatal("first step must be preserved verbatim")
	}
	pending, err := store.Pending([]string{"case-1", "case-2"})
	if err != nil {
		t.Fatal(err)
	}
	// case-1 failed (terminal-ish but re-runnable? failed stays out of
	// pending only when passed; failed/error re-enter the resume set).
	found := false
	for _, id := range pending {
		if id == "case-2" {
			found = true
		}
	}
	if !found {
		t.Fatal("untouched case must be pending")
	}
}

func TestEmitterJSONL(t *testing.T) {
	var buf bytes.Buffer
	e := NewEmitter(&buf)
	e.Emit("step.completed", map[string]interface{}{"case": "c1", "verdict": "passed"})
	line := strings.TrimSpace(buf.String())
	if !strings.Contains(line, `"event":"step.completed"`) || !strings.Contains(line, `"at":`) {
		t.Fatalf("bad event line: %s", line)
	}
	// Nil emitter never panics.
	NewEmitter(nil).Emit("x", nil)
}
