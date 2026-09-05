// Package routines promotes the existing scheduler into Ghost's
// product-level routine architecture — without replacing it.
//
// A routine is a persistent instruction for Ghost to perform work in the
// future ("Every Monday at 9 AM, prepare my weekly brief"). Storage
// reuses scheduled.Store (no duplicate scheduling tables): a routine IS
// a scheduled automation item plus a metadata sidecar (instruction,
// allowed capabilities, ownership). Execution reuses the SAME pipeline
// as interactive requests:
//
// Scheduler → Routine → Ghost context → Brain → Memory → Capabilities →
// Permission Broker → Execution → Event Stream → Notification/Result.
//
// Unattended permission semantics: a routine CANNOT inherit unlimited
// permissions. NEEDS_PERMISSION pauses the run (routine.waiting +
// event + preserved state) instead of hanging; the user approves/denies
// from the activity surface and the run resumes or cancels.
package routines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Status is the product-level routine state.
type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusWaiting   Status = "waiting" // blocked on permission/configuration
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
)

// Routine is the product view over a scheduled automation item.
type Routine struct {
	ID                  string     `json:"id"`
	GhostID             string     `json:"ghost_id"`
	OwnerID             string     `json:"owner_id"`
	Name                string     `json:"name"`
	Instruction         string     `json:"instruction"`
	Timezone            string     `json:"timezone"`
	Status              Status     `json:"status"`
	AllowedCapabilities []string   `json:"allowed_capabilities,omitempty"`
	ScheduleKind        string     `json:"schedule_kind,omitempty"`
	ScheduleExpr        string     `json:"schedule_expr,omitempty"`
	ScheduleEverySecs   int64      `json:"schedule_every_seconds,omitempty"`
	NextRun             *time.Time `json:"next_run,omitempty"`
	LastRun             *time.Time `json:"last_run,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// RunOutcome is what one routine execution produced (canonical result,
// consumed by the event stream + activity, never inferred from text).
type RunOutcome struct {
	Completion product.Completion `json:"completion"`
	Message    string             `json:"message"`
	WaitingOn  string             `json:"waiting_on,omitempty"` // permission id / config action
}

// Executor runs the routine's work through the standard pipeline and
// returns the canonical outcome. The routines package owns
// started/waiting/completed/failed transitions around it.
type Executor func(ctx context.Context, r *Routine) RunOutcome

// Service adapts scheduled.Store into routines.
type Service struct {
	db    *sql.DB
	store *scheduled.Store
}

// New creates the service; routine_meta sidecar lives beside scheduled
// tables in the same SQLite database.
func New(db *sql.DB, store *scheduled.Store) (*Service, error) {
	s := &Service{db: db, store: store}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS routine_meta (
		item_id TEXT PRIMARY KEY, ghost_id TEXT, owner_id TEXT,
		instruction TEXT, allowed_capabilities TEXT,
		created_at TEXT, updated_at TEXT
	)`)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Create persists a routine as a scheduled automation + metadata. The
// instruction must name WHAT to do; schedule/timezone come from the
// parsed schedule. Allowed capabilities scope unattended execution.
func (s *Service) Create(ghostID, ownerID, name, instruction, timezone string, sched scheduled.Schedule, allowed []string) (*Routine, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(instruction) == "" {
		return nil, errors.New("routine name and instruction required")
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, errors.New("unknown timezone")
	}
	now := time.Now()
	item := &scheduled.ScheduledItem{
		ID:          "routine-" + uuid.NewString()[:8],
		Type:        scheduled.TypeAutomation,
		Title:       strings.TrimSpace(name),
		Description: strings.TrimSpace(instruction),
		State:       scheduled.StateScheduled,
		Schedule:    sched,
		Timezone:    timezone,
		Action:      scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: strings.TrimSpace(instruction)},
		Source:      "routine",
		CreatedBy:   ownerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.Create(item); err != nil {
		return nil, err
	}
	ac, _ := json.Marshal(allowed)
	_, err := s.db.Exec(`INSERT INTO routine_meta (item_id, ghost_id, owner_id, instruction, allowed_capabilities, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, item.ID, ghostID, ownerID, item.Description, string(ac),
		now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return s.Get(item.ID)
}

// Get loads the product view.
func (s *Service) Get(id string) (*Routine, error) {
	item, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}
	var ghostID, ownerID, instruction, ac string
	var created, updated string
	err = s.db.QueryRow(`SELECT ghost_id, owner_id, instruction, allowed_capabilities, created_at, updated_at FROM routine_meta WHERE item_id=?`, id).
		Scan(&ghostID, &ownerID, &instruction, &ac, &created, &updated)
	if err != nil {
		return nil, errors.New("routine metadata not found")
	}
	r := &Routine{ID: item.ID, GhostID: ghostID, OwnerID: ownerID,
		Name: item.Title, Instruction: instruction, Timezone: item.Timezone,
		Status: statusOf(item.State), NextRun: item.NextRunAt, LastRun: item.LastRunAt}
	switch item.Schedule.Kind {
	case scheduled.ScheduleCron:
		r.ScheduleKind = "cron"
		r.ScheduleExpr = item.Schedule.Expr
	case scheduled.ScheduleEvery:
		r.ScheduleKind = "every"
		r.ScheduleEverySecs = int64(item.Schedule.Every / time.Second)
	case scheduled.ScheduleAt:
		r.ScheduleKind = "at"
		if item.Schedule.At != nil {
			r.ScheduleExpr = item.Schedule.At.Format(time.RFC3339)
		}
	}
	_ = json.Unmarshal([]byte(ac), &r.AllowedCapabilities)
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		r.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updated); err == nil {
		r.UpdatedAt = t
	}
	return r, nil
}

// List returns routines for a Ghost (product view, newest first).
func (s *Service) List(ghostID string, limit int) []*Routine {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT item_id FROM routine_meta WHERE ghost_id=? ORDER BY created_at DESC LIMIT ?`, ghostID, limit)
	if err != nil {
		return nil
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []*Routine
	for _, id := range ids {
		if r, err := s.Get(id); err == nil {
			out = append(out, r)
		}
	}
	return out
}

// Pause / Resume / Cancel mutate scheduler state through the one engine.
func (s *Service) Pause(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.store.UpdateState(id, scheduled.StatePaused)
}

func (s *Service) Resume(id string) error {
	r, err := s.Get(id)
	if err != nil {
		return err
	}
	if r.Status == StatusCompleted || r.Status == StatusCancelled {
		return errors.New("finished routines cannot resume")
	}
	return s.store.UpdateState(id, scheduled.StateScheduled)
}

func (s *Service) Cancel(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.store.UpdateState(id, scheduled.StateCancelled)
}

// Delete removes the item and its metadata.
func (s *Service) Delete(id string) error {
	if _, err := s.db.Exec(`DELETE FROM routine_meta WHERE item_id=?`, id); err != nil {
		return err
	}
	return s.store.Delete(id)
}

// Run executes one routine occurrence through the standard pipeline.
// Outcome mapping: SUCCESS→completed, NEEDS_PERMISSION/CONFIG/AUTH→
// waiting (state preserved, run resumable), FAILED→failed, CANCELLED→
// cancelled. It never hangs on approval and never double-executes: the
// caller passes the occurrence's idempotency key.
func (s *Service) Run(ctx context.Context, id, executionKey string, exec Executor) (RunOutcome, error) {
	r, err := s.Get(id)
	if err != nil {
		return RunOutcome{}, err
	}
	if r.Status == StatusPaused || r.Status == StatusCancelled || r.Status == StatusCompleted {
		return RunOutcome{}, errors.New("routine is not runnable")
	}
	dup, err := s.store.HasExecution(executionKey)
	if err != nil {
		return RunOutcome{}, err
	}
	if dup {
		return RunOutcome{}, errors.New("duplicate execution (idempotency key seen)")
	}
	now := time.Now()
	_ = s.store.RecordExecution(&scheduled.ExecutionRecord{
		ItemID: id, ExecutionID: executionKey,
		ScheduledAt: now, StartedAt: now, Status: "running",
	})
	_ = s.store.UpdateState(id, scheduled.StateRunning)
	outcome := exec(ctx, r)
	finalize := func(status string) {
		completed := now
		_ = s.store.RecordExecution(&scheduled.ExecutionRecord{
			ItemID: id, ExecutionID: executionKey + ":done",
			ScheduledAt: now, StartedAt: now, CompletedAt: &completed, Status: status,
		})
	}
	switch outcome.Completion {
	case product.CompletionSuccess:
		_ = s.store.UpdateState(id, scheduled.StateCompleted)
		finalize("ok")
	case product.CompletionCancelled:
		_ = s.store.UpdateState(id, scheduled.StateCancelled)
		finalize("cancelled")
	case product.CompletionWaitingForConfig, product.CompletionWaitingForAuth,
		product.CompletionWaitingForPermission, product.CompletionWaitingForUser:
		// Preserve runnability: back to scheduled so the next trigger or
		// an approval resume can fire it; waiting is a product overlay.
		_ = s.store.UpdateState(id, scheduled.StateScheduled)
		finalize("waiting")
	default:
		_ = s.store.UpdateState(id, scheduled.StateFailed)
		finalize("error")
	}
	r2, _ := s.Get(id)
	_ = r2
	return outcome, nil
}

// statusOf maps scheduler state to the product overlay.
func statusOf(st scheduled.ItemState) Status {
	switch st {
	case scheduled.StatePaused:
		return StatusPaused
	case scheduled.StateCancelled:
		return StatusCancelled
	case scheduled.StateCompleted:
		return StatusCompleted
	case scheduled.StateFailed:
		return StatusFailed
	case scheduled.StateRunning:
		return StatusActive
	default:
		return StatusActive
	}
}
