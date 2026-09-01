package agent

import (
	"testing"
)

// TestNoticerGate asserts the "proactive ≠ noisy" rules deterministically.
func TestNoticerGate(t *testing.T) {
	var notified []Notice
	n := NewNoticer(func(nt Notice) { notified = append(notified, nt) })

	// Low priority, not urgent → silenced.
	if got := n.ShouldNotify(Notice{Topic: "news", Priority: 4, Confidence: 0.9, DedupeKey: "n1"}); got != DecisionLowPriority {
		t.Fatalf("low priority: got %v", got)
	}
	// Low confidence → silenced even if priority is high.
	if got := n.ShouldNotify(Notice{Topic: "deadline", Priority: 9, Confidence: 0.2, DedupeKey: "n2"}); got != DecisionLowConfidence {
		t.Fatalf("low confidence: got %v", got)
	}
	// High priority + confidence → notify.
	if got := n.ShouldNotify(Notice{Topic: "deadline", Priority: 9, Confidence: 0.9, DedupeKey: "n3"}); got != DecisionNotify {
		t.Fatalf("expected notify, got %v", got)
	}
	// Same dedupe key → duplicate.
	if got := n.ShouldNotify(Notice{Topic: "deadline", Priority: 9, Confidence: 0.9, DedupeKey: "n3"}); got != DecisionDuplicate {
		t.Fatalf("expected duplicate, got %v", got)
	}
	// Same topic, different key → cooling down (per-topic silence).
	if got := n.ShouldNotify(Notice{Topic: "deadline", Priority: 9, Confidence: 0.9, DedupeKey: "n4"}); got != DecisionCoolingDown {
		t.Fatalf("expected cooldown, got %v", got)
	}
	// Urgent, low numerical priority, fresh topic → notify (urgency overrides).
	if got := n.ShouldNotify(Notice{Topic: "flight", Priority: 3, Urgency: true, Confidence: 0.9, DedupeKey: "n5"}); got != DecisionNotify {
		t.Fatalf("expected notify for urgent, got %v", got)
	}

	if len(notified) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(notified))
	}
}

func TestNoticerDailyBudget(t *testing.T) {
	var notified []Notice
	n := NewNoticer(func(nt Notice) { notified = append(notified, nt) })
	// Exhaust the budget across distinct topics/keys.
	for i := 0; i < n.dailyBudget; i++ {
		d := n.ShouldNotify(Notice{Topic: "t" + string(rune('a'+i)), Priority: 9, Confidence: 0.9, DedupeKey: "k" + string(rune('a'+i))})
		if d != DecisionNotify {
			t.Fatalf("budget item %d: got %v", i, d)
		}
	}
	if got := n.ShouldNotify(Notice{Topic: "tzz", Priority: 9, Confidence: 0.9, DedupeKey: "kzz"}); got != DecisionBudgetExhausted {
		t.Fatalf("expected budget exhausted, got %v", got)
	}
}
