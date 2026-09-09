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
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	// Waiting states: the work is alive but blocked on something outside
	// itself. Waiting work survives restarts and resumes; it never silently
	// becomes failed or completed.
	StatusWaitingPermission Status = "waiting_for_permission"
	StatusWaitingUser       Status = "waiting_for_user"
	StatusPaused            Status = "paused"
	StatusRetrying          Status = "retrying"
	StatusExpired           Status = "expired"
	StatusSucceeded         Status = "succeeded"
	StatusFailed            Status = "failed"
	StatusCancelled         Status = "cancelled"
	StatusInterrupted       Status = "interrupted"
)

// Event kinds emitted by the store (the agent maps these to typed events).
const (
	EventStarted   = "task.started"
	EventProgress  = "task.progress"
	EventDone      = "task.completed"
	EventFailed    = "task.failed"
	EventCancelled = "task.cancelled"
	EventRetrying  = "task.retrying"
	EventWaiting   = "task.waiting"
	EventPaused    = "task.paused"
	EventResumed   = "task.resumed"
	EventExpired   = "task.expired"
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
	// Owner scopes the work to one Ghost principal; ContextID binds it to
	// one context (personal/work/...) so work never leaks across contexts.
	Owner     string `json:"owner,omitempty"`
	ContextID string `json:"context_id,omitempty"`
	// Generation guards against stale workers: every handoff mints a new
	// generation, and completions carrying an old one are rejected.
	Generation string `json:"generation,omitempty"`
	// Evidence is the human-safe record of what the work did (never
	// secrets); ResumeState is the durable cursor a restart resumes from.
	Evidence    string `json:"evidence,omitempty"`
	ResumeState string `json:"resume_state,omitempty"`
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
	return s.CreateWithScope(kind, sessionKey, "", "", payload)
}

// CreateWithScope registers a new pending job bound to one owner and
// context from birth. Unscoped legacy callers keep using Create.
func (s *Store) CreateWithScope(kind, sessionKey, owner, contextID string, payload map[string]interface{}) (Job, error) {
	j := Job{
		ID:         newID(),
		Kind:       kind,
		Status:     StatusPending,
		Progress:   0,
		Payload:    payload,
		SessionKey: sessionKey,
		Owner:      owner,
		ContextID:  contextID,
		Generation: newGeneration(),
		CreatedAt:  now(),
		UpdatedAt:  now(),
	}
	cp, _ := json.Marshal(j.Checkpoints)
	pl, _ := json.Marshal(j.Payload)
	_, err := s.db.Exec(`INSERT INTO jobs (id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, updated_at, owner, context_id, generation, evidence, resume_state)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Kind, string(j.Status), j.Progress, string(cp), string(pl), j.SessionKey, "", 0, j.CreatedAt, j.UpdatedAt,
		j.Owner, j.ContextID, j.Generation, "", "")
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

// SetWaiting moves a live job into an explicit waiting state with a
// reason recorded as evidence. Waiting work is alive: it survives
// restarts and resumes, and never silently becomes failed or completed.
func (s *Store) SetWaiting(id string, waiting Status, reason string) (Job, error) {
	if waiting != StatusWaitingPermission && waiting != StatusWaitingUser {
		return Job{}, fmt.Errorf("not a waiting status: %v", waiting)
	}
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, evidence=?, updated_at=? WHERE id=? AND status IN (?,?,?,?,?)`,
		string(waiting), reason, t, id,
		string(StatusPending), string(StatusRunning), string(StatusRetrying), string(StatusPaused), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("wait job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id)
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventWaiting, j)
	}
	return j, err
}

// Pause suspends a live job; Resume returns it to pending. Neither loses
// progress, evidence, or resume state.
func (s *Store) Pause(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, updated_at=? WHERE id=? AND status IN (?,?,?,?,?,?)`,
		string(StatusPaused), t, id,
		string(StatusPending), string(StatusRunning), string(StatusRetrying), string(StatusInterrupted),
		string(StatusWaitingPermission), string(StatusWaitingUser))
	if err != nil {
		return Job{}, fmt.Errorf("pause job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id)
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventPaused, j)
	}
	return j, err
}

// Resume returns a paused job to pending.
func (s *Store) Resume(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, updated_at=? WHERE id=? AND status=?`,
		string(StatusPending), t, id, string(StatusPaused))
	if err != nil {
		return Job{}, fmt.Errorf("resume job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id)
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventResumed, j)
	}
	return j, err
}

// Expire retires work whose deadline passed. Expired work never runs;
// it stays readable as history.
func (s *Store) Expire(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, finished_at=?, updated_at=? WHERE id=? AND status NOT IN (?,?,?,?,?)`,
		string(StatusExpired), t, t, id,
		string(StatusSucceeded), string(StatusFailed), string(StatusCancelled), string(StatusExpired), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("expire job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.Get(id)
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventExpired, j)
	}
	return j, err
}

// CancelWithReason cancels like Cancel but records why. The reason is
// evidence, shown in activity instead of a bare "cancelled".
func (s *Store) CancelWithReason(id, reason string) (Job, error) {
	j, err := s.finish(id, StatusCancelled, reason)
	return j, err
}

// SetEvidence replaces a job's human-safe evidence record. Secrets must
// never be stored here; callers are responsible for redacting first.
func (s *Store) SetEvidence(id, evidence string) error {
	_, err := s.db.Exec(`UPDATE jobs SET evidence=?, updated_at=? WHERE id=?`, evidence, now(), id)
	return err
}

// SetResumeState stores the durable cursor a restart resumes from: the
// last confirmed step plus whatever the next safe operation is. Never a
// promise that a side effect happened — only confirmed evidence resumes.
func (s *Store) SetResumeState(id, state string) error {
	_, err := s.db.Exec(`UPDATE jobs SET resume_state=?, updated_at=? WHERE id=?`, state, now(), id)
	return err
}

// RotateGeneration mints a fresh generation for a job and returns it.
// Every handoff (retry, resume, re-dispatch) rotates; completions
// carrying any older generation are stale and must be rejected via
// CheckGeneration.
func (s *Store) RotateGeneration(id string) (string, error) {
	gen := newGeneration()
	res, err := s.db.Exec(`UPDATE jobs SET generation=?, updated_at=? WHERE id=?`, gen, now(), id)
	if err != nil {
		return "", fmt.Errorf("rotate generation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("job %s not found", id)
	}
	return gen, nil
}

// CheckGeneration reports whether gen is the job's current generation. A
// late worker holding a rotated-out generation gets false and must drop
// its result instead of mutating live state.
func (s *Store) CheckGeneration(id, gen string) bool {
	if gen == "" {
		return false
	}
	var current string
	if err := s.db.QueryRow(`SELECT generation FROM jobs WHERE id=?`, id).Scan(&current); err != nil {
		return false
	}
	return current != "" && current == gen
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

// jobColumns is the single column list every read and write uses, so a
// schema change fails loudly in one place instead of silently shifting.
const jobColumns = `id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, started_at, finished_at, updated_at, owner, context_id, generation, evidence, resume_state`

// Get returns a job by id.
func (s *Store) Get(id string) (Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// List returns jobs, optionally filtered by exact status.
func (s *Store) List(status Status) ([]Job, error) {
	query := `SELECT ` + jobColumns + ` FROM jobs`
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
	var sessionKey, jobErr, owner, contextID, generation, evidence, resumeState sql.NullString
	if err := rs.Scan(&j.ID, &j.Kind, &status, &j.Progress, &checkpoints, &payload, &sessionKey, &jobErr, &j.Attempts, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt,
		&owner, &contextID, &generation, &evidence, &resumeState); err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}
	j.Status = Status(status)
	j.SessionKey = sessionKey.String
	j.Error = jobErr.String
	j.Owner = owner.String
	j.ContextID = contextID.String
	j.Generation = generation.String
	j.Evidence = evidence.String
	j.ResumeState = resumeState.String
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
