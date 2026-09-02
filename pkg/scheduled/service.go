package scheduled

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/adhocore/gronx"
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

	items, err := s.store.ListDue(now)
	if err != nil {
		log.Printf("[scheduled] failed to list due items: %v", err)
		return
	}

	for _, item := range items {
		// Transition the item to running synchronously so a subsequent tick can
		// never re-list it and fire it a second time while execution is in flight.
		if err := s.store.UpdateState(item.ID, StateRunning); err != nil {
			log.Printf("[scheduled] failed to mark item running: %v", err)
			continue
		}

		// Execute asynchronously
		go s.executeItem(item)
	}
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

	// Record execution start
	record := &ExecutionRecord{
		ItemID:      item.ID,
		ExecutionID: execID,
		ScheduledAt: *item.NextRunAt,
		StartedAt:   time.Now().UTC(),
		Status:      "ok",
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
		log.Printf("[scheduled] execution failed for item %s: %v", item.ID, err)
	} else {
		record.Status = "ok"
	}

	if err := s.store.RecordExecution(record); err != nil {
		log.Printf("[scheduled] failed to record execution: %v", err)
	}

	// Update item state
	if err := s.store.IncrementRunCount(item.ID); err != nil {
		log.Printf("[scheduled] failed to increment run count: %v", err)
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
	if item.IsOneTime() || item.DeleteAfterRun {
		// One-time item: delete after successful execution
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

	// Recurring item: compute next run
	nextRun := s.computeNextRun(item)
	if nextRun == nil {
		// No more runs: delete
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

	if s.events != nil {
		s.events.Publish("schedule.completed", map[string]interface{}{
			"item_id": item.ID,
			"type":    item.Type,
			"title":   item.Title,
			"next_at": nextRun,
		})
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

// HandleMissedSolicies processes items that were missed during downtime.
func (s *Service) HandleMissedSolicies() {
	now := time.Now().UTC()

	// Find items that should have run but didn't
	items, err := s.store.ListDue(now.Add(-24 * time.Hour))
	if err != nil {
		log.Printf("[scheduled] failed to list missed items: %v", err)
		return
	}

	for _, item := range items {
		if item.NextRunAt == nil {
			continue
		}

		missedDuration := now.Sub(*item.NextRunAt)

		if item.IsOneTime() {
			// One-time reminder: apply missed schedule policy
			s.handleMissedOneTime(item, missedDuration)
		} else {
			// Recurring: just compute next valid occurrence
			s.handleMissedRecurring(item, now)
		}
	}
}

// handleMissedOneTime processes a missed one-time reminder.
func (s *Service) handleMissedOneTime(item *ScheduledItem, missedDuration time.Duration) {
	switch {
	case missedDuration < time.Hour:
		// Fire immediately with note
		go s.executeItem(item)
	case missedDuration < 24*time.Hour:
		// Fire with "late" note
		go s.executeItem(item)
	default:
		// Mark as missed, don't fire
		if err := s.store.UpdateState(item.ID, StateMissed); err != nil {
			log.Printf("[scheduled] failed to mark item missed: %v", err)
		}
		if s.events != nil {
			s.events.Publish("reminder.missed", map[string]interface{}{
				"item_id": item.ID,
				"title":   item.Title,
			})
		}
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
	go s.executeItem(item)
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
