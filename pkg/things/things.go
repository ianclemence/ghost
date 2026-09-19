// Package things is Ghost's single product-facing model for "things Ghost
// does for you". It is deliberately NOT a new storage layer.
//
// The domain already has exactly one scheduler authority (pkg/scheduled):
// a routine IS a scheduled automation item with Source=="routine" plus a
// metadata sidecar (pkg/routines). Product surfaces historically split the
// two — "Routines" vs "Automations" — which forced owners to learn an
// internal taxonomy to file their own intent.
//
// This package collapses that split at the presentation boundary only. It
// reads the existing models and returns one normalized list. No new tables,
// no duplicated authority, no second scheduler. Shape inference is
// deterministic and total, so the same input always yields the same Thing.
package things

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Kind is the user-facing shape of a Thing. Owners never choose it; Ghost
// infers it from what the Thing actually is.
type Kind string

const (
	// KindReminder: "tell me something at a time".
	KindReminder Kind = "reminder"
	// KindRoutine: "do this work on a recurring cadence".
	KindRoutine Kind = "routine"
	// KindAutomation: "when a schedule fires, run this".
	KindAutomation Kind = "automation"
	// KindTask: "do this work" (manual / no schedule).
	KindTask Kind = "task"
)

// State is the normalized lifecycle state, independent of which backing
// model produced the Thing. It is intentionally identical to the words a
// person would use.
type State string

const (
	StateActive    State = "active"
	StatePaused    State = "paused"
	StateWaiting   State = "waiting" // blocked on permission or configuration
	StateDone      State = "done"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Thing is the one product shape both the phone and the console render.
type Thing struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	What      string     `json:"what"` // the instruction / prompt / message body
	Kind      Kind       `json:"kind"`
	State     State      `json:"state"`
	Schedule  string     `json:"schedule"` // human sentence, e.g. "Every Monday at 9:00 AM"
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	LastRunAt *time.Time `json:"last_run_at,omitempty"`
	RunCount  int        `json:"run_count"`
	LastError string     `json:"last_error,omitempty"`
	WaitingOn string     `json:"waiting_on,omitempty"` // permission id / config action when State==waiting
	Source    string     `json:"source"`               // "routine" | "user" | "system" | ... (provenance, not navigation)
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	// Inference explains why Ghost chose this Kind. It is diagnostic
	// metadata, not something the owner must act on.
	KindReason string `json:"kind_reason,omitempty"`
}

// FromRoutine normalizes a routine product view into a Thing.
func FromRoutine(r *routines.Routine) Thing {
	if r == nil {
		return Thing{}
	}
	sched := humanRoutineSchedule(r)
	return Thing{
		ID:        r.ID,
		Title:     r.Name,
		What:      r.Instruction,
		Kind:      KindRoutine,
		State:     normalizeRoutineState(r.Status),
		Schedule:  sched,
		NextRunAt: r.NextRun,
		LastRunAt: r.LastRun,
		Source:    "routine",
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		// A routine is always recurring work by construction; the sidecar
		// instruction is the "what", so no shape inference is needed.
		KindReason: "recurring instruction",
	}
}

// FromScheduled normalizes a raw scheduled item into a Thing. This is the
// path used for non-routine items (reminders, one-off automations, tasks).
func FromScheduled(item *scheduled.ScheduledItem) Thing {
	if item == nil {
		return Thing{}
	}
	kind, reason := inferKind(item)
	return Thing{
		ID:         item.ID,
		Title:      item.Title,
		What:       item.Action.Content,
		Kind:       kind,
		State:      normalizeItemState(item.State),
		Schedule:   humanItemSchedule(item),
		NextRunAt:  item.NextRunAt,
		LastRunAt:  item.LastRunAt,
		RunCount:   item.RunCount,
		LastError:  item.LastError,
		Source:     item.Source,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
		KindReason: reason,
	}
}

// inferKind deterministically classifies a scheduled item. It is total:
// every item maps to exactly one Kind, and the reason is human-readable so
// the classification is auditable rather than magic.
func inferKind(item *scheduled.ScheduledItem) (Kind, string) {
	recurring := item.IsRecurring()
	switch {
	case item.Type == scheduled.TypeReminder:
		return KindReminder, "one-time reminder"
	case item.Type == scheduled.TypeTask:
		return KindTask, "standalone task"
	case item.Type == scheduled.TypeAutomation && recurring:
		return KindAutomation, "recurring action"
	case item.Type == scheduled.TypeAutomation && !recurring:
		return KindAutomation, "scheduled action"
	case item.Type == scheduled.TypeEvent:
		return KindReminder, "scheduled event"
	case recurring:
		return KindRoutine, "recurring work"
	default:
		return KindTask, "unscheduled work"
	}
}

func normalizeRoutineState(s routines.Status) State {
	switch s {
	case routines.StatusActive:
		return StateActive
	case routines.StatusPaused:
		return StatePaused
	case routines.StatusWaiting:
		return StateWaiting
	case routines.StatusCompleted:
		return StateDone
	case routines.StatusFailed:
		return StateFailed
	case routines.StatusCancelled:
		return StateCancelled
	default:
		return StateActive
	}
}

func normalizeItemState(s scheduled.ItemState) State {
	switch s {
	case scheduled.StateScheduled, scheduled.StateDue, scheduled.StateRunning:
		return StateActive
	case scheduled.StatePaused:
		return StatePaused
	case scheduled.StateCompleted:
		return StateDone
	case scheduled.StateFailed, scheduled.StateMissed:
		return StateFailed
	case scheduled.StateCancelled:
		return StateCancelled
	default:
		return StateActive
	}
}

// humanRoutineSchedule renders a routine's schedule as a sentence. It
// reuses the scheduled package's humanizer so the console, phone, and
// routine surfaces never disagree about what "0 9 * * 1-5" means.
func humanRoutineSchedule(r *routines.Routine) string {
	switch r.ScheduleKind {
	case "cron":
		item := scheduled.ScheduledItem{Schedule: scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: r.ScheduleExpr}}
		return item.HumanSchedule()
	case "every":
		item := scheduled.ScheduledItem{Schedule: scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Duration(r.ScheduleEverySecs) * time.Second}}
		return item.HumanSchedule()
	case "at":
		if t, err := time.Parse(time.RFC3339, r.ScheduleExpr); err == nil {
			item := scheduled.ScheduledItem{Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &t}}
			return item.HumanSchedule()
		}
	}
	return "Manual"
}

func humanItemSchedule(item *scheduled.ScheduledItem) string {
	if item.Schedule.Kind == "" || item.Schedule.Kind == scheduled.ScheduleNone {
		return "Manual"
	}
	return item.HumanSchedule()
}

// List merges routines and scheduled items into one normalized, sorted
// list. Routines win on ID collision because the routine view carries the
// product metadata (status, next run) that the raw item lacks.
//
// Ordering is deterministic: active items first, then by soonest next run,
// then newest created, then ID. This makes the feed stable across calls.
func List(routineList []*routines.Routine, scheduledList []*scheduled.ScheduledItem) []Thing {
	seen := make(map[string]bool, len(routineList))
	out := make([]Thing, 0, len(routineList)+len(scheduledList))

	for _, r := range routineList {
		if r == nil || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		out = append(out, FromRoutine(r))
	}
	for _, item := range scheduledList {
		if item == nil || seen[item.ID] {
			continue
		}
		// Routine-sourced items are represented by their routine view;
		// the raw row would be a duplicate with less information.
		if item.Source == "routine" {
			continue
		}
		seen[item.ID] = true
		out = append(out, FromScheduled(item))
	}

	sort.SliceStable(out, func(i, j int) bool {
		return less(out[i], out[j])
	})
	return out
}

func less(a, b Thing) bool {
	aActive := a.State == StateActive || a.State == StateWaiting
	bActive := b.State == StateActive || b.State == StateWaiting
	if aActive != bActive {
		return aActive
	}
	switch {
	case a.NextRunAt != nil && b.NextRunAt != nil && !a.NextRunAt.Equal(*b.NextRunAt):
		return a.NextRunAt.Before(*b.NextRunAt)
	case a.NextRunAt != nil && b.NextRunAt == nil:
		return true
	case a.NextRunAt == nil && b.NextRunAt != nil:
		return false
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID < b.ID
}

// Summary is a one-line, user-safe description for compact surfaces (home
// cards, notifications). It never contains credentials or message bodies
// beyond the Thing's own instruction.
func Summary(t Thing) string {
	s := strings.TrimSpace(t.Schedule)
	if s == "" {
		s = "Manual"
	}
	if t.State == StateWaiting {
		return fmt.Sprintf("%s · waiting for you", s)
	}
	if t.State == StatePaused {
		return fmt.Sprintf("%s · paused", s)
	}
	if t.RunCount > 0 {
		return fmt.Sprintf("%s · ran %d×", s, t.RunCount)
	}
	return s
}
