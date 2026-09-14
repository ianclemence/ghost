package agent

import (
	"fmt"
	"sync"
	"time"
)

// Attempt budgets: per-capability daily attempt caps enforced as a
// deny-only Governance Guard. The broker stays authoritative; this layer
// only makes outcomes stricter. Counts are attempts (evaluations), which
// bound executions for gated tools since every execution evaluates first.
//
// Budgets reset on process restart (documented, not silent): the appliance
// is single-owner and restarts are infrequent. A restart widens nothing —
// the broker still gates every consequential act.
type attemptBudget struct {
	mu     sync.Mutex
	caps   map[string]int
	counts map[string]int
	day    string
}

// DefaultAttemptCaps bounds high-impact capabilities per day. Read-only and
// low-risk capabilities are uncapped (absent = unlimited).
func DefaultAttemptCaps() map[string]int {
	return map[string]int{
		"browser.transact":  5,
		"email.send":        20,
		"exec.shell":        50,
		"exec.sandbox":      50,
		"message.send":      50,
		"device.control":    100,
		"computer.control":  100,
		"calendar.modify":   50,
		"routine.create":    20,
		"routine.modify":    20,
		"skills.manage":     10,
		"system.update":     3,
	}
}

func newAttemptBudget(caps map[string]int) *attemptBudget {
	if caps == nil {
		caps = DefaultAttemptCaps()
	}
	return &attemptBudget{caps: caps, counts: map[string]int{}}
}

// guard evaluates the budget. Exceeding the cap denies with an honest
// reason; uncapped capabilities abstain.
func (b *attemptBudget) guard(_, _, capabilityID, _ string, _ map[string]interface{}) string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	cap, ok := b.caps[capabilityID]
	if !ok || cap <= 0 {
		return ""
	}
	today := time.Now().UTC().Format("2006-01-02")
	if b.day != today {
		b.day = today
		b.counts = map[string]int{}
	}
	b.counts[capabilityID]++
	if b.counts[capabilityID] > cap {
		return fmt.Sprintf("That capability reached its daily attempt budget (%d/day for %s), so I didn't run it. It resets tomorrow.", cap, capabilityID)
	}
	return ""
}

// routineGuard returns a Guard that applies tighter daily caps to
// unattended (routine-scoped) turns. Interactive turns abstain. Routine
// caps default to half the interactive cap (min 1): a heartbeat that
// needs more is misconfigured, not throttled unfairly.
func (b *attemptBudget) routineGuard(g *Governance) Guard {
	return func(_, sessionKey, capabilityID, _ string, _ map[string]interface{}) string {
		if b == nil || g == nil || g.routineIDFor(sessionKey) == "" {
			return ""
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		cap, ok := b.caps[capabilityID]
		if !ok || cap <= 0 {
			return ""
		}
		if cap = cap / 2; cap < 1 {
			cap = 1
		}
		today := time.Now().UTC().Format("2006-01-02")
		if b.day != today {
			b.day = today
			b.counts = map[string]int{}
		}
		key := "routine:" + capabilityID
		b.counts[key]++
		if b.counts[key] > cap {
			return fmt.Sprintf("This routine reached its daily budget (%d/day for %s in unattended runs), so I didn't run it.", cap, capabilityID)
		}
		return ""
	}
}

func (b *attemptBudget) count(capabilityID string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.day != time.Now().UTC().Format("2006-01-02") {
		return 0
	}
	return b.counts[capabilityID]
}
