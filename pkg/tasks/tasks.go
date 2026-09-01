// Package tasks provides a durable, resumable job store for Ghost. Schedules
// (cron) are not the same thing as durable work: a job can be checkpointed,
// retried, cancelled, and resumed after a provider failure or process restart.
// It is SQLite-backed and part of Ghost State, not a separate service.
package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Status is a job's lifecycle state.
type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusSucceeded   Status = "succeeded"
	StatusFailed      Status = "failed"
	StatusCancelled   Status = "cancelled"
	StatusInterrupted Status = "interrupted"
)

// Event kinds emitted by the store (the agent maps these to typed events).
const (
	EventStarted   = "task.started"
	EventProgress  = "task.progress"
	EventDone      = "task.completed"
	EventFailed    = "task.failed"
	EventCancelled = "task.cancelled"
	EventRetrying  = "task.retrying"
)

// Job is one durable unit of work.
type Job struct {
	ID          string                 `json:"id"`
	Kind        string                 `json:"kind"`
	Status      Status                 `json:"status"`
	Progress    float64                `json:"progress"`
	Checkpoints []string               `json:"checkpoints,omitempty"`
	Payload     map[string]interface{} `json:"payload,omitempty"`
	SessionKey  string                 `json:"session_key,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Attempts    int                    `json:"attempts"`
	CreatedAt   int64                  `json:"created_at"`
	StartedAt   *int64                 `json:"started_at,omitempty"`
	FinishedAt  *int64                 `json:"finished_at,omitempty"`
	UpdatedAt   int64                  `json:"updated_at"`
}

// Store persists jobs in SQLite.
type Store struct {
	db      *sql.DB
	onEvent func(kind string, job Job)
}

// NewStore creates a Store. onEvent is optional and receives lifecycle events.
func NewStore(db *sql.DB, onEvent func(kind string, job Job)) *Store {
	return &Store{db: db, onEvent: onEvent}
}

// Create registers a new pending job.
func (s *Store) Create(kind, sessionKey string, payload map[string]interface{}) (Job, error) {
	j := Job{
		ID:         newID(),
		Kind:       kind,
		Status:     StatusPending,
		Progress:   0,
		Payload:    payload,
		SessionKey: sessionKey,
		CreatedAt:  now(),
		UpdatedAt:  now(),
	}
	cp, _ := json.Marshal(j.Checkpoints)
	pl, _ := json.Marshal(j.Payload)
	_, err := s.db.Exec(`INSERT INTO jobs (id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Kind, string(j.Status), j.Progress, string(cp), string(pl), j.SessionKey, "", 0, j.CreatedAt, j.UpdatedAt)
	if err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	s.emit(EventStarted, j)
	return j, nil
}

// Start moves a pending job to running and records a start time.
func (s *Store) Start(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, started_at=?, attempts=attempts+1, updated_at=? WHERE id=? AND status=?`,
		string(StatusRunning), t, t, id, string(StatusPending))
	if err != nil {
		return Job{}, fmt.Errorf("start job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id) // already running or otherwise; return current
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventStarted, j)
	}
	return j, err
}

// Progress records progress and appends an optional checkpoint.
func (s *Store) Progress(id string, p float64, checkpoint string) (Job, error) {
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	cur, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	if checkpoint != "" {
		cur.Checkpoints = append(cur.Checkpoints, checkpoint)
	}
	cp, _ := json.Marshal(cur.Checkpoints)
	t := now()
	if _, err := s.db.Exec(`UPDATE jobs SET progress=?, checkpoints=?, updated_at=? WHERE id=?`, p, string(cp), t, id); err != nil {
		return Job{}, fmt.Errorf("progress job: %w", err)
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventProgress, j)
	}
	return j, err
}

// Succeed marks a job done.
func (s *Store) Succeed(id string) (Job, error) {
	return s.finish(id, StatusSucceeded, "")
}

// Fail marks a job failed with a reason.
func (s *Store) Fail(id, errMsg string) (Job, error) {
	return s.finish(id, StatusFailed, errMsg)
}

// Cancel marks a job cancelled.
func (s *Store) Cancel(id string) (Job, error) {
	return s.finish(id, StatusCancelled, "")
}

func (s *Store) finish(id string, status Status, errMsg string) (Job, error) {
	t := now()
	if _, err := s.db.Exec(`UPDATE jobs SET status=?, error=?, finished_at=?, updated_at=? WHERE id=?`,
		string(status), errMsg, t, t, id); err != nil {
		return Job{}, fmt.Errorf("finish job: %w", err)
	}
	j, _ := s.Get(id)
	evt := map[Status]string{StatusSucceeded: EventDone, StatusFailed: EventFailed, StatusCancelled: EventCancelled}[status]
	s.emit(evt, j)
	return j, nil
}

// Retry moves a failed job back to pending for another attempt.
func (s *Store) Retry(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, error='', finished_at=NULL, updated_at=? WHERE id=? AND status IN (?,?)`,
		string(StatusPending), t, id, string(StatusFailed), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("retry job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id)
	}
	j, _ := s.Get(id)
	s.emit(EventRetrying, j)
	return j, nil
}

// MarkInterrupted flags any job left in "running" (e.g. from a crash) as
// interrupted, making them resumable via Retry. Called at startup.
func (s *Store) MarkInterrupted() (int, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, error=?, finished_at=?, updated_at=? WHERE status=?`,
		string(StatusInterrupted), "interrupted by restart", t, t, string(StatusRunning))
	if err != nil {
		return 0, fmt.Errorf("mark interrupted: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Get returns a job by id.
func (s *Store) Get(id string) (Job, error) {
	row := s.db.QueryRow(`SELECT id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, started_at, finished_at, updated_at FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// List returns jobs, optionally filtered by exact status.
func (s *Store) List(status Status) ([]Job, error) {
	query := `SELECT id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, started_at, finished_at, updated_at FROM jobs`
	args := []interface{}{}
	if status != "" {
		query += ` WHERE status=?`
		args = append(args, string(status))
	}
	query += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) emit(kind string, job Job) {
	if s.onEvent != nil && kind != "" {
		s.onEvent(kind, job)
	}
}

func scanJob(rs interface{ Scan(...interface{}) error }) (Job, error) {
	var j Job
	var status, checkpoints, payload string
	var startedAt, finishedAt sql.NullInt64
	var sessionKey, jobErr sql.NullString
	if err := rs.Scan(&j.ID, &j.Kind, &status, &j.Progress, &checkpoints, &payload, &sessionKey, &jobErr, &j.Attempts, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt); err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}
	j.Status = Status(status)
	j.SessionKey = sessionKey.String
	j.Error = jobErr.String
	_ = json.Unmarshal([]byte(checkpoints), &j.Checkpoints)
	if len(j.Checkpoints) == 0 {
		j.Checkpoints = nil
	}
	if payload != "" {
		_ = json.Unmarshal([]byte(payload), &j.Payload)
	}
	if startedAt.Valid {
		j.StartedAt = &startedAt.Int64
	}
	if finishedAt.Valid {
		j.FinishedAt = &finishedAt.Int64
	}
	return j, nil
}
