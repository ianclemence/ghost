package workflow

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerAppendAndRate(t *testing.T) {
	l, err := OpenLedger(filepath.Join(t.TempDir(), "wf.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer l.Close()
	in := map[string]interface{}{"subject": "hi", "api_key": "SECRET123"}
	rec := RunRecord{RunID: "r1", WorkflowName: "inbox", WorkflowVer: "0.1.0",
		Digest: "abc", NodeID: "j", Decision: "yes", Confidence: 0.96,
		InputHash: HashState(in), OutputHash: HashState(map[string]interface{}{"a": 1})}
	if err := l.Append(rec); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := l.Append(RunRecord{RunID: "r2", WorkflowName: "inbox", WorkflowVer: "0.1.0",
		Digest: "abc", NodeID: "e", Escalated: true, Reason: "low conf",
		InputHash: "x", OutputHash: "y"}); err != nil {
		t.Fatalf("append2: %v", err)
	}
	esc, total, err := l.EscalationRate("inbox", "0.1.0", time.Now().Add(-time.Hour))
	if err != nil || total != 2 || esc != 1 {
		t.Fatalf("rate wrong: esc=%d total=%d err=%v", esc, total, err)
	}
}

func TestNotesDedup(t *testing.T) {
	n, err := OpenNotes(filepath.Join(t.TempDir(), "lessons.jsonl"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ok, err := n.Add("inbox", "Newsletters decide nothing; filter them on sender before opening bodies.")
	if err != nil || !ok {
		t.Fatalf("first add failed ok=%v err=%v", ok, err)
	}
	ok, _ = n.Add("inbox", "Newsletters decide nothing; filter them on sender before opening bodies.")
	if ok {
		t.Fatal("duplicate lesson must be ignored")
	}
}
