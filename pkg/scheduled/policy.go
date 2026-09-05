// Package scheduled — policy additions: missed-run behavior, timezone
// resolution, and deterministic idempotency keys.
//
// Missed runs (Ghost was offline during a scheduled run) need an explicit
// per-item decision: run once immediately, skip, run at the next scheduled
// occurrence, or notify the user. Reminders default to notify-or-run-once
// (a commitment to the user); recurring automations default to next
// (never double-deliver a weekly brief); one-shots default to run-once
// inside a grace window, else skip with notice.
//
// Timezone is first-class: explicit request > user configuration > Ghost
// configuration > UTC fallback. Never silently assume UTC when the user's
// configured timezone is known; never infer a timezone from a casually
// mentioned city.
package scheduled

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// MissedPolicy decides what happens when Ghost was offline for a run.
type MissedPolicy string

const (
	// MissedRunOnce executes the missed run exactly once on recovery.
	MissedRunOnce MissedPolicy = "run_once"
	// MissedSkip drops the missed occurrence silently (next cycle continues).
	MissedSkip MissedPolicy = "skip"
	// MissedNext waits for the next scheduled occurrence (no catch-up).
	MissedNext MissedPolicy = "next"
	// MissedNotify tells the user instead of executing (for commitments
	// whose moment has passed, e.g. "remind me at 9 to call").
	MissedNotify MissedPolicy = "notify"
)

// graceWindow bounds run-once catch-up: a missed run older than this is
// never executed (stale commitments become notifications, not actions).
const graceWindow = 24 * time.Hour

// DefaultMissedPolicy returns the policy for an item type when none is
// configured explicitly.
func DefaultMissedPolicy(t ItemType) MissedPolicy {
	switch t {
	case TypeReminder:
		return MissedNotify
	case TypeAutomation:
		return MissedNext
	default:
		return MissedRunOnce
	}
}

// MissedDecision is the runtime's verdict for one missed occurrence.
type MissedDecision struct {
	Policy     MissedPolicy `json:"policy"`
	ShouldRun  bool         `json:"should_run"`
	NotifyUser bool         `json:"notify_user"`
	Reason     string       `json:"reason"`
}

// ClassifyMissed decides a missed run. now is the recovery time,
// scheduledAt the missed fire time. An explicit policy overrides the
// type default; grace expiry converts run-once into notify.
func ClassifyMissed(t ItemType, policy MissedPolicy, scheduledAt, now time.Time) MissedDecision {
	if policy == "" {
		policy = DefaultMissedPolicy(t)
	}
	late := now.Sub(scheduledAt)
	if late < 0 {
		late = 0
	}
	switch policy {
	case MissedSkip:
		return MissedDecision{Policy: policy, Reason: "missed occurrence skipped by policy"}
	case MissedNext:
		return MissedDecision{Policy: policy, Reason: "will run at next scheduled occurrence"}
	case MissedNotify:
		return MissedDecision{Policy: policy, NotifyUser: true, Reason: "user will be notified of missed commitment"}
	case MissedRunOnce:
		if late > graceWindow {
			return MissedDecision{Policy: MissedNotify, NotifyUser: true,
				Reason: "missed run too stale to execute; notifying instead"}
		}
		return MissedDecision{Policy: policy, ShouldRun: true, Reason: "catching up missed run once"}
	default:
		return MissedDecision{Policy: MissedNext, Reason: "unknown policy; waiting for next occurrence"}
	}
}

// ResolveTimezone implements the precedence: explicit request > user
// configuration > Ghost configuration > UTC fallback. Empty inputs yield
// "UTC" rather than a guess. A casually mentioned city must never be
// passed here as an implicit zone — only configured values.
func ResolveTimezone(explicit, user, ghost string) string {
	for _, z := range []string{explicit, user, ghost} {
		if z == "" {
			continue
		}
		if _, err := time.LoadLocation(z); err == nil {
			return z
		}
	}
	return "UTC"
}

// InZone converts t to the item's timezone for display. Invalid or empty
// zones fall back to UTC rather than failing.
func InZone(t time.Time, zone string) time.Time {
	if zone == "" {
		zone = "UTC"
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		loc = time.UTC
	}
	return t.In(loc)
}

// ExecutionKey returns the deterministic idempotency key for one
// scheduled occurrence: sha256(itemID + scheduledAt UTC). A restart can
// never produce two reminders/two briefs for the same occurrence because
// the store dedups on this key (see Service.executeItem).
func ExecutionKey(itemID string, scheduledAt time.Time) string {
	sum := sha256.Sum256([]byte(itemID + "\x00" + scheduledAt.UTC().Format(time.RFC3339)))
	return hex.EncodeToString(sum[:])
}
