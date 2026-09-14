package agent

import "testing"

func TestAttemptBudgetDeniesOverCap(t *testing.T) {
	b := newAttemptBudget(map[string]int{"browser.transact": 2})
	args := map[string]interface{}{}
	if r := b.guard("r", "s", "browser.transact", "browser_submit", args); r != "" {
		t.Fatalf("attempt 1 denied: %s", r)
	}
	if r := b.guard("r", "s", "browser.transact", "browser_submit", args); r != "" {
		t.Fatalf("attempt 2 denied: %s", r)
	}
	if r := b.guard("r", "s", "browser.transact", "browser_submit", args); r == "" {
		t.Fatal("attempt 3 over cap allowed")
	}
	if r := b.guard("r", "s", "memory.recall", "recall", args); r != "" {
		t.Fatalf("uncapped capability denied: %s", r)
	}
}

func TestRoutineGuardHalvesCaps(t *testing.T) {
	g := &Governance{GhostID: "g", AgentID: "a"}
	b := newAttemptBudget(map[string]int{"email.send": 4})
	gr := b.routineGuard(g)
	args := map[string]interface{}{}
	// Interactive (no routine scope) abstains.
	if r := gr("r", "session:plain", "email.send", "email_send", args); r != "" {
		t.Fatalf("interactive denied: %s", r)
	}
	g.SetRoutineContext("session:routine:1", "routine-1", nil)
	// Routine cap = 4/2 = 2.
	gr("r", "session:routine:1", "email.send", "email_send", args)
	gr("r", "session:routine:1", "email.send", "email_send", args)
	if r := gr("r", "session:routine:1", "email.send", "email_send", args); r == "" {
		t.Fatal("routine over half-cap allowed")
	}
	g.ClearRoutineContext("session:routine:1")
}
