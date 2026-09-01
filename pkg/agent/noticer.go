package agent

import (
	"sync"
	"time"
)

// Decision is the outcome of a proactive gating check.
type Decision string

const (
	DecisionNotify          Decision = "notify"
	DecisionLowPriority     Decision = "silenced_low_priority"
	DecisionLowConfidence   Decision = "silenced_low_confidence"
	DecisionDuplicate       Decision = "duplicate"
	DecisionCoolingDown     Decision = "cooling_down"
	DecisionBudgetExhausted Decision = "budget_exhausted"
)

// Notice is a candidate proactive message Ghost is considering surfacing.
type Notice struct {
	Topic      string  // subject, for per-topic cooldown / dedupe
	Priority   int     // 1-10: how important to this user the thing is
	Urgency    bool    // time-sensitive (deadline, data arrived, unfinished task)
	Confidence float64 // 0-1: how sure Ghost is it's genuinely useful
	DedupeKey  string  // unique content key (duplicate suppression)
	Message    string
}

// Noticer is the value gate for proactive behaviour. The principle: "proactive
// does not mean noisy." Ghost should only interrupt when expected usefulness is
// high, and should never spam the same thing twice or exceed a daily budget.
// The gate is deterministic and fully testable; candidate signals are wired to
// it later.
type Noticer struct {
	mu sync.Mutex

	threshold     int           // min priority to consider (unless urgent)
	dailyBudget   int           // max notices per day
	cooldown      time.Duration // per-topic silence window
	dedupeTTL     time.Duration // how long a dedupe key is suppressed
	minConfidence float64       // below this, don't surface

	topicUntil map[string]time.Time
	seenDedupe map[string]time.Time

	today      string
	countToday int

	onNotify func(Notice)
}

// NewNoticer returns a conservative, spam-resistant noticer.
func NewNoticer(onNotify func(Notice)) *Noticer {
	return &Noticer{
		threshold:     7,
		dailyBudget:   3,
		cooldown:      6 * time.Hour,
		dedupeTTL:     24 * time.Hour,
		minConfidence: 0.6,
		topicUntil:    map[string]time.Time{},
		seenDedupe:    map[string]time.Time{},
		onNotify:      onNotify,
	}
}

// ShouldNotify applies the value gate and returns the decision. On DecisionNotify
// it records budget/cooldown/dedupe and invokes the notify callback. It is
// deterministic given time (budget/cooldown) and the notice fields.
func (n *Noticer) ShouldNotify(nt Notice) Decision {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Reset the daily counter when the day rolls over.
	day := time.Now().Format("20060102")
	if day != n.today {
		n.today = day
		n.countToday = 0
	}

	// 1. Gate on importance/urgency.
	if nt.Priority < n.threshold && !nt.Urgency {
		return DecisionLowPriority
	}
	// 2. Gate on confidence (only surface things we're sure about).
	if nt.Confidence < n.minConfidence {
		return DecisionLowConfidence
	}
	// 3. Duplicate suppression.
	if nt.DedupeKey != "" {
		if until, ok := n.seenDedupe[nt.DedupeKey]; ok && time.Now().Before(until) {
			return DecisionDuplicate
		}
	}
	// 4. Per-topic silence.
	if nt.Topic != "" {
		if until, ok := n.topicUntil[nt.Topic]; ok && time.Now().Before(until) {
			return DecisionCoolingDown
		}
	}
	// 5. Daily budget.
	if n.countToday >= n.dailyBudget {
		return DecisionBudgetExhausted
	}

	// Approved — record and deliver.
	if nt.DedupeKey != "" {
		n.seenDedupe[nt.DedupeKey] = time.Now().Add(n.dedupeTTL)
	}
	if nt.Topic != "" {
		n.topicUntil[nt.Topic] = time.Now().Add(n.cooldown)
	}
	n.countToday++
	if n.onNotify != nil {
		n.onNotify(nt)
	}
	return DecisionNotify
}
