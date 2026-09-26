package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
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

	// Proposal binding. When ProposalID is set the notice is a proactive
	// proposal: delivery attaches a rich suggestion card whose actions carry
	// RequestID, so the owner's approval resolves the exact broker request
	// the runtime created for this proposal — no separate approval path.
	ProposalID string
	RequestID  string
	Actions    []cards.Action
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

	// persistDir, when set, durably stores budget/cooldown/dedupe so a
	// restart doesn't reset the user's spam protection.
	persistDir string
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
	n.saveLocked()
	if n.onNotify != nil {
		n.onNotify(nt)
	}
	return DecisionNotify
}

// ApplyPolicy tunes the gate from PROACTIVE_PREFERENCES.md. Non-positive
// values are ignored (defaults stand).
func (n *Noticer) ApplyPolicy(dailyBudget int, cooldown, dedupeTTL time.Duration) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if dailyBudget > 0 && dailyBudget <= 50 {
		n.dailyBudget = dailyBudget
	}
	if cooldown > 0 {
		n.cooldown = cooldown
	}
	if dedupeTTL > 0 {
		n.dedupeTTL = dedupeTTL
	}
}

// Budget reports today's push count and the daily cap for the owner-facing
// status. It rolls the day over first so a stale count is never reported.
func (n *Noticer) Budget() (used, max int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	day := time.Now().Format("20060102")
	if day != n.today {
		n.today = day
		n.countToday = 0
	}
	return n.countToday, n.dailyBudget
}

// pushSnapshot is the durable form of the gate state.
type pushSnapshot struct {
	Today      string           `json:"today"`
	CountToday int              `json:"count_today"`
	Topics     map[string]int64 `json:"topics,omitempty"`
	Dedupe     map[string]int64 `json:"dedupe,omitempty"`
}

// WithPersistence enables durable budget/cooldown/dedupe under dir
// (workspace/proactive). Best-effort: failures keep the in-memory gate.
func (n *Noticer) WithPersistence(dir string) *Noticer {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.persistDir = dir
	data, err := os.ReadFile(filepath.Join(dir, "pushes.json"))
	if err != nil {
		return n
	}
	var snap pushSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return n
	}
	today := time.Now().Format("20060102")
	if snap.Today == today {
		n.today = snap.Today
		n.countToday = snap.CountToday
	}
	now := time.Now()
	for k, v := range snap.Topics {
		if t := time.Unix(v, 0); t.After(now) {
			n.topicUntil[k] = t
		}
	}
	for k, v := range snap.Dedupe {
		if t := time.Unix(v, 0); t.After(now) {
			n.seenDedupe[k] = t
		}
	}
	return n
}

// saveLocked persists gate state. Caller holds the mutex.
func (n *Noticer) saveLocked() {
	if n.persistDir == "" {
		return
	}
	snap := pushSnapshot{Today: n.today, CountToday: n.countToday,
		Topics: map[string]int64{}, Dedupe: map[string]int64{}}
	if snap.Today == "" {
		snap.Today = time.Now().Format("20060102")
	}
	for k, v := range n.topicUntil {
		snap.Topics[k] = v.Unix()
	}
	for k, v := range n.seenDedupe {
		snap.Dedupe[k] = v.Unix()
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}
	_ = os.MkdirAll(n.persistDir, 0755)
	tmp := filepath.Join(n.persistDir, "pushes.json.tmp")
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(n.persistDir, "pushes.json"))
}
