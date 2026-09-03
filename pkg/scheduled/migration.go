package scheduled

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// CronJob represents the old cron job format for migration.
type CronJob struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Enabled         bool              `json:"enabled"`
	LifecycleState  string            `json:"lifecycle_state"`
	PausedAt        *time.Time        `json:"paused_at"`
	RunCount        int               `json:"run_count"`
	LastRunAt       *time.Time        `json:"last_run_at"`
	NextRunAt       *time.Time        `json:"next_run_at"`
	Schedule        CronSchedule      `json:"schedule"`
	Payload         CronPayload       `json:"payload"`
	State           CronJobState      `json:"state"`
	Metadata        map[string]interface{} `json:"metadata"`
	CreatedAtMS     int64             `json:"created_at_ms"`
	UpdatedAtMS     int64             `json:"updated_at_ms"`
	DeleteAfterRun  bool              `json:"delete_after_run"`
	Skills          []string          `json:"skills"`
	NoAgent         bool              `json:"no_agent"`
}

// CronSchedule is the old schedule format.
type CronSchedule struct {
	Kind  string `json:"kind"`
	AtMS  *int64 `json:"at_ms,omitempty"`
	EveryMS *int64 `json:"every_ms,omitempty"`
	Expr  string `json:"expr,omitempty"`
	Timezone string `json:"tz,omitempty"`
}

// CronPayload is the old payload format.
type CronPayload struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Command string   `json:"command,omitempty"`
	Deliver bool     `json:"deliver"`
	Channel string   `json:"channel,omitempty"`
	To      string   `json:"to,omitempty"`
	Target  string   `json:"target,omitempty"`
	Skills  []string `json:"skills,omitempty"`
	Profile string   `json:"profile,omitempty"`
}

// CronJobState is the old state format.
type CronJobState struct {
	NextRunAtMS *int64  `json:"next_run_at_ms,omitempty"`
	LastRunAtMS *int64  `json:"last_run_at_ms,omitempty"`
	LastStatus  string  `json:"last_status,omitempty"`
	LastError   string  `json:"last_error,omitempty"`
}

// CronStore is the old store format.
type CronStore struct {
	Version int        `json:"version"`
	Jobs    []CronJob  `json:"jobs"`
}

// MigrateFromCronJSON reads the old cron/jobs.json file and creates
// ScheduledItems in SQLite. Returns the number of migrated items.
func MigrateFromCronJSON(store *Store, cronJSONPath string) (int, error) {
	// Read the old file
	data, err := os.ReadFile(cronJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // No file to migrate
		}
		return 0, fmt.Errorf("failed to read cron JSON: %w", err)
	}

	// Parse old format
	var oldStore CronStore
	if err := json.Unmarshal(data, &oldStore); err != nil {
		return 0, fmt.Errorf("failed to parse cron JSON: %w", err)
	}

	// Migrate each job
	migrated := 0
	for _, oldJob := range oldStore.Jobs {
		item := convertCronJob(&oldJob)
		if item == nil {
			continue
		}

		// Check if already migrated
		existing, err := store.Get(item.ID)
		if err == nil && existing != nil {
			continue // Already migrated
		}

		if err := store.Create(item); err != nil {
			return migrated, fmt.Errorf("failed to create item %s: %w", item.ID, err)
		}
		migrated++
	}

	return migrated, nil
}

// convertCronJob converts an old CronJob to a ScheduledItem.
func convertCronJob(old *CronJob) *ScheduledItem {
	item := &ScheduledItem{
		ID:              old.ID,
		Type:            TypeAutomation, // All old jobs are automations
		Title:           old.Name,
		Description:     old.Payload.Message,
		State:           convertState(old.LifecycleState, old.Enabled),
		Timezone:        "UTC",
		Channel:         old.Payload.Channel,
		ChatID:          old.Payload.To,
		DeliveryMode:    DeliverySmart,
		Source:          "migration",
		CreatedBy:       "system",
		RunCount:        old.RunCount,
		DeleteAfterRun:  old.DeleteAfterRun,
		LastError:       old.State.LastError,
	}

	// Convert timestamps
	if old.CreatedAtMS > 0 {
		item.CreatedAt = time.UnixMilli(old.CreatedAtMS)
	}
	if old.UpdatedAtMS > 0 {
		item.UpdatedAt = time.UnixMilli(old.UpdatedAtMS)
	}
	item.LastRunAt = old.LastRunAt
	item.NextRunAt = old.NextRunAt

	// Convert schedule
	item.Schedule = convertSchedule(old.Schedule)

	// Convert action
	item.Action = convertPayload(old.Payload)

	// Set max retries
	item.MaxRetries = 3

	return item
}

// convertState converts the old lifecycle state to the new format.
func convertState(oldState string, enabled bool) ItemState {
	switch oldState {
	case "paused":
		return StatePaused
	case "running":
		return StateRunning
	default:
		if enabled {
			return StateScheduled
		}
		return StatePaused
	}
}

// convertSchedule converts the old schedule format.
func convertSchedule(old CronSchedule) Schedule {
	s := Schedule{Kind: ScheduleCron}

	switch old.Kind {
	case "at":
		s.Kind = ScheduleAt
		if old.AtMS != nil {
			t := time.UnixMilli(*old.AtMS)
			s.At = &t
		}
	case "every":
		s.Kind = ScheduleEvery
		if old.EveryMS != nil {
			s.Every = time.Duration(*old.EveryMS) * time.Millisecond
		}
	case "cron":
		s.Kind = ScheduleCron
		s.Expr = old.Expr
	}

	return s
}

// convertPayload converts the old payload format.
func convertPayload(old CronPayload) Action {
	a := Action{
		Content: old.Message,
		Command: old.Command,
		Deliver: old.Deliver,
		Skills:  old.Skills,
	}

	switch {
	case old.Command != "":
		a.Kind = ActionCommand
	case old.Deliver:
		a.Kind = ActionMessage
	default:
		a.Kind = ActionAgentTurn
	}

	return a
}

// BackupCronJSON creates a backup of the old cron/jobs.json file.
func BackupCronJSON(cronJSONPath string) error {
	data, err := os.ReadFile(cronJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Nothing to backup
		}
		return err
	}

	backupPath := cronJSONPath + ".backup." + time.Now().Format("20060102T150405")
	return os.WriteFile(backupPath, data, 0644)
}
