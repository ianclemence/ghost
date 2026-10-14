package modelhealth

import (
	"testing"
	"time"
)

func feed(l *Ledger, n int, out Outcome, ms int64) {
	for i := 0; i < n; i++ {
		l.Record(Call{Model: "m", Outcome: out, LatencyMs: ms, At: time.Now()})
	}
}

func TestSteadyModelIsHealthy(t *testing.T) {
	l := Open(t.TempDir())
	feed(l, 60, OK, 2000)
	if st, _ := l.DoctorLine(); st != "ok" {
		t.Fatalf("a steady model drew a warning: %v", l.Summaries())
	}
}

func TestSuddenFailuresAreFlaggedAgainstTheBaseline(t *testing.T) {
	l := Open(t.TempDir())
	feed(l, 60, OK, 2000)
	feed(l, 12, Failed, 0)
	feed(l, 8, OK, 2000)
	st, msg := l.DoctorLine()
	if st != "warn" || msg == "" {
		t.Fatalf("a model that started failing was not flagged: %s %s", st, msg)
	}
}

func TestSuddenSlownessIsFlagged(t *testing.T) {
	l := Open(t.TempDir())
	feed(l, 60, OK, 2000)
	feed(l, 20, OK, 9000)
	if st, _ := l.DoctorLine(); st != "warn" {
		t.Fatal("a model that became 4x slower was not flagged")
	}
}

func TestRefusedRequestsAreNotTheModelsFault(t *testing.T) {
	l := Open(t.TempDir())
	feed(l, 60, OK, 2000)
	feed(l, 20, Refused, 0)
	if st, _ := l.DoctorLine(); st != "ok" {
		t.Fatal("requests the provider refused for their own shape blamed the model")
	}
}

func TestLedgerSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	first := Open(dir)
	feed(first, 30, OK, 1500)
	again := Open(dir)
	if s := again.Summaries(); len(s) != 1 || s[0].Calls != 30 {
		t.Fatalf("history was lost: %+v", s)
	}
}

func TestTooFewCallsSayNothing(t *testing.T) {
	l := Open(t.TempDir())
	feed(l, 5, Failed, 0)
	if _, msg := l.DoctorLine(); msg == "" {
		t.Fatal("expected a summary line")
	}
	if l.Summaries()[0].Drift != "" {
		t.Fatal("five calls are not enough to call drift")
	}
}
