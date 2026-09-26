package ideas

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// This file is the opportunity/proposal half of the package. Phase A/B
// (ideas.go, generate.go) drafts advice from caller-supplied signals; this
// half turns a runtime Observation into a grounded, actionable, deduplicated
// proposal with a durable lifecycle.
//
// The package still never opens a store: the runtime (pkg/agent) collects
// Observations from the real subsystems and hands them in. Everything here
// is deterministic and cheap — no model call decides whether to interrupt.

// ObsKind classifies what Ghost observed.
type ObsKind string

const (
	ObsReminderOverdue ObsKind = "reminder_overdue"
	ObsReminderFailed  ObsKind = "reminder_failed"
	ObsRoutineFailed   ObsKind = "routine_failed"
	ObsRoutineWaiting  ObsKind = "routine_waiting"
	ObsRoutineStale    ObsKind = "routine_stale"
	ObsGoalStalled     ObsKind = "goal_stalled"
	ObsTaskOverdue     ObsKind = "task_overdue"
	ObsTaskFailed      ObsKind = "task_failed"
)

// Category groups kinds for per-category throttling and owner controls.
func (k ObsKind) Category() string {
	switch k {
	case ObsReminderOverdue, ObsReminderFailed:
		return "reminders"
	case ObsRoutineFailed, ObsRoutineWaiting, ObsRoutineStale:
		return "routines"
	case ObsGoalStalled:
		return "goals"
	case ObsTaskOverdue, ObsTaskFailed:
		return "tasks"
	default:
		return "other"
	}
}

// Title is the short human label for the category.
func (k ObsKind) Title() string {
	switch k {
	case ObsReminderOverdue:
		return "a reminder you never got"
	case ObsReminderFailed:
		return "a reminder that failed"
	case ObsRoutineFailed:
		return "a routine that keeps failing"
	case ObsRoutineWaiting:
		return "a routine waiting on you"
	case ObsRoutineStale:
		return "a routine that stopped running"
	case ObsGoalStalled:
		return "a goal that went quiet"
	case ObsTaskOverdue:
		return "a task past its due time"
	case ObsTaskFailed:
		return "a task that failed"
	default:
		return "something"
	}
}

// Observation is one grounded fact about runtime state, collected from a real
// subsystem. Every field is derived from stored rows — never from model
// output — so a proposal built from it can always cite where it came from.
type Observation struct {
	Kind    ObsKind
	Subject string // stable entity id (reminder id, routine id, goal id, job id)
	// Summary is one plain sentence of runtime fact ("your 09:00 reminder
	// never fired"). It is rendered from rows, not written by a model.
	Summary string
	Detail  string
	At      time.Time
	// Evidence cites the rows behind the observation.
	Evidence []Source
	// StateVer digests the state the observation was built from. The runtime
	// recomputes it before acting: a mismatch voids the approval.
	StateVer string
	// Actionable is true when the runtime knows a concrete next action.
	Actionable bool
	// Priority (1-10), Urgency, and Confidence feed the deterministic gate.
	Priority   int
	Urgency    bool
	Confidence float64
	// NotBefore defers surfacing until a time the observation itself implies
	// (e.g. a reminder that has not reached its due window yet).
	NotBefore time.Time
}

// Risk mirrors the permission broker's risk classes. It is deliberately a
// plain string so this package stays dependency-free; pkg/agent maps it onto
// permissions.Risk from the capability registry, never from model output.
type Risk string

const (
	RiskReadOnly      Risk = "read_only"
	RiskLow           Risk = "low_risk"
	RiskConsequential Risk = "consequential"
	RiskHighImpact    Risk = "high_impact"
)

// Plan kinds.
const (
	PlanTool  = "tool"
	PlanLocal = "local"
)

// Plan is the concrete action a proposal offers. Two shapes exist:
//
//   - Tool: call a registered tool through the existing registry, which runs
//     its own schema validation, permission grant check, evidence contract
//     and verification. The broker decides allow/ask/deny first.
//   - Local: a first-class runtime operation with no tool surface (pause a
//     routine, reschedule a reminder). It is declared here with its
//     capability and risk, authorized by the same broker, and executed by a
//     thin adapter over the same service the console API calls.
//
// Either way the model never chooses it at execution time: the plan is
// pinned when the proposal is created and revalidated before it runs.
type Plan struct {
	Kind       string            `json:"kind"` // "tool" | "local"
	Op         string            `json:"op,omitempty"`
	Capability string            `json:"capability"`
	Tool       string            `json:"tool,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Risk       Risk              `json:"risk"`
	// Describe is the owner-facing phrase for the action ("remind you again
	// in two hours"), used in the proposal line and the button label.
	Describe string `json:"describe,omitempty"`
}

// DedupeWindow says how long an answered opportunity stays quiet. The runtime
// passes owner policy in; zero fields fall back to the documented defaults.
type DedupeWindow struct {
	Dismissed time.Duration // after a dismissal, before the same key may return
	Completed time.Duration // after a successful action, before it may return
	Expired   time.Duration // after an unanswered proposal expires
	Failed    time.Duration // after a failed action (backoff)
}

// DefaultDedupeWindow is the conservative default anti-nag policy.
func DefaultDedupeWindow() DedupeWindow {
	return DedupeWindow{
		Dismissed: 7 * 24 * time.Hour,
		Completed: 24 * time.Hour,
		Expired:   6 * time.Hour,
		Failed:    4 * time.Hour,
	}
}

func (d DedupeWindow) withDefaults() DedupeWindow {
	def := DefaultDedupeWindow()
	if d.Dismissed <= 0 {
		d.Dismissed = def.Dismissed
	}
	if d.Completed <= 0 {
		d.Completed = def.Completed
	}
	if d.Expired <= 0 {
		d.Expired = def.Expired
	}
	if d.Failed <= 0 {
		d.Failed = def.Failed
	}
	return d
}

// StateVer digests the parts of state an observation was built from. Two
// collections of the same entity that differ only in timestamps would
// otherwise look "changed" every tick; callers pass only the meaningful
// fields.
func StateVer(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// DedupeKey is the stable identity of an opportunity. It deliberately
// excludes timestamps: the same underlying situation is the same
// opportunity until it is answered.
func DedupeKey(kind ObsKind, subject string, discriminator string) string {
	key := string(kind) + ":" + subject
	if d := strings.TrimSpace(discriminator); d != "" {
		key += ":" + d
	}
	return key
}

// Quiet reports whether an earlier idea with this dedupe key should suppress a
// fresh one. One live opportunity per key, ever: a pending/presented/accepted/
// snoozed/executing record blocks recreation, and an answered one blocks it
// for its cooldown window.
func Quiet(existing []Idea, key string, now time.Time, win DedupeWindow) (bool, string) {
	win = win.withDefaults()
	for i := range existing {
		e := &existing[i]
		if e.DedupeKey != key {
			continue
		}
		switch e.Status {
		case StatusPending, StatusPresented, StatusAccepted, StatusExecuting:
			return true, "already live as " + e.ID
		case StatusSnoozed:
			if e.SnoozedUntil != nil && now.Before(*e.SnoozedUntil) {
				return true, "snoozed until " + e.SnoozedUntil.Format(time.RFC3339)
			}
			// Snooze elapsed: the opportunity is allowed to return.
			if e.StateVer == "" {
				return true, "snoozed"
			}
			continue
		case StatusDismissed:
			if since(e.DecidedAt, now) < win.Dismissed {
				return true, "dismissed recently"
			}
		case StatusCompleted:
			if since(e.DecidedAt, now) < win.Completed {
				return true, "just completed"
			}
		case StatusFailed:
			if since(e.DecidedAt, now) < win.Failed {
				return true, "failed recently"
			}
		case StatusExpired:
			if since(e.DecidedAt, now) < win.Expired {
				return true, "expired recently"
			}
		case StatusSuperseded:
			if since(e.DecidedAt, now) < win.Dismissed {
				return true, "superseded recently"
			}
		}
	}
	return false, ""
}

func since(t *time.Time, now time.Time) time.Duration {
	if t == nil {
		return 1<<62 - 1
	}
	return now.Sub(*t)
}

// Candidate is the proposal the runtime will consider surfacing. It is an
// Idea with its lifecycle fields filled in; callers persist it (Add) and let
// the gate decide when it goes out.
type Candidate = Idea

// Candidates turns observations into durable pending candidates, skipping any
// opportunity already live or recently answered.
func Candidates(obs []Observation, existing []Idea, now time.Time, win DedupeWindow) []Candidate {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]Candidate, 0, len(obs))
	seen := map[string]bool{}
	for _, o := range obs {
		if strings.TrimSpace(o.Subject) == "" || strings.TrimSpace(o.Summary) == "" {
			continue
		}
		// The dedupe key is stable per opportunity (kind + subject). The state
		// digest is carried separately so a change invalidates the existing
		// proposal rather than spawning a second one beside it.
		key := DedupeKey(o.Kind, o.Subject, "")
		if seen[key] {
			continue
		}
		if quiet, _ := Quiet(existing, key, now, win); quiet {
			continue
		}
		seen[key] = true
		c := Candidate{
			ID:         newID(),
			Title:      o.Title(),
			Body:       o.Summary,
			Sources:    o.Evidence,
			Status:     StatusPending,
			CreatedAt:  now,
			Kind:       o.Kind,
			Reason:     o.reason(),
			Priority:   o.effectivePriority(),
			Urgency:    o.Urgency,
			Confidence: o.Confidence,
			DedupeKey:  key,
			StateVer:   o.StateVer,
		}
		if o.NotBefore.After(now) {
			// A candidate that cannot be useful yet waits as a snooze so it
			// does not surface early and does not get re-created each tick.
			c.Status = StatusSnoozed
			until := o.NotBefore.UTC()
			c.SnoozedUntil = &until
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// StateVerDiscriminator is the half of the dedupe key that changes only when
// the situation itself changes (not when the clock moves).
func (o Observation) StateVerDiscriminator() string { return o.StateVer }

func (o Observation) reason() string {
	if strings.TrimSpace(o.Detail) != "" {
		return o.Detail
	}
	return o.Summary
}

// effectivePriority folds actionability and urgency into the base priority so
// the gate sees one number. An observation Ghost can actually act on is worth
// more than one it can only mention; the runtime never inflates this from
// model output.
func (o Observation) effectivePriority() int {
	p := o.Priority
	if o.Actionable {
		p++
	}
	if o.Urgency {
		p++
	}
	if p > 10 {
		p = 10
	}
	if p < 1 {
		p = 1
	}
	return p
}

// Title renders the owner-facing one-liner for the observation kind.
func (o Observation) Title() string { return o.Kind.Title() }

// Gate is the deterministic surfacing decision. It is intentionally separate
// from delivery: the noticer already enforces budget, per-topic cooldown and
// dedupe; this gate enforces priority, confidence, freshness and the
// "no useless interruption" rule before anything reaches it.
type Gate struct {
	MinPriority   int
	MinConfident  float64
	Informational int // higher bar when there is no concrete action
}

// DefaultGate is the shipped threshold set.
func DefaultGate() Gate {
	return Gate{MinPriority: 7, MinConfident: 0.6, Informational: 8}
}

// ShouldSurface decides whether a candidate may be delivered now.
func (g Gate) ShouldSurface(i Idea, now time.Time) (bool, string) {
	if g.MinPriority == 0 {
		g = DefaultGate()
	}
	if i.Status != StatusPending {
		return false, "not pending"
	}
	if i.ExpiresAt != nil && !now.Before(*i.ExpiresAt) {
		return false, "expired"
	}
	min := g.MinPriority
	if i.Plan == nil {
		// Advice with no action must clear a higher bar: Ghost does not
		// interrupt to muse.
		min = g.Informational
	}
	if i.Priority < min {
		return false, fmt.Sprintf("priority %d below %d", i.Priority, min)
	}
	if i.Confidence < g.MinConfident {
		return false, fmt.Sprintf("confidence %.2f below %.2f", i.Confidence, g.MinConfident)
	}
	return true, ""
}

// Render writes the proposal the owner reads. The voice is calm and direct:
// the observation, then the single offered action. It is assembled from
// structured fields only, so it can never drift from what will actually
// happen.
func Render(i Idea) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(strings.TrimSpace(i.Body), "."))
	b.WriteString(".")
	if i.Plan != nil && strings.TrimSpace(i.Plan.Describe) != "" {
		b.WriteString(" Want me to ")
		b.WriteString(strings.TrimSpace(i.Plan.Describe))
		b.WriteString("?")
	} else {
		// Informational: nothing will execute, so ask nothing.
		b.WriteString(" Want me to take a look?")
	}
	return b.String()
}

// Expired reports whether the proposal's window has closed.
func (i Idea) Expired(now time.Time) bool {
	return i.ExpiresAt != nil && !now.Before(*i.ExpiresAt)
}
