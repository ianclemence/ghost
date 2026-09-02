# Ghost Scheduled Intelligence Architecture

## Overview

Ghost's scheduled intelligence system provides a unified model for reminders, future events, automations, and durable tasks. It replaces the old cron/jobs.json system with a SQLite-backed, human-readable scheduling system.

## Data Model

### ScheduledItem (unified model)

```go
type ItemType string

const (
    TypeReminder   ItemType = "reminder"   // "Tell me something at time T"
    TypeEvent      ItemType = "event"      // "Something happens at time T"
    TypeAutomation ItemType = "automation" // "When X, do Y"
    TypeTask       ItemType = "task"       // "Do work W"
)

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

type ScheduledItem struct {
    ID              string      // Stable unique ID
    Type            ItemType    // reminder, event, automation, task
    Title           string      // Human-readable title
    Description     string      // Natural language description
    State           ItemState   // Current lifecycle state
    
    // Schedule
    Schedule        Schedule    // When to trigger
    Timezone        string      // Explicit timezone (IANA)
    
    // Action
    Action          Action      // What to do when triggered
    
    // Delivery
    Channel         string      // Target channel
    ChatID          string      // Target chat
    DeliveryMode    DeliveryMode // "smart", "origin", "explicit"
    
    // Metadata
    Source          string      // "user", "system", "proactive"
    CreatedBy       string      // User who created it
    CreatedAt       time.Time
    UpdatedAt       time.Time
    
    // Execution tracking
    NextRunAt       *time.Time
    LastRunAt       *time.Time
    RunCount        int
    DeleteAfterRun  bool        // For one-time items
    
    // Failure handling
    RetryCount      int
    MaxRetries      int
    LastError       string
    
    // Parent/child relationships
    ParentID        string      // For recurring: parent template ID
    OccurrenceID    string      // For recurring: specific occurrence ID
}

type ScheduleKind string

const (
    ScheduleAt    ScheduleKind = "at"    // Specific time
    ScheduleEvery ScheduleKind = "every" // Recurring interval
    ScheduleCron  ScheduleKind = "cron"  // Cron expression
    ScheduleNone  ScheduleKind = "none"  // Manual trigger only
)

type Schedule struct {
    Kind    ScheduleKind    // "at", "every", "cron", "none"
    At      *time.Time      // For "at"
    Every   time.Duration   // For "every"
    Expr    string          // For "cron" (5-field)
    TZ      string          // Timezone for cron expressions
}

type ActionKind string

const (
    ActionMessage   ActionKind = "message"   // Send directly
    ActionAgentTurn ActionKind = "agent_turn" // Process through agent
    ActionCommand   ActionKind = "command"   // Shell command
)

type DeliveryMode string

const (
    DeliverySmart  DeliveryMode = "smart"
    DeliveryOrigin DeliveryMode = "origin"
    DeliveryExplicit DeliveryMode = "explicit"
)

type Action struct {
    Kind        ActionKind  // "message", "agent_turn", "command"
    Content     string      // Message or prompt
    Command     string      // Shell command (if kind="command")
    Deliver     bool        // If true, send directly; if false, process through agent
    Skills      []string    // Required skills
}
```

### ExecutionRecord (SQLite)

```go
type ExecutionRecord struct {
    ID              string
    ItemID          string      // FK to ScheduledItem
    ExecutionID     string      // Idempotency key
    ScheduledAt     time.Time   // When it was supposed to run
    StartedAt       time.Time
    CompletedAt     *time.Time
    Status          string      // "ok", "error", "missed", "cancelled"
    Error           string
    Channel         string      // Which channel delivered
    DeliveredAt     *time.Time
    DeliveryStatus  string      // "delivered", "failed", "unknown"
}
```

## SQLite Schema

```sql
CREATE TABLE IF NOT EXISTS scheduled_items (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT DEFAULT '',
    state TEXT NOT NULL DEFAULT 'scheduled',
    schedule_kind TEXT NOT NULL,
    schedule_at DATETIME,
    schedule_every INTEGER DEFAULT 0,
    schedule_expr TEXT DEFAULT '',
    timezone TEXT DEFAULT 'UTC',
    action_kind TEXT NOT NULL DEFAULT 'message',
    action_content TEXT DEFAULT '',
    action_command TEXT DEFAULT '',
    action_deliver INTEGER DEFAULT 0,
    action_skills TEXT DEFAULT '[]',
    channel TEXT DEFAULT '',
    chat_id TEXT DEFAULT '',
    delivery_mode TEXT DEFAULT 'smart',
    source TEXT DEFAULT 'user',
    created_by TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    next_run_at DATETIME,
    last_run_at DATETIME,
    run_count INTEGER DEFAULT 0,
    delete_after_run INTEGER DEFAULT 0,
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    last_error TEXT DEFAULT '',
    parent_id TEXT DEFAULT '',
    occurrence_id TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS execution_history (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    scheduled_at DATETIME NOT NULL,
    started_at DATETIME NOT NULL,
    completed_at DATETIME,
    status TEXT NOT NULL DEFAULT 'ok',
    error TEXT DEFAULT '',
    channel TEXT DEFAULT '',
    delivered_at DATETIME,
    delivery_status TEXT DEFAULT 'unknown',
    FOREIGN KEY (item_id) REFERENCES scheduled_items(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_scheduled_items_state ON scheduled_items(state);
CREATE INDEX IF NOT EXISTS idx_scheduled_items_next_run ON scheduled_items(next_run_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_items_type ON scheduled_items(type);
CREATE INDEX IF NOT EXISTS idx_execution_history_item_id ON execution_history(item_id);
CREATE INDEX IF NOT EXISTS idx_execution_history_execution_id ON execution_history(execution_id);
```

## API Endpoints

### List Scheduled Items
```
GET /v1/scheduled?type=reminder&state=scheduled&limit=50
```

Response:
```json
{
    "items": [...]
}
```

### Create Scheduled Item
```
POST /v1/scheduled
```

Request:
```json
{
    "type": "reminder",
    "title": "Morning briefing",
    "description": "Summarize my calendar and news",
    "schedule": {
        "kind": "cron",
        "expr": "0 8 * * *",
        "tz": "Asia/Bangkok"
    },
    "action": {
        "kind": "agent_turn",
        "content": "Summarize my calendar and today's news"
    },
    "channel": "telegram",
    "chat_id": "123456789"
}
```

Response:
```json
{
    "ok": true,
    "item": {...}
}
```

### Get Scheduled Item
```
GET /v1/scheduled/:id
```

### Update Scheduled Item
```
PATCH /v1/scheduled/:id
```

Request:
```json
{
    "title": "Updated title",
    "state": "paused"
}
```

### Delete Scheduled Item
```
DELETE /v1/scheduled/:id
```

### Pause Scheduled Item
```
POST /v1/scheduled/:id/pause
```

### Resume Scheduled Item
```
POST /v1/scheduled/:id/resume
```

### Run Now
```
POST /v1/scheduled/:id/run
```

### Get Execution History
```
GET /v1/scheduled/history?item_id=:id&limit=50
```

Response:
```json
{
    "history": [...]
}
```

## Migration from cron/jobs.json

The system includes a migration utility that reads the old `cron/jobs.json` file and creates ScheduledItems in SQLite.

```go
migrated, err := scheduled.MigrateFromCronJSON(store, "/path/to/cron/jobs.json")
```

The migration:
- Converts all old jobs to `TypeAutomation`
- Maps schedule types (at, every, cron)
- Preserves run counts and timestamps
- Skips already-migrated items
- Creates backup of original file

## Scheduler Service

The scheduler service runs a 1-second tick loop that:
1. Queries for due items (next_run_at <= now)
2. Marks items as "due" to prevent duplicate execution
3. Executes items asynchronously
4. Records execution history with idempotency
5. Handles success (mark completed, schedule next run) and failure (retry with backoff)
6. Publishes events via EventBus

### Missed Schedule Handling

- **One-time items**: Fire if missed < 24h, mark as "missed" if > 24h
- **Recurring items**: Compute next valid occurrence after downtime

### Event Vocabulary

```
schedule.created    - New item created
schedule.updated    - Item updated
schedule.cancelled  - Item cancelled
schedule.started    - Execution started
schedule.completed  - Execution completed
schedule.failed     - Execution failed (after max retries)
reminder.missed     - One-time reminder missed
```

## Web Console Redesign

The automations section has been redesigned as "Things Ghost Will Do":

- **Chronological view**: Items sorted by next run time
- **Type badges**: Visual distinction between reminders, events, automations, tasks
- **Human-readable schedules**: "Every day at 8:00 AM", "Weekdays at 9:00 AM"
- **Execution history**: Shows recent runs with status and errors
- **Actions**: Pause, resume, run now, delete
- **Create form**: Guided creation with type selector and schedule input

## Implementation Status

### Completed
- [x] Data model (`pkg/scheduled/types.go`)
- [x] SQLite store (`pkg/scheduled/store.go`)
- [x] Scheduler service (`pkg/scheduled/service.go`)
- [x] Migration utility (`pkg/scheduled/migration.go`)
- [x] API endpoints (`cmd/ghost/internal_api.go`)
- [x] Web Console redesign (`cmd/ghost-web/web/js/sections/automations.js`)
- [x] Unit tests (`pkg/scheduled/types_test.go`, `migration_test.go`)

### In Progress
- [ ] Cron expression parsing (using gronx library)
- [ ] Natural language schedule parsing
- [ ] Integration with existing cron tool
- [ ] Backward compatibility with old cron API

### Planned
- [ ] Timezone-aware cron expressions
- [ ] Schedule templates
- [ ] Batch operations
- [ ] Webhook delivery mode
- [ ] Advanced retry strategies
