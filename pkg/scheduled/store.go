package scheduled

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Store persists ScheduledItems and ExecutionRecords in SQLite.
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// InitSchema creates the scheduled_items and execution_history tables.
func (s *Store) InitSchema() error {
	schema := `
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
	`
	_, err := s.db.Exec(schema)
	return err
}

// Create persists a new ScheduledItem.
func (s *Store) Create(item *ScheduledItem) error {
	if item.ID == "" {
		item.ID = generateID()
	}
	now := time.Now().UTC()
	item.CreatedAt = now
	item.UpdatedAt = now
	// All schedule instants are stored UTC. The TEXT rendering of a
	// non-UTC time sorts AFTER its UTC equivalent ("12:56 +0700" >
	// "05:56 +0000"), which silently breaks the lexicographic
	// next_run_at <= now comparison in ListDue — one-time reminders
	// would fire hours late or never. Timezone stays on the row for
	// display; storage is always UTC.
	utcScheduleTimes(item)

	actionsJSON, _ := json.Marshal(item.Action.Skills)

	_, err := s.db.Exec(`
		INSERT INTO scheduled_items (
			id, type, title, description, state,
			schedule_kind, schedule_at, schedule_every, schedule_expr,
			timezone,
			action_kind, action_content, action_command, action_deliver, action_skills,
			channel, chat_id, delivery_mode,
			source, created_by, created_at, updated_at,
			next_run_at, last_run_at, run_count, delete_after_run,
			retry_count, max_retries, last_error,
			parent_id, occurrence_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		item.ID, item.Type, item.Title, item.Description, item.State,
		item.Schedule.Kind, timePtrToSQL(item.Schedule.At), int64(item.Schedule.Every), item.Schedule.Expr,
		item.Timezone,
		item.Action.Kind, item.Action.Content, item.Action.Command, boolToInt(item.Action.Deliver), string(actionsJSON),
		item.Channel, item.ChatID, item.DeliveryMode,
		item.Source, item.CreatedBy, item.CreatedAt, item.UpdatedAt,
		timePtrToSQL(item.NextRunAt), timePtrToSQL(item.LastRunAt), item.RunCount, boolToInt(item.DeleteAfterRun),
		item.RetryCount, item.MaxRetries, item.LastError,
		item.ParentID, item.OccurrenceID,
	)
	return err
}

// Get retrieves a ScheduledItem by ID.
func (s *Store) Get(id string) (*ScheduledItem, error) {
	var item ScheduledItem
	var scheduleAt sql.NullTime
	var scheduleEvery int64
	var actionSkillsJSON string
	var nextRunAt, lastRunAt sql.NullTime

	err := s.db.QueryRow(`
		SELECT id, type, title, description, state,
			schedule_kind, schedule_at, schedule_every, schedule_expr,
			timezone,
			action_kind, action_content, action_command, action_deliver, action_skills,
			channel, chat_id, delivery_mode,
			source, created_by, created_at, updated_at,
			next_run_at, last_run_at, run_count, delete_after_run,
			retry_count, max_retries, last_error,
			parent_id, occurrence_id
		FROM scheduled_items WHERE id = ?
	`, id).Scan(
		&item.ID, &item.Type, &item.Title, &item.Description, &item.State,
		&item.Schedule.Kind, &scheduleAt, &scheduleEvery, &item.Schedule.Expr,
		&item.Timezone,
		&item.Action.Kind, &item.Action.Content, &item.Action.Command, &item.Action.Deliver, &actionSkillsJSON,
		&item.Channel, &item.ChatID, &item.DeliveryMode,
		&item.Source, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&nextRunAt, &lastRunAt, &item.RunCount, &item.DeleteAfterRun,
		&item.RetryCount, &item.MaxRetries, &item.LastError,
		&item.ParentID, &item.OccurrenceID,
	)
	if err != nil {
		return nil, err
	}

	item.Schedule.At = sqlTimeToPtr(scheduleAt)
	item.Schedule.Every = time.Duration(scheduleEvery)
	item.NextRunAt = sqlTimeToPtr(nextRunAt)
	item.LastRunAt = sqlTimeToPtr(lastRunAt)
	json.Unmarshal([]byte(actionSkillsJSON), &item.Action.Skills)

	return &item, nil
}

// List retrieves ScheduledItems with optional filters.
func (s *Store) List(itemType ItemType, state ItemState, limit int) ([]*ScheduledItem, error) {
	return s.listFiltered(itemType, state, limit, "")
}

// ListExcludingSource lists scheduled items while omitting one source
// (e.g. routines, which have their own product surface and must not
// appear twice). Empty excludeSource behaves exactly like List.
func (s *Store) ListExcludingSource(itemType ItemType, state ItemState, limit int, excludeSource string) ([]*ScheduledItem, error) {
	return s.listFiltered(itemType, state, limit, excludeSource)
}

func (s *Store) listFiltered(itemType ItemType, state ItemState, limit int, excludeSource string) ([]*ScheduledItem, error) {
	query := `SELECT id, type, title, description, state,
		schedule_kind, schedule_at, schedule_every, schedule_expr,
		timezone,
		action_kind, action_content, action_command, action_deliver, action_skills,
		channel, chat_id, delivery_mode,
		source, created_by, created_at, updated_at,
		next_run_at, last_run_at, run_count, delete_after_run,
		retry_count, max_retries, last_error,
		parent_id, occurrence_id
	FROM scheduled_items WHERE 1=1`
	args := []interface{}{}

	if itemType != "" {
		query += ` AND type = ?`
		args = append(args, itemType)
	}
	if state != "" {
		query += ` AND state = ?`
		args = append(args, state)
	}
	if excludeSource != "" {
		query += ` AND source != ?`
		args = append(args, excludeSource)
	}

	query += ` ORDER BY next_run_at ASC NULLS LAST, created_at DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ScheduledItem
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// ListDue retrieves items that are due for execution.
func (s *Store) ListDue(now time.Time) ([]*ScheduledItem, error) {
	rows, err := s.db.Query(`
		SELECT id, type, title, description, state,
			schedule_kind, schedule_at, schedule_every, schedule_expr,
			timezone,
			action_kind, action_content, action_command, action_deliver, action_skills,
			channel, chat_id, delivery_mode,
			source, created_by, created_at, updated_at,
			next_run_at, last_run_at, run_count, delete_after_run,
			retry_count, max_retries, last_error,
			parent_id, occurrence_id
		FROM scheduled_items
		WHERE state IN ('scheduled', 'due')
		  AND next_run_at IS NOT NULL
		  AND next_run_at <= ?
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ScheduledItem
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// Update modifies an existing ScheduledItem.
func (s *Store) Update(item *ScheduledItem) error {
	item.UpdatedAt = time.Now().UTC()
	utcScheduleTimes(item)

	actionsJSON, _ := json.Marshal(item.Action.Skills)

	_, err := s.db.Exec(`
		UPDATE scheduled_items SET
			title = ?, description = ?, state = ?,
			schedule_kind = ?, schedule_at = ?, schedule_every = ?, schedule_expr = ?,
			timezone = ?,
			action_kind = ?, action_content = ?, action_command = ?, action_deliver = ?, action_skills = ?,
			channel = ?, chat_id = ?, delivery_mode = ?,
			updated_at = ?,
			next_run_at = ?, last_run_at = ?, run_count = ?, delete_after_run = ?,
			retry_count = ?, max_retries = ?, last_error = ?,
			parent_id = ?, occurrence_id = ?
		WHERE id = ?
	`,
		item.Title, item.Description, item.State,
		item.Schedule.Kind, timePtrToSQL(item.Schedule.At), int64(item.Schedule.Every), item.Schedule.Expr,
		item.Timezone,
		item.Action.Kind, item.Action.Content, item.Action.Command, boolToInt(item.Action.Deliver), string(actionsJSON),
		item.Channel, item.ChatID, item.DeliveryMode,
		item.UpdatedAt,
		timePtrToSQL(item.NextRunAt), timePtrToSQL(item.LastRunAt), item.RunCount, boolToInt(item.DeleteAfterRun),
		item.RetryCount, item.MaxRetries, item.LastError,
		item.ParentID, item.OccurrenceID,
		item.ID,
	)
	return err
}

// Delete removes a ScheduledItem by ID.
func (s *Store) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM scheduled_items WHERE id = ?`, id)
	return err
}

// RecordExecution stores an execution record.
func (s *Store) RecordExecution(record *ExecutionRecord) error {
	if record.ID == "" {
		record.ID = generateID()
	}
	if record.ExecutionID == "" {
		record.ExecutionID = generateID()
	}

	_, err := s.db.Exec(`
		INSERT INTO execution_history (
			id, item_id, execution_id, scheduled_at, started_at,
			completed_at, status, error, channel, delivered_at, delivery_status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		record.ID, record.ItemID, record.ExecutionID, record.ScheduledAt, record.StartedAt,
		timePtrToSQL(record.CompletedAt), record.Status, record.Error, record.Channel,
		timePtrToSQL(record.DeliveredAt), record.DeliveryStatus,
	)
	return err
}

// GetExecutionHistory retrieves execution records for an item.
func (s *Store) GetExecutionHistory(itemID string, limit int) ([]*ExecutionRecord, error) {
	query := `
		SELECT id, item_id, execution_id, scheduled_at, started_at,
			completed_at, status, error, channel, delivered_at, delivery_status
		FROM execution_history
		WHERE item_id = ?
		ORDER BY started_at DESC
	`
	args := []interface{}{itemID}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*ExecutionRecord
	for rows.Next() {
		var record ExecutionRecord
		var completedAt, deliveredAt sql.NullTime
		err := rows.Scan(
			&record.ID, &record.ItemID, &record.ExecutionID, &record.ScheduledAt, &record.StartedAt,
			&completedAt, &record.Status, &record.Error, &record.Channel,
			&deliveredAt, &record.DeliveryStatus,
		)
		if err != nil {
			continue
		}
		record.CompletedAt = sqlTimeToPtr(completedAt)
		record.DeliveredAt = sqlTimeToPtr(deliveredAt)
		records = append(records, &record)
	}
	return records, nil
}

// HasExecution checks if an execution ID already exists (idempotency).
func (s *Store) HasExecution(executionID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM execution_history WHERE execution_id = ?`, executionID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateNextRun updates the next_run_at for an item.
func (s *Store) UpdateNextRun(id string, nextRun *time.Time) error {
	_, err := s.db.Exec(`UPDATE scheduled_items SET next_run_at = ?, updated_at = ? WHERE id = ?`,
		timePtrToSQL(nextRun), time.Now().UTC(), id)
	return err
}

// UpdateState updates the state of an item.
func (s *Store) UpdateState(id string, state ItemState) error {
	_, err := s.db.Exec(`UPDATE scheduled_items SET state = ?, updated_at = ? WHERE id = ?`,
		state, time.Now().UTC(), id)
	return err
}

// IncrementRunCount increments the run count and updates last_run_at.
func (s *Store) IncrementRunCount(id string) error {
	_, err := s.db.Exec(`UPDATE scheduled_items SET run_count = run_count + 1, last_run_at = ?, updated_at = ? WHERE id = ?`,
		time.Now().UTC(), time.Now().UTC(), id)
	return err
}

// utcScheduleTimes normalizes an item's schedule instants to UTC in
// place. See Create: TEXT-rendered offsets break due comparisons.
func utcScheduleTimes(item *ScheduledItem) {
	if item == nil {
		return
	}
	if item.Schedule.At != nil {
		at := item.Schedule.At.UTC()
		item.Schedule.At = &at
	}
	if item.NextRunAt != nil {
		next := item.NextRunAt.UTC()
		item.NextRunAt = &next
	}
	if item.LastRunAt != nil {
		last := item.LastRunAt.UTC()
		item.LastRunAt = &last
	}
}

// legacyTimeLayouts parses the TEXT forms previously stored for schedule
// columns: Go time.String() output with a numeric offset, an optional zone
// abbreviation, and an optional monotonic suffix
// ("2026-09-12 12:56:00.007 +0700 +07 m=+145.9"), or the driver's
// DATETIME round-trip form ("2026-09-12T12:56:00.007+07:00").
func parseLegacyStoredTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("empty stored time")
	}
	rfcLayouts := []string{time.RFC3339Nano, time.RFC3339}
	if !strings.Contains(trimmed, " ") {
		for _, layout := range rfcLayouts {
			if t, err := time.Parse(layout, trimmed); err == nil {
				return t, nil
			}
		}
		return time.Time{}, fmt.Errorf("unparseable stored time %q", raw)
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		return time.Time{}, fmt.Errorf("unparseable stored time %q", raw)
	}
	// Date, clock, numeric offset; everything after is zone abbreviation
	// and/or monotonic reading — same instant, dropped for parsing.
	compact := fields[0] + " " + fields[1] + " " + fields[2]
	for _, layout := range append([]string{
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05 -0700",
	}, rfcLayouts...) {
		if t, err := time.Parse(layout, compact); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable stored time %q", raw)
}

// NormalizeStoredTimesToUTC rewrites schedule_at/next_run_at/last_run_at
// values stored with non-UTC offsets (or monotonic suffixes) into UTC.
// One-time repair for rows written before schedule instants were
// normalized on write; idempotent and safe to run every boot.
func (s *Store) NormalizeStoredTimesToUTC() (int64, error) {
	rows, err := s.db.Query(`SELECT id, schedule_at, next_run_at, last_run_at FROM scheduled_items`)
	if err != nil {
		return 0, err
	}
	type fix struct {
		id         string
		scheduleAt *string
		nextRunAt  *string
		lastRunAt  *string
	}
	var fixes []fix
	for rows.Next() {
		var id string
		var schedAt, nextAt, lastAt sql.NullString
		if err := rows.Scan(&id, &schedAt, &nextAt, &lastAt); err != nil {
			rows.Close()
			return 0, err
		}
		normalize := func(ns sql.NullString) *string {
			if !ns.Valid || ns.String == "" {
				return nil
			}
			// Already UTC-shaped (or UTC ISO): leave untouched.
			if strings.Contains(ns.String, "+0000") || strings.HasSuffix(ns.String, "Z") ||
				strings.Contains(ns.String, "+00:00") {
				return nil
			}
			t, err := parseLegacyStoredTime(ns.String)
			if err != nil {
				return nil
			}
			out := t.UTC().Format("2006-01-02 15:04:05.999999999 +0000 UTC")
			return &out
		}
		if sa, na, la := normalize(schedAt), normalize(nextAt), normalize(lastAt); sa != nil || na != nil || la != nil {
			fixes = append(fixes, fix{id: id, scheduleAt: sa, nextRunAt: na, lastRunAt: la})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	var total int64
	for _, f := range fixes {
		if f.scheduleAt != nil {
			if _, err := s.db.Exec(`UPDATE scheduled_items SET schedule_at = ? WHERE id = ?`, *f.scheduleAt, f.id); err != nil {
				return total, err
			}
			total++
		}
		if f.nextRunAt != nil {
			if _, err := s.db.Exec(`UPDATE scheduled_items SET next_run_at = ? WHERE id = ?`, *f.nextRunAt, f.id); err != nil {
				return total, err
			}
			total++
		}
		if f.lastRunAt != nil {
			if _, err := s.db.Exec(`UPDATE scheduled_items SET last_run_at = ? WHERE id = ?`, *f.lastRunAt, f.id); err != nil {
				return total, err
			}
			total++
		}
	}
	return total, nil
}

// scanItem scans a row into a ScheduledItem.
func scanItem(rows *sql.Rows) (*ScheduledItem, error) {
	var item ScheduledItem
	var scheduleAt sql.NullTime
	var scheduleEvery int64
	var actionSkillsJSON string
	var nextRunAt, lastRunAt sql.NullTime

	err := rows.Scan(
		&item.ID, &item.Type, &item.Title, &item.Description, &item.State,
		&item.Schedule.Kind, &scheduleAt, &scheduleEvery, &item.Schedule.Expr,
		&item.Timezone,
		&item.Action.Kind, &item.Action.Content, &item.Action.Command, &item.Action.Deliver, &actionSkillsJSON,
		&item.Channel, &item.ChatID, &item.DeliveryMode,
		&item.Source, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
		&nextRunAt, &lastRunAt, &item.RunCount, &item.DeleteAfterRun,
		&item.RetryCount, &item.MaxRetries, &item.LastError,
		&item.ParentID, &item.OccurrenceID,
	)
	if err != nil {
		return nil, err
	}

	item.Schedule.At = sqlTimeToPtr(scheduleAt)
	item.Schedule.Every = time.Duration(scheduleEvery)
	item.NextRunAt = sqlTimeToPtr(nextRunAt)
	item.LastRunAt = sqlTimeToPtr(lastRunAt)
	json.Unmarshal([]byte(actionSkillsJSON), &item.Action.Skills)

	return &item, nil
}

// Helper functions

func generateID() string {
	return uuid.New().String()[:16]
}

func timePtrToSQL(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func sqlTimeToPtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func nullTimePtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
