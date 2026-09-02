// Package scheduled implements Ghost's scheduled intelligence system.
// It provides a unified model for reminders, future events, automations,
// and durable tasks with deterministic scheduling, reliable execution,
// and human-readable presentation.
package scheduled

import (
	"time"
)

// ItemType discriminates between the different kinds of scheduled items.
type ItemType string

const (
	TypeReminder   ItemType = "reminder"   // "Tell me something at time T"
	TypeEvent      ItemType = "event"      // "Something happens at time T"
	TypeAutomation ItemType = "automation" // "When X, do Y"
	TypeTask       ItemType = "task"       // "Do work W"
)

// ItemState represents the lifecycle state of a scheduled item.
type ItemState string

const (
	StateScheduled ItemState = "scheduled" // Waiting for trigger
	StateDue       ItemState = "due"       // Triggered, waiting to execute
	StateRunning   ItemState = "running"   // Executing
	StateCompleted ItemState = "completed" // Successfully finished
	StateFailed    ItemState = "failed"    // Execution failed
	StateCancelled ItemState = "cancelled" // User cancelled
	StateMissed    ItemState = "missed"    // Past due, not executed (one-time)
	StatePaused    ItemState = "paused"    // User paused
)

// ScheduleKind defines how a schedule triggers.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"    // One-time at absolute time
	ScheduleEvery ScheduleKind = "every" // Recurring interval
	ScheduleCron  ScheduleKind = "cron"  // Standard cron expression
	ScheduleNone  ScheduleKind = "none"  // No schedule (manual only)
)

// ActionKind defines what happens when a schedule triggers.
type ActionKind string

const (
	ActionMessage   ActionKind = "message"   // Send a message directly
	ActionAgentTurn ActionKind = "agent_turn" // Process through agent
	ActionCommand   ActionKind = "command"   // Run a shell command
)

// DeliveryMode defines how the item is delivered to the user.
type DeliveryMode string

const (
	DeliverySmart   DeliveryMode = "smart"   // Follow user to last active session
	DeliveryOrigin  DeliveryMode = "origin"  // Return to originating channel
	DeliveryExplicit DeliveryMode = "explicit" // Target specific channel
)

// Schedule defines when an item should trigger.
type Schedule struct {
	Kind  ScheduleKind  `json:"kind"`
	At    *time.Time    `json:"at,omitempty"`    // For "at"
	Every time.Duration `json:"every,omitempty"` // For "every"
	Expr  string        `json:"expr,omitempty"`  // For "cron" (5-field)
}

// Action defines what happens when a schedule triggers.
type Action struct {
	Kind    ActionKind `json:"kind"`
	Content string     `json:"content"` // Message or prompt
	Command string     `json:"command,omitempty"` // Shell command
	Deliver bool       `json:"deliver"` // If true, send directly; if false, process through agent
	Skills  []string   `json:"skills,omitempty"`
}

// ScheduledItem is the core domain object for all scheduled intelligence.
type ScheduledItem struct {
	ID              string           `json:"id"`
	Type            ItemType         `json:"type"`
	Title           string           `json:"title"`
	Description     string           `json:"description"`
	State           ItemState        `json:"state"`

	// Schedule
	Schedule  Schedule `json:"schedule"`
	Timezone  string   `json:"timezone"` // IANA timezone

	// Action
	Action Action `json:"action"`

	// Delivery
	Channel      string       `json:"channel,omitempty"`
	ChatID       string       `json:"chat_id,omitempty"`
	DeliveryMode DeliveryMode `json:"delivery_mode"`

	// Metadata
	Source    string    `json:"source"`    // "user", "system", "proactive"
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Execution tracking
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	RunCount       int        `json:"run_count"`
	DeleteAfterRun bool       `json:"delete_after_run"`

	// Failure handling
	RetryCount int    `json:"retry_count"`
	MaxRetries int    `json:"max_retries"`
	LastError  string `json:"last_error,omitempty"`

	// Relationships
	ParentID     string `json:"parent_id,omitempty"`     // For recurring occurrences
	OccurrenceID string `json:"occurrence_id,omitempty"` // Specific occurrence
}

// ExecutionRecord tracks a single execution of a scheduled item.
type ExecutionRecord struct {
	ID              string     `json:"id"`
	ItemID          string     `json:"item_id"`
	ExecutionID     string     `json:"execution_id"` // Idempotency key
	ScheduledAt     time.Time  `json:"scheduled_at"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	Status          string     `json:"status"` // "ok", "error", "missed", "cancelled"
	Error           string     `json:"error,omitempty"`
	Channel         string     `json:"channel,omitempty"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	DeliveryStatus  string     `json:"delivery_status,omitempty"` // "sent", "failed", "unknown"
}

// ValidTypes is the set of valid item types.
var ValidTypes = map[ItemType]bool{
	TypeReminder:   true,
	TypeEvent:      true,
	TypeAutomation: true,
	TypeTask:       true,
}

// ValidStates is the set of valid item states.
var ValidStates = map[ItemState]bool{
	StateScheduled: true,
	StateDue:       true,
	StateRunning:   true,
	StateCompleted: true,
	StateFailed:    true,
	StateCancelled: true,
	StateMissed:    true,
	StatePaused:    true,
}

// ValidScheduleKinds is the set of valid schedule kinds.
var ValidScheduleKinds = map[ScheduleKind]bool{
	ScheduleAt:    true,
	ScheduleEvery: true,
	ScheduleCron:  true,
	ScheduleNone:  true,
}

// ValidActionKinds is the set of valid action kinds.
var ValidActionKinds = map[ActionKind]bool{
	ActionMessage:   true,
	ActionAgentTurn: true,
	ActionCommand:   true,
}

// ValidDeliveryModes is the set of valid delivery modes.
var ValidDeliveryModes = map[DeliveryMode]bool{
	DeliverySmart:    true,
	DeliveryOrigin:   true,
	DeliveryExplicit: true,
}

// IsRecurring returns true if this item repeats.
func (si *ScheduledItem) IsRecurring() bool {
	return si.Schedule.Kind == ScheduleEvery || si.Schedule.Kind == ScheduleCron
}

// IsOneTime returns true if this item fires once.
func (si *ScheduledItem) IsOneTime() bool {
	return si.Schedule.Kind == ScheduleAt
}

// IsPaused returns true if the item is paused.
func (si *ScheduledItem) IsPaused() bool {
	return si.State == StatePaused
}

// CanExecute returns true if the item is in a state that allows execution.
func (si *ScheduledItem) CanExecute() bool {
	return si.State == StateScheduled || si.State == StateDue
}

// HumanSchedule returns a human-readable schedule description.
func (si *ScheduledItem) HumanSchedule() string {
	switch si.Schedule.Kind {
	case ScheduleAt:
		if si.Schedule.At != nil {
			return si.Schedule.At.Format("Monday, January 2 at 3:04 PM")
		}
		return "One-time"
	case ScheduleEvery:
		return formatDuration(si.Schedule.Every)
	case ScheduleCron:
		return formatCronExpression(si.Schedule.Expr)
	default:
		return "Manual"
	}
}

// formatDuration converts a duration to human-readable form.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "Every few seconds"
	}
	if d < time.Hour {
		minutes := int(d.Minutes())
		if minutes == 1 {
			return "Every minute"
		}
		return formatEveryN(minutes, "minute")
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "Every hour"
		}
		return formatEveryN(hours, "hour")
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "Every day"
	}
	if days == 7 {
		return "Every week"
	}
	return formatEveryN(days, "day")
}

func formatEveryN(n int, unit string) string {
	if n == 1 {
		return "Every " + unit
	}
	return "Every " + itoa(n) + " " + unit + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// formatCronExpression converts a cron expression to human-readable form.
func formatCronExpression(expr string) string {
	// Simple cron formatting (supports basic patterns)
	switch expr {
	case "0 8 * * *":
		return "Every day at 8:00 AM"
	case "0 9 * * 1-5":
		return "Weekdays at 9:00 AM"
	case "0 0 * * *":
		return "Every day at midnight"
	case "0 */2 * * *":
		return "Every 2 hours"
	case "*/15 * * * *":
		return "Every 15 minutes"
	case "0 8 * * 1":
		return "Every Monday at 8:00 AM"
	case "0 8 * * 5":
		return "Every Friday at 8:00 AM"
	}
	return expr
}

// SimpleEventBus is a no-op event bus for basic usage.
type SimpleEventBus struct{}

// Publish implements the EventBus interface.
func (b *SimpleEventBus) Publish(topic string, payload interface{}) {
	// No-op: events are logged but not published
}
