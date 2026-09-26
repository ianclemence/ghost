// Package ideas implements the ideas-with-evidence loop: deterministic,
// source-cited suggestions a normal user can trust, with accept/dismiss
// receipts. Every idea cites the exact rows behind it (memory entries,
// routine runs); nothing is asserted from prose.
//
// Phase A (this package): deterministic generators over caller-supplied
// signals. Phase B (generate.go): model-drafted ideas whose citations are
// verified against the stores; unverified drafts render as such.
package ideas

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SourceKind names what backs an idea.
type SourceKind string

const (
	SourceMemory  SourceKind = "memory"
	SourceRoutine SourceKind = "routine"
	SourceModel   SourceKind = "model"
	// Proactive observations cite the subsystem rows they were derived from.
	SourceSchedule SourceKind = "schedule"
	SourceGoal     SourceKind = "goal"
	SourceTask     SourceKind = "task"
)

// Source is one cited row behind an idea.
type Source struct {
	Kind    SourceKind `json:"kind"`
	Ref     string     `json:"ref"`               // memory entry id, routine run id, model run id
	Excerpt string     `json:"excerpt,omitempty"` // short human-readable evidence
}

// Status is the decision state of an idea. The first three are the original
// owner-decision vocabulary; the rest are the durable opportunity lifecycle
// the proactive runtime drives. "pending" and "presented" both mean the owner
// has not decided yet — pending is a candidate the runtime has not surfaced.
type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusDismissed Status = "dismissed"

	// Proactive lifecycle. Persisted transitions only; every move is a
	// record, never a rewrite of history.
	StatusPresented  Status = "presented"  // surfaced to the owner, awaiting decision
	StatusSnoozed    Status = "snoozed"    // deliberately deferred until SnoozedUntil
	StatusExpired    Status = "expired"    // its window closed before it was answered
	StatusExecuting  Status = "executing"  // approved and running right now
	StatusCompleted  Status = "completed"  // the action ran and runtime evidence proved it
	StatusFailed     Status = "failed"     // the action ran and failed, or verification failed
	StatusSuperseded Status = "superseded" // the underlying state changed; the proposal is void
)

// Undecided reports whether the owner can still act on an idea.
func (s Status) Undecided() bool {
	return s == StatusPending || s == StatusPresented || s == StatusSnoozed
}

// Idea is one suggestion with its evidence and decision receipt.
//
// The first block is the original Phase A/B shape (unchanged). The second
// block carries the proactive lifecycle: a structured, runtime-verified
// proposal rather than a bare sentence of advice.
type Idea struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Body       string    `json:"body"`
	Sources    []Source  `json:"sources"`
	Status     Status    `json:"status"`
	Action     string    `json:"action,omitempty"` // safe reversible op the CLI may run on accept, e.g. "pause:routine-id"
	CreatedAt  time.Time `json:"created_at"`
	DecidedAt  *time.Time `json:"decided_at,omitempty"`
	Unverified bool      `json:"unverified,omitempty"` // Phase B: citations failed verification

	// --- proactive proposal lifecycle ---

	// Kind is the opportunity category (see ObsKind). It drives per-category
	// throttling and owner controls.
	Kind ObsKind `json:"kind,omitempty"`
	// Reason states why this matters now, in the runtime's own words. It is
	// never model prose: it is rendered from the observation.
	Reason string `json:"reason,omitempty"`
	// Plan is the concrete action the runtime will take on approval. Nil means
	// the idea is informational only and nothing can execute from it.
	Plan *Plan `json:"plan,omitempty"`
	// Priority/Urgency/Confidence are the deterministic scoring inputs, kept
	// on the record so the runtime can explain every interruption.
	Priority   int     `json:"priority,omitempty"`
	Urgency    bool    `json:"urgency,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	// DedupeKey identifies the underlying opportunity. The same key never
	// produces a second live idea.
	DedupeKey string `json:"dedupe_key,omitempty"`
	// ExpiresAt bounds how long the proposal is answerable. Past it the idea
	// is expired and cannot execute.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// StateVer is a digest of the runtime state the proposal was built from.
	// If the state changes materially, the approval is void (superseded).
	StateVer string `json:"state_version,omitempty"`
	// PermissionRequestID is the broker request bound to this proposal. The
	// owner's approval resolves that exact request; the capability executes
	// through the same governed path as any other authorized action.
	PermissionRequestID string `json:"permission_request_id,omitempty"`
	// Risk is the runtime-classified risk of the plan (never model-declared).
	Risk string `json:"risk,omitempty"`

	PresentedAt  *time.Time `json:"presented_at,omitempty"`
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty"`
	// Outcome/Result are written only from runtime evidence after execution.
	Outcome string `json:"outcome,omitempty"`
	Result  string `json:"result,omitempty"`
}

// Store is a workspace-scoped JSONL idea store.
type Store struct {
	path string
	mu   sync.Mutex
}

// New opens (creating) the store under workspace/state.
func New(workspace string) (*Store, error) {
	dir := filepath.Join(workspace, "state")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, "ideas.jsonl")}, nil
}

// MemoryFact is one caller-supplied memory row for generation.
type MemoryFact struct {
	ID        string
	Predicate string
	Value     string
	UpdatedAt time.Time
}

// RoutineRun is one caller-supplied routine execution record.
type RoutineRun struct {
	RunID       string
	RoutineID   string
	RoutineName string
	Status      string // ok | error | missed | cancelled
	Error       string
	At          time.Time
}

// RoutineMeta is one caller-supplied routine for staleness checks.
type RoutineMeta struct {
	ID       string
	Name     string
	Status   string
	LastRun  *time.Time
	NextRun  *time.Time
	Timezone string
}

// Signals is everything generation may cite. Callers assemble it from the
// real stores; this package never opens them, so generation stays pure.
type Signals struct {
	Memories []MemoryFact
	Runs     []RoutineRun
	Routines []RoutineMeta
	Now      time.Time
}

// Generate derives deterministic ideas from signals, skipping anything an
// existing pending or accepted idea already cites (dedupe by source ref).
func Generate(sig Signals, existing []Idea) []Idea {
	now := sig.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cited := map[string]bool{}
	for _, e := range existing {
		if e.Status == StatusDismissed {
			continue
		}
		for _, s := range e.Sources {
			cited[string(s.Kind)+":"+s.Ref] = true
		}
	}
	var out []Idea
	// Rule 1: a routine whose latest runs errored. Highest value, cites rows.
	byRoutine := map[string][]RoutineRun{}
	for _, r := range sig.Runs {
		byRoutine[r.RoutineID] = append(byRoutine[r.RoutineID], r)
	}
	for rid, runs := range byRoutine {
		sort.Slice(runs, func(i, j int) bool { return runs[i].At.After(runs[j].At) })
		errs := 0
		for _, r := range runs {
			if now.Sub(r.At) > 7*24*time.Hour {
				break
			}
			if r.Status == "error" {
				errs++
			} else {
				break
			}
		}
		if errs == 0 {
			continue
		}
		latest := runs[0]
		ref := string(SourceRoutine) + ":" + latest.RunID
		if cited[ref] {
			continue
		}
		name := latest.RoutineName
		if name == "" {
			name = rid
		}
		body := fmt.Sprintf("Its latest run failed %s.", latest.At.Local().Format("Mon 15:04"))
		if latest.Error != "" {
			body += " Reported: " + truncate(latest.Error, 160)
		}
		out = append(out, Idea{
			ID:     newID(),
			Title:  fmt.Sprintf("Your %q routine keeps failing — pause it?", name),
			Body:   body + " Accepting pauses it; you can resume anytime with /task.",
			Sources: []Source{{
				Kind: SourceRoutine, Ref: latest.RunID,
				Excerpt: fmt.Sprintf("run %s status=%s", shortRef(latest.RunID), latest.Status),
			}},
			Status:    StatusPending,
			Action:    "pause:" + rid,
			CreatedAt: now,
		})
	}
	// Rule 2: a standing goal without check-ins. Offer them, cite the goal.
	for _, m := range sig.Memories {
		if m.Predicate != "goal/primary" {
			continue
		}
		ref := string(SourceMemory) + ":" + m.ID
		if cited[ref] {
			continue
		}
		out = append(out, Idea{
			ID:    newID(),
			Title: fmt.Sprintf("Want check-ins for %q?", truncate(m.Value, 60)),
			Body: "Accepting suggests a weekly check-in routine (you confirm the schedule before anything is created — Ghost never automates silently).",
			Sources: []Source{{
				Kind: SourceMemory, Ref: m.ID,
				Excerpt: fmt.Sprintf("%s: %s", m.Predicate, truncate(m.Value, 80)),
			}},
			Status:    StatusPending,
			CreatedAt: now,
		})
	}
	// Rule 3: a routine idle 30+ days with a future schedule. Offer pause/delete.
	for _, r := range sig.Routines {
		if r.Status != "active" || r.LastRun == nil || r.NextRun == nil {
			continue
		}
		if now.Sub(*r.LastRun) < 30*24*time.Hour {
			continue
		}
		ref := string(SourceRoutine) + ":stale:" + r.ID
		if cited[ref] {
			continue
		}
		name := r.Name
		if name == "" {
			name = r.ID
		}
		out = append(out, Idea{
			ID:    newID(),
			Title: fmt.Sprintf("%q hasn't run in %d days — pause it?", name, int(now.Sub(*r.LastRun).Hours()/24)),
			Body:  "It's still scheduled. Accepting pauses it; resume anytime with /task.",
			Sources: []Source{{
				Kind: SourceRoutine, Ref: ref,
				Excerpt: fmt.Sprintf("last run %s", r.LastRun.Local().Format("2006-01-02")),
			}},
			Status:    StatusPending,
			Action:    "pause:" + r.ID,
			CreatedAt: now,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortRef(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
