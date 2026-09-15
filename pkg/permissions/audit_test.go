package permissions

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openAuditBroker(t *testing.T) *Broker {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := Open(db, ModeAsk, 0)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAuditModeEnforcesNothing(t *testing.T) {
	b := openAuditBroker(t)
	b.SetMode(ModeAudit)
	if got := b.Evaluate("email.send", "send", "owner", RiskHighImpact); got != VerdictAllow {
		t.Fatalf("audit Evaluate must allow, got %s", got)
	}
	if got := b.Evaluate("", "", "owner", RiskHighImpact); got != VerdictAllow {
		t.Fatalf("audit Evaluate must allow even malformed input, got %s", got)
	}
}

func TestEvaluateAuditReportsStrictVerdict(t *testing.T) {
	b := openAuditBroker(t)
	enforced, wouldBe := b.EvaluateAudit("email.send", "send", "owner", RiskHighImpact)
	if enforced != VerdictAllow {
		t.Fatalf("enforced = %s, want allow", enforced)
	}
	if wouldBe != VerdictAsk {
		t.Fatalf("wouldBe = %s, want ask (high impact never auto-allows)", wouldBe)
	}
	enforced, wouldBe = b.EvaluateAudit("weather.get", "read", "owner", RiskReadOnly)
	if enforced != VerdictAllow || wouldBe != VerdictAllow {
		t.Fatalf("read-only: enforced=%s wouldBe=%s", enforced, wouldBe)
	}
}

func TestAuditEmitsEvent(t *testing.T) {
	b := openAuditBroker(t)
	var gotType string
	var gotReq *Request
	b.SetEmitter(func(t string, r *Request) { gotType, gotReq = t, r })
	if verdict := b.Audit("email.send", "send", "owner", RiskConsequential); verdict != VerdictAllow {
		t.Fatalf("audit must enforce allow, got %s", verdict)
	}
	if gotType != "permission.audited" {
		t.Fatalf("event type = %q", gotType)
	}
	if gotReq == nil || gotReq.Capability != "email.send" || gotReq.Risk != RiskConsequential {
		t.Fatalf("audit record wrong: %+v", gotReq)
	}
	if gotReq.Reason == "" {
		t.Fatal("audit record must carry the would-be verdict")
	}
}
