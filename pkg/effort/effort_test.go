package effort

import "testing"

func TestPolicyBounds(t *testing.T) {
	q := Policy(Quick)
	n := Policy(Normal)
	d := Policy(Deep)
	if !(q.MaxToolCalls < n.MaxToolCalls) {
		t.Fatalf("quick must allow fewer tool calls than normal: %d vs %d", q.MaxToolCalls, n.MaxToolCalls)
	}
	if !(q.MaxOutputTokens < n.MaxOutputTokens && n.MaxOutputTokens < d.MaxOutputTokens) {
		t.Fatal("output budget must grow with effort")
	}
	if q.AllowCloudEscalation {
		t.Fatal("quick must not escalate to cloud")
	}
	if !d.AllowCloudEscalation {
		t.Fatal("deep may escalate to cloud")
	}
	if q.Verification == d.Verification {
		t.Fatal("verification strength must differ by effort")
	}
}

func TestClassifyTrivialIsQuick(t *testing.T) {
	for _, msg := range []string{"hi", "what time is it", "thanks!"} {
		if got := Classify(msg); got != Quick {
			t.Fatalf("Classify(%q) = %s, want quick", msg, got)
		}
	}
}

func TestClassifyComplexIsDeep(t *testing.T) {
	msg := "Debug this Go stack trace, then refactor the SQL query, and also step by step explain the bug"
	if got := Classify(msg); got != Deep {
		t.Fatalf("Classify(complex) = %s, want deep", got)
	}
}

func TestClassifyOrdinaryIsNormal(t *testing.T) {
	msg := "Summarize the document I uploaded yesterday and save the notes"
	if got := Classify(msg); got != Normal {
		t.Fatalf("Classify(ordinary) = %s, want normal", got)
	}
}

func TestEscalationBounded(t *testing.T) {
	if next, ok := Escalate(Quick); !ok || next != Normal {
		t.Fatalf("quick -> %s %v", next, ok)
	}
	if next, ok := Escalate(Normal); !ok || next != Deep {
		t.Fatalf("normal -> %s %v", next, ok)
	}
	if next, ok := Escalate(Deep); ok || next != Deep {
		t.Fatalf("deep must not escalate further: %s %v", next, ok)
	}
	if EscalateForFailure(Deep) != Deep {
		t.Fatal("failure escalation must stay bounded at deep")
	}
}

func TestLadderFor(t *testing.T) {
	if LadderFor(Quick) >= LadderFor(Normal) || LadderFor(Normal) >= LadderFor(Deep) {
		t.Fatal("ladder must increase with effort")
	}
}
