package scheduled

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/adhocore/gronx"
	"github.com/ianclemence/ghost/pkg/clock"
)

// Executor is the function signature for executing a scheduled item.
type Executor func(ctx context.Context, item *ScheduledItem) error

// EventBus is a simple event publisher for typed events.
type EventBus interface {
	Publish(topic string, payload interface{})
}

// Service is the core scheduler that manages scheduled items.
type Service struct {
	store    *Store
	events   EventBus
	executor Executor
	mu       sync.RWMutex
	stopChan chan struct{}
	running  bool
	// ClockGate, when set, is consulted on every tick: a false return
	// means wall time is untrustworthy and due items must not fire this
	// cycle. Nil means always fire (tests, callers without a clock).
	ClockGate func() bool
	// MissedNotice, when set, is told about a recurring run that was skipped
	// because Ghost was off when it came due, so the owner can be told.
	MissedNotice func(item *ScheduledItem, due time.Time)
	// OnMissed, when set, hears about a one-time item that came due more than
	// graceWindow ago (Ghost was off) and was marked missed instead of fired.
	OnMissed func(item *ScheduledItem, due time.Time)
	// clockBlocked remembers the gate state to log transitions once
	// instead of every second.
	clockBlocked bool
	clockWatch   clock.Watcher
}

// NewService creates a new scheduler service.
func NewService(store *Store, events EventBus, executor Executor) *Service {
	return &Service{
		store:    store,
		events:   events,
		executor: executor,
		stopChan: make(chan struct{}),
	}
}

// Start begins the scheduler tick loop.
func (s *Service) Start() error {
	if err := s.store.InitSchema(); err != nil {
		return err
	}
	if n := s.RepairMissingNextRun(time.Now().UTC()); n > 0 {
		log.Printf("[scheduled] gave %d waiting item(s) the next run they were missing", n)
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	go s.runLoop()
	return nil
}

// Stop halts the scheduler.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopChan)
}

// runLoop is the main scheduler tick loop.
func (s *Service) runLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick checks for due items and executes them.
func (s *Service) tick() {
	now := time.Now().UTC()

	// A stepped wall clock explains every "everything fired at once"
	// mystery; record it loudly but keep firing semantics unchanged.
	if s.clockWatch.Observe(now, time.Now()) {
		log.Printf("[scheduled] wall-clock jump detected; due items evaluate against the new time")
	}
	if s.ClockGate != nil && !s.ClockGate() {
		s.mu.Lock()
		blocked := !s.clockBlocked
		s.clockBlocked = true
		s.mu.Unlock()
		if blocked {
			log.Printf("[scheduled] wall clock untrusted; holding scheduled items until time is sane")
		}
		return
	}
	s.mu.Lock()
	s.clockBlocked = false
	s.mu.Unlock()

	items, err := s.store.ListDue(now)
	if err != nil {
		log.Printf("[scheduled] failed to list due items: %v", err)
		return
	}

	for _, item := range items {
		// A recurring run that came due while Ghost was off is not replayed
		// hours later (a morning brief at 9pm is noise). Commitments to the
		// owner, such as reminders, are delivered late with a note instead; that
		// is the policy table's call, not ours.
		if !item.IsOneTime() && item.NextRunAt != nil && now.Sub(*item.NextRunAt) > missedRecurringGrace {
			d := ClassifyMissed(item.Type, "", *item.NextRunAt, now)
			if !d.ShouldRun && !d.NotifyUser {
				due := *item.NextRunAt
				s.handleMissedRecurring(item, now)
				if s.MissedNotice != nil {
					s.MissedNotice(item, due)
				}
				continue
			}
		}
		// A one-time item a day or more past its time is not delivered now as if
		// it were fresh: "remind me at 9 to call" a day later is noise. It is
		// recorded as missed and the owner is told once.
		if item.IsOneTime() && item.NextRunAt != nil && now.Sub(*item.NextRunAt) > graceWindow {
			if err := s.store.UpdateState(item.ID, StateMissed); err != nil {
				log.Printf("[scheduled] failed to mark item missed: %v", err)
				continue
			}
			if s.OnMissed != nil {
				s.OnMissed(item, *item.NextRunAt)
			}
			continue
		}
		// Transition the item to running synchronously so a subsequent tick can
		// never re-list it and fire it a second time while execution is in flight.
		if err := s.store.UpdateState(item.ID, StateRunning); err != nil {
			log.Printf("[scheduled] failed to mark item running: %v", err)
			continue
		}

		// Execute asynchronously
		go s.runItemSafely(item)
	}
}

// missedRecurringGrace is how late a recurring run may be and still run: enough
// for a slow boot or a busy tick, not enough to replay yesterday.
const missedRecurringGrace = 30 * time.Minute

// runItemSafely runs one scheduled item in its own goroutine and contains a
// panic so a single failing executor cannot crash the whole personal AI. The
// item's execution state is left for the store's own retry/expiry handling.
func (s *Service) runItemSafely(item *ScheduledItem) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduled] panic executing item %s: %v", item.ID, r)
		}
	}()
	s.executeItem(item)
}

// executeItem runs a scheduled item.
func (s *Service) executeItem(item *ScheduledItem) {
	// Check for idempotency
	execID := generateExecutionID(item)
	exists, err := s.store.HasExecution(execID)
	if err != nil {
		log.Printf("[scheduled] failed to check execution: %v", err)
		return
	}
	if exists {
		log.Printf("[scheduled] skipping duplicate execution %s for item %s", execID, item.ID)
		return
	}

	// Mark as running
	if err := s.store.UpdateState(item.ID, StateRunning); err != nil {
		log.Printf("[scheduled] failed to mark item running: %v", err)
		return
	}

	// Record execution start. Channel is always known; delivery starts
	// as dispatched and is upgraded below once the outcome is known.
	// Delivery semantics by executor path:
	//   - bus reminders/automations are async (the turn runs later via the
	//     message bus), so "dispatched" is the truthful terminal state here.
	//   - routines and shell commands run synchronously inside the
	//     executor, so a nil error means the result was produced: delivered.
	record := &ExecutionRecord{
		ItemID:         item.ID,
		ExecutionID:    execID,
		ScheduledAt:    *item.NextRunAt,
		StartedAt:      time.Now().UTC(),
		Status:         "ok",
		Channel:        item.Channel,
		DeliveryStatus: "dispatched",
	}

	// Publish event
	if s.events != nil {
		s.events.Publish("schedule.started", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
		})
	}

	// Execute
	ctx := context.Background()
	err = s.executor(ctx, item)

	// Record completion
	now := time.Now().UTC()
	record.CompletedAt = &now
	if err != nil {
		record.Status = "error"
		record.Error = err.Error()
		record.DeliveryStatus = "failed"
		log.Printf("[scheduled] execution failed for item %s: %v", item.ID, err)
	} else {
		record.Status = "ok"
		if item.Source == "routine" || item.Action.Kind == ActionCommand {
			record.DeliveryStatus = "delivered"
			record.DeliveredAt = &now
		}
	}

	if err := s.store.RecordExecution(record); err != nil {
		log.Printf("[scheduled] failed to record execution: %v", err)
	}

	// Update item state
	if err := s.store.IncrementRunCount(item.ID); err != nil {
		log.Printf("[scheduled] failed to increment run count: %v", err)
	}

	// Re-read the row before handling success/failure: both handlers
	// persist the in-memory struct via Update, which would clobber the
	// just-incremented run_count/last_run_at (and the running→scheduled
	// transition) with stale zeros. The fresh row is authoritative.
	if fresh, gerr := s.store.Get(item.ID); gerr == nil && fresh != nil {
		item = fresh
	} else {
		item.RunCount++
		item.LastRunAt = &now
	}

	if err != nil {
		// Handle failure
		s.handleFailure(item, err)
	} else {
		// Handle success
		s.handleSuccess(item)
	}
}

// handleSuccess processes a successful execution.
func (s *Service) handleSuccess(item *ScheduledItem) {
	// Explicit opt-in only: the row really is consumed after one run.
	if item.DeleteAfterRun {
		if err := s.store.Delete(item.ID); err != nil {
			log.Printf("[scheduled] failed to delete one-time item: %v", err)
		}
		if s.events != nil {
			s.events.Publish("schedule.completed", map[string]interface{}{
				"item_id": item.ID,
				"type":    item.Type,
				"title":   item.Title,
			})
		}
		return
	}

	// Fired work stays on the record: a one-shot becomes StateCompleted
	// instead of vanishing, so "check my reminders" can show what already
	// fired — not just what is still pending. ListDue only ever selects
	// scheduled/due, so a completed row can never re-fire. Retention is
	// bounded by pruneCompleted.
	if item.IsOneTime() {
		item.State = StateCompleted
		if err := s.store.Update(item); err != nil {
			log.Printf("[scheduled] failed to mark item completed: %v", err)
		}
		s.pruneCompleted()
		if s.events != nil {
			s.events.Publish("schedule.completed", map[string]interface{}{
				"item_id": item.ID,
				"type":    item.Type,
				"title":   item.Title,
			})
		}
		return
	}

	// Recurring item: compute next run
	nextRun := s.computeNextRun(item)
	if nextRun == nil {
		// No more runs: everything it ever will do, it has done.
		item.State = StateCompleted
		if err := s.store.Update(item); err != nil {
			log.Printf("[scheduled] failed to mark exhausted item completed: %v", err)
		}
		s.pruneCompleted()
		return
	}

	item.NextRunAt = nextRun
	item.State = StateScheduled
	if err := s.store.Update(item); err != nil {
		log.Printf("[scheduled] failed to update next run: %v", err)
	}

	if s.events != nil {
		s.events.Publish("schedule.completed", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
			"next_at": nextRun,
		})
	}
}

// completedHistoryKeep bounds how many completed items stay on the
// schedule table. Enough for "did it fire?" answers, never unbounded.
const completedHistoryKeep = 100

// pruneCompleted keeps retained completed history bounded. Best-effort:
// a prune failure never affects scheduling, only table size.
func (s *Service) pruneCompleted() {
	n, err := s.store.PruneCompleted(completedHistoryKeep)
	if err != nil {
		log.Printf("[scheduled] failed to prune completed items: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[scheduled] pruned %d old completed items", n)
	}
}

// handleFailure processes a failed execution.
func (s *Service) handleFailure(item *ScheduledItem, err error) {
	item.LastError = err.Error()
	item.RetryCount++

	if item.RetryCount >= item.MaxRetries {
		// Max retries exceeded: mark as failed
		item.State = StateFailed
		if err := s.store.Update(item); err != nil {
			log.Printf("[scheduled] failed to update item state: %v", err)
		}
		if s.events != nil {
			s.events.Publish("schedule.failed", map[string]interface{}{
				"item_id": item.ID,
				"type":    item.Type,
				"title":   item.Title,
				"error":   err.Error(),
			})
		}
		return
	}

	// Schedule retry with exponential backoff
	backoff := time.Duration(item.RetryCount) * time.Minute
	nextRun := time.Now().UTC().Add(backoff)
	item.NextRunAt = &nextRun
	item.State = StateScheduled
	if err := s.store.Update(item); err != nil {
		log.Printf("[scheduled] failed to schedule retry: %v", err)
	}
}

// FirstRun is when a newly created schedule should first fire. Every path that
// creates an item must set it: ListDue only selects items with a next run, so
// an item without one is never fired and never reported missed. Routines were
// created that way and sat "active" without ever running.
func FirstRun(sched Schedule, tz string, now time.Time) *time.Time {
	switch sched.Kind {
	case ScheduleAt:
		if sched.At == nil {
			return nil
		}
		at := sched.At.UTC()
		return &at
	case ScheduleEvery:
		if sched.Every <= 0 {
			return nil
		}
		next := now.UTC().Add(sched.Every)
		return &next
	case ScheduleCron:
		return computeCronNextRun(sched.Expr, tz, now)
	}
	return nil
}

// RepairMissingNextRun gives every waiting item without a next run the one it
// should have had, so nothing created before FirstRun existed stays silent.
// Idempotent; returns how many items it fixed.
func (s *Service) RepairMissingNextRun(now time.Time) int {
	items, err := s.store.List("", StateScheduled, 1000)
	if err != nil {
		return 0
	}
	fixed := 0
	for _, it := range items {
		if it == nil || it.NextRunAt != nil {
			continue
		}
		next := FirstRun(it.Schedule, it.Timezone, now)
		if next == nil {
			continue
		}
		it.NextRunAt = next
		if err := s.store.Update(it); err != nil {
			log.Printf("[scheduled] failed to repair next run for %s: %v", it.ID, err)
			continue
		}
		fixed++
	}
	return fixed
}

// computeNextRun calculates the next run time for a recurring item.
func (s *Service) computeNextRun(item *ScheduledItem) *time.Time {
	now := time.Now().UTC()

	switch item.Schedule.Kind {
	case ScheduleEvery:
		next := now.Add(item.Schedule.Every)
		return &next
	case ScheduleCron:
		// Use gronx to compute next run
		return computeCronNextRun(item.Schedule.Expr, item.Timezone, now)
	default:
		return nil
	}
}

// handleMissedRecurring computes the next valid occurrence after downtime.
func (s *Service) handleMissedRecurring(item *ScheduledItem, now time.Time) {
	nextRun := s.computeNextRun(item)
	if nextRun == nil {
		if err := s.store.Delete(item.ID); err != nil {
			log.Printf("[scheduled] failed to delete exhausted item: %v", err)
		}
		return
	}

	item.NextRunAt = nextRun
	item.State = StateScheduled
	if err := s.store.Update(item); err != nil {
		log.Printf("[scheduled] failed to update next run: %v", err)
	}
}

// CRUD methods that delegate to the store

// CreateItem creates a new scheduled item.
func (s *Service) CreateItem(item *ScheduledItem) error {
	if item.NextRunAt == nil && (item.State == "" || item.State == StateScheduled) {
		item.NextRunAt = FirstRun(item.Schedule, item.Timezone, time.Now().UTC())
	}
	if err := s.store.Create(item); err != nil {
		return err
	}
	if s.events != nil {
		s.events.Publish("schedule.created", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
		})
	}
	return nil
}

// GetItem retrieves a scheduled item by ID.
func (s *Service) GetItem(id string) (*ScheduledItem, error) {
	return s.store.Get(id)
}

// ListItems retrieves scheduled items with optional filters.
func (s *Service) ListItems(itemType ItemType, state ItemState, limit int) ([]*ScheduledItem, error) {
	return s.store.List(itemType, state, limit)
}

// ListItemsExcluding behaves like ListItems but omits items from one
// source. The Automations surface uses it to hide routines, which live
// in the same table but have their own Routines surface.
func (s *Service) ListItemsExcluding(itemType ItemType, state ItemState, limit int, excludeSource string) ([]*ScheduledItem, error) {
	return s.store.ListExcludingSource(itemType, state, limit, excludeSource)
}

// UpdateItem updates a scheduled item.
func (s *Service) UpdateItem(item *ScheduledItem) error {
	if err := s.store.Update(item); err != nil {
		return err
	}
	if s.events != nil {
		s.events.Publish("schedule.updated", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
		})
	}
	return nil
}

// CancelItem cancels a scheduled item.
func (s *Service) CancelItem(id string) error {
	item, err := s.store.Get(id)
	if err != nil {
		return err
	}
	item.State = StateCancelled
	if err := s.store.Update(item); err != nil {
		return err
	}
	if s.events != nil {
		s.events.Publish("schedule.cancelled", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
		})
	}
	return nil
}

// PauseItem pauses a scheduled item.
func (s *Service) PauseItem(id string) error {
	item, err := s.store.Get(id)
	if err != nil {
		return err
	}
	item.State = StatePaused
	return s.store.Update(item)
}

// ResumeItem resumes a paused scheduled item.
func (s *Service) ResumeItem(id string) error {
	item, err := s.store.Get(id)
	if err != nil {
		return err
	}
	item.State = StateScheduled
	return s.store.Update(item)
}

// RunNow triggers immediate execution of an item.
func (s *Service) RunNow(id string) error {
	item, err := s.store.Get(id)
	if err != nil {
		return err
	}
	go s.runItemSafely(item)
	return nil
}

// GetHistory retrieves execution history for an item.
func (s *Service) GetHistory(itemID string, limit int) ([]*ExecutionRecord, error) {
	return s.store.GetExecutionHistory(itemID, limit)
}

// generateExecutionID creates a unique execution ID for idempotency.
func generateExecutionID(item *ScheduledItem) string {
	return item.ID + ":" + time.Now().UTC().Format("20060102T150405Z")
}

// NextCronRun returns the next time the given cron expression fires after
// `now`, in the schedule's timezone. It is the single cron next-run source used
// both when a recurring item is first created and when it is rescheduled after
// an execution. Returns nil if the expression is unparseable (so the item is
// retired rather than drifting to an arbitrary "next hour").
func NextCronRun(expr, tz string, now time.Time) *time.Time {
	return computeCronNextRun(expr, tz, now)
}

// computeCronNextRun computes the next run for a cron expression using the
// gronx library, interpreted in the schedule's timezone. The reference clock is
// converted into the target timezone so "9 AM" means 9 AM there; the returned
// time is converted back to UTC for consistent storage and comparison.
func computeCronNextRun(expr, tz string, now time.Time) *time.Time {
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	localNow := now.In(loc)
	next, err := gronx.NextTickAfter(expr, localNow, false)
	if err != nil {
		// Unparseable or invalid expression: don't guess. Returning nil causes
		// the item to be retired (see handleSuccess) rather than firing at an
		// arbitrary time.
		return nil
	}
	utc := next.UTC()
	return &utc
}

// MigrateLegacyCron migrates a legacy cron/jobs.json file into the
// authoritative store. It is idempotent (deterministic item IDs) and safe to
// call on every boot.
func (s *Service) MigrateLegacyCron(path string) (LegacyCronMigrationResult, error) {
	if s == nil || s.store == nil {
		return LegacyCronMigrationResult{}, fmt.Errorf("scheduled service not initialized")
	}
	return MigrateLegacyCron(path, s.store)
}
