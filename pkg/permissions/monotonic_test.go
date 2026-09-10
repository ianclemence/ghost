package permissions

import "testing"

func TestCombineDenyAbsorbing(t *testing.T) {
	// Every permutation containing a deny must yield deny.
	perms := [][]Decision{
		{DecisionAllow, DecisionAsk, DecisionDeny},
		{DecisionDeny, DecisionAllow, DecisionAsk},
		{DecisionAsk, DecisionDeny, DecisionAllow},
		{DecisionDeny, DecisionDeny},
		{DecisionAllow, DecisionAsk},
	}
	want := []Decision{DecisionDeny, DecisionDeny, DecisionDeny, DecisionDeny, DecisionAsk}
	for i, p := range perms {
		if got := Combine(p...); got != want[i] {
			t.Fatalf("Combine(%v) = %s, want %s", p, got, want[i])
		}
	}
	// Empty and all-allow are allow.
	if got := Combine(); got != DecisionAllow {
		t.Fatalf("Combine() = %s", got)
	}
	if got := Combine(DecisionAllow, DecisionAllow); got != DecisionAllow {
		t.Fatalf("all-allow = %s", got)
	}
}

func TestMoreRestrictiveCommutative(t *testing.T) {
	all := []Decision{DecisionAllow, DecisionAsk, DecisionDeny}
	for _, a := range all {
		for _, b := range all {
			if MoreRestrictive(a, b) != MoreRestrictive(b, a) {
				t.Fatalf("MoreRestrictive not commutative for %s,%s", a, b)
			}
		}
	}
}

func TestVerdictDecision(t *testing.T) {
	if VerdictDecision(VerdictAllow) != DecisionAllow {
		t.Fatal("allow mapping")
	}
	if VerdictDecision(VerdictDeny) != DecisionDeny {
		t.Fatal("deny mapping")
	}
	if VerdictDecision(VerdictAsk) != DecisionAsk {
		t.Fatal("ask mapping")
	}
}
