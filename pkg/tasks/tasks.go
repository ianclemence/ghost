// Package tasks provides a durable, resumable job store for Ghost. Schedules
// (cron) are not the same thing as durable work: a job can be checkpointed,
// retried, cancelled, and resumed after a provider failure or process restart.
// It is SQLite-backed and part of Ghost State, not a separate service.
package tasks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
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
	EventStarted     = "task.started"
	EventProgress    = "task.progress"
	EventDone        = "task.completed"
	EventFailed      = "task.failed"
	EventCancelled   = "task.cancelled"
	EventRetrying    = "task.retrying"
	EventWaiting     = "task.waiting"
	EventPaused      = "task.paused"
	EventResumed     = "task.resumed"
	EventExpired     = "task.expired"
	EventInterrupted = "task.interrupted"
	// EventCheckpointed marks a Progress call that actually recorded a
	// checkpoint (as opposed to a bare progress heartbeat).
	EventCheckpointed = "task.checkpointed"
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
	// TrajectoryID links the job's lifecycle events to the execution trace
	// that created it (empty for jobs with no originating turn).
	TrajectoryID string `json:"trajectory_id,omitempty"`
	// NextAttemptAt is when a scheduled retry becomes due (Unix seconds,
	// 0 = due now). It is what a runner reads to decide the job waits.
	NextAttemptAt int64 `json:"next_attempt_at,omitempty"`
	// MaxAttempts caps attempts for this job (0 = runner's default). A
	// job that has spent its attempts fails for good instead of looping.
	MaxAttempts int `json:"max_attempts,omitempty"`
	// Failures counts attempts that ended in a scheduled retry, which is
	// what the retry budget is spent against. Pauses for approval never
	// touch it: waiting on you is not a failure.
	Failures int `json:"failures,omitempty"`
}

// TransitionError reports a rejected state-machine move. Moves that would
// make dead work live again (start/resume/retry/pause/wait/expire out of a
// terminal state) fail loudly instead of silently no-op-ing: a caller that
// thinks it restarted a finished job must find out, not proceed on a stale
// assumption. Same-outcome completions (succeeding an already-succeeded job)
// stay idempotent — first terminal outcome wins, duplicates acknowledge it.
type TransitionError struct {
	JobID string
	From  Status
	Op    string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid task transition: %s on job %s in terminal state %s (create a new job or Retry an explicit failure instead)", e.Op, e.JobID, e.From)
}

// terminal reports whether no further transition may leave the state
// (interrupted is restartable via Retry, so it is not terminal here).
func terminal(s Status) bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// rejectIfTerminal returns a TransitionError when the job sits in a true
// terminal state that op may not leave. Same-state completion callers
// handle their own idempotency; everything else funnels through here.
func rejectIfTerminal(cur Job, op string) error {
	if terminal(cur.Status) {
		return &TransitionError{JobID: cur.ID, From: cur.Status, Op: op}
	}
	return nil
}

// Store persists jobs in SQLite.
type Store struct {
	db      *sql.DB
	onEvent func(kind string, job Job)
	// stream receives durable task-lifecycle events (restart-surviving
	// evidence). Nil = disabled; set by the runtime that owns a stream.
	stream *cevents.Stream
}

// SetEventStream installs the canonical stream for durable task events.
// Nil-safe: a nil stream disables durable emission.
func (s *Store) SetEventStream(st *cevents.Stream) { s.stream = st }

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
	return s.CreateWithTrajectory(kind, sessionKey, owner, contextID, "", payload)
}

// CreateWithTrajectory is CreateWithScope plus the originating turn's
// trajectory, so the job's lifecycle events join that execution trace.
func (s *Store) CreateWithTrajectory(kind, sessionKey, owner, contextID, trajectoryID string, payload map[string]interface{}) (Job, error) {
	j := Job{
		ID:           newID(),
		Kind:         kind,
		Status:       StatusPending,
		Progress:     0,
		Payload:      payload,
		SessionKey:   sessionKey,
		Owner:        owner,
		ContextID:    contextID,
		Generation:   newGeneration(),
		TrajectoryID: trajectoryID,
		CreatedAt:    now(),
		UpdatedAt:    now(),
	}
	cp, _ := json.Marshal(j.Checkpoints)
	pl, _ := json.Marshal(j.Payload)
	_, err := s.db.Exec(`INSERT INTO jobs (id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, updated_at, owner, context_id, generation, evidence, resume_state, trajectory_id, next_attempt_at, max_attempts, failures)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		j.ID, j.Kind, string(j.Status), j.Progress, string(cp), string(pl), j.SessionKey, "", 0, j.CreatedAt, j.UpdatedAt,
		j.Owner, j.ContextID, j.Generation, "", "", j.TrajectoryID, 0, 0, 0)
	if err != nil {
		return Job{}, fmt.Errorf("create job: %w", err)
	}
	s.emit(EventStarted, j)
	return j, nil
}

// Start moves a pending job to running and records a start time. It clears
// the pending attempt time, so a job being run right now is never also due.
func (s *Store) Start(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, started_at=?, attempts=attempts+1, next_attempt_at=0, updated_at=? WHERE id=? AND status=?`,
		string(StatusRunning), t, t, id, string(StatusPending))
	if err != nil {
		return Job{}, fmt.Errorf("start job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.noopOrReject(id, "start")
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventStarted, j)
	}
	return j, err
}

// noopOrReject resolves a guarded UPDATE that matched zero rows: same-state
// re-entry returns the current job silently (idempotent), but a move out of
// a terminal state is an explicit error — the caller must not proceed as if
// dead work restarted.
func (s *Store) noopOrReject(id, op string) (Job, error) {
	cur, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	if rerr := rejectIfTerminal(cur, op); rerr != nil {
		return cur, rerr
	}
	return cur, nil
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
	if rerr := rejectIfTerminal(cur, "progress"); rerr != nil {
		return cur, rerr
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
		if checkpoint != "" {
			s.emit(EventCheckpointed, j)
		}
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
	// Only LIVE jobs may finish. A terminal job (already succeeded,
	// failed, cancelled, or expired) is never overwritten by a stale or
	// duplicate completion — the first terminal outcome wins. Repeating
	// the SAME outcome stays silent (idempotent completion); claiming a
	// DIFFERENT outcome for dead work is an explicit error.
	cur, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	if terminal(cur.Status) {
		if cur.Status == status {
			return cur, nil
		}
		return cur, &TransitionError{JobID: id, From: cur.Status, Op: "finish:" + string(status)}
	}
	res, err := s.db.Exec(`UPDATE jobs SET status=?, error=?, finished_at=?, updated_at=? WHERE id=? AND status IN (?,?,?,?,?,?,?)`,
		string(status), errMsg, t, t, id,
		string(StatusPending), string(StatusRunning), string(StatusRetrying),
		string(StatusWaitingPermission), string(StatusWaitingUser),
		string(StatusPaused), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("finish job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Lost a race with a concurrent transition after the pre-check;
		// report current state without claiming the finish.
		return s.Get(id)
	}
	j, _ := s.Get(id)
	evt := map[Status]string{StatusSucceeded: EventDone, StatusFailed: EventFailed, StatusCancelled: EventCancelled}[status]
	s.emit(evt, j)
	return j, nil
}

// Retry moves a failed job back to pending for another attempt.
func (s *Store) Retry(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, error='', finished_at=NULL, next_attempt_at=0, updated_at=? WHERE id=? AND status IN (?,?)`,
		string(StatusPending), t, id, string(StatusFailed), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("retry job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.noopOrReject(id, "retry")
	}
	j, _ := s.Get(id)
	s.emit(EventRetrying, j)
	return j, nil
}

// Snooze returns a job to the queue for a later attempt without spending
// one of its attempts. It is for "not now" rather than "not this": a
// concurrency slot was full, a rate limit asked us to come back. Nothing
// failed, and charging the budget would let a busy minute eventually kill
// work that was never wrong.
func (s *Store) Snooze(id string, delay time.Duration, reason string) (Job, error) {
	return s.schedule(id, delay, reason, false)
}

// SetRetryPolicy records how many attempts this job is allowed. A job whose
// MaxAttempts is already written keeps it: the stricter of the two wins
// nowhere, the job's own word wins, because whoever set it knew what the
// work costs.
func (s *Store) SetRetryPolicy(id string, maxAttempts int) error {
	if maxAttempts < 0 {
		return fmt.Errorf("max attempts cannot be negative")
	}
	_, err := s.db.Exec(`UPDATE jobs SET max_attempts=?, updated_at=? WHERE id=?`, maxAttempts, now(), id)
	if err != nil {
		return fmt.Errorf("set retry policy: %w", err)
	}
	return nil
}

// ScheduleRetry returns running work to the queue for a later attempt,
// recording why and charging the attempt. The job becomes due again after
// delay, which is how transient failure turns into a wait instead of a
// spin: a provider that is down for two minutes should cost two minutes,
// not a failed job and an owner who has to ask again.
//
// It never schedules terminal work (cancelled/expired stay as they are)
// and never invents an attempt: Start still owns the count.
func (s *Store) ScheduleRetry(id string, delay time.Duration, reason string) (Job, error) {
	return s.schedule(id, delay, reason, true)
}

// schedule is the shared body: after delay, the job is pending again with
// reason recorded. chargeAttempt decides whether this counts against the
// retry budget — a failure does, a "not now" does not.
func (s *Store) schedule(id string, delay time.Duration, reason string, chargeAttempt bool) (Job, error) {
	if delay < 0 {
		delay = 0
	}
	t := now()
	// Attempts are counted in seconds, like every other time in this
	// table, and rounded up: a wait is never cut short by truncation, so
	// asking for 400ms waits one second rather than none.
	delaySec := int64((delay + time.Second - 1) / time.Second)
	due := t + delaySec
	if due < t {
		due = t
	}
	claim := `status=?, error=?, finished_at=NULL, next_attempt_at=?, updated_at=?`
	if chargeAttempt {
		claim = `status=?, error=?, failures=failures+1, finished_at=NULL, next_attempt_at=?, updated_at=?`
	}
	res, err := s.db.Exec(`UPDATE jobs SET `+claim+` WHERE id=? AND status IN (?,?,?)`,
		string(StatusPending), reason, due, t, id,
		string(StatusRunning), string(StatusFailed), string(StatusInterrupted))
	if err != nil {
		return Job{}, fmt.Errorf("schedule retry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.noopOrReject(id, "schedule-retry")
	}
	j, _ := s.Get(id)
	s.emit(EventRetrying, j)
	return j, nil
}

// ListDue returns pending jobs whose attempt time has arrived, oldest first
// and at most limit (0 = no limit). A pending job with no attempt time is
// due now, so newly created work runs without waiting for a tick.
func (s *Store) ListDue(limit int) ([]Job, error) {
	query := `SELECT ` + jobColumns + ` FROM jobs WHERE status=? AND next_attempt_at<=? ORDER BY next_attempt_at, created_at`
	args := []interface{}{string(StatusPending), now()}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list due jobs: %w", err)
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
		return s.noopOrReject(id, "wait")
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
		return s.noopOrReject(id, "pause")
	}
	j, err := s.Get(id)
	if err == nil {
		s.emit(EventPaused, j)
	}
	return j, err
}

// Resume returns a waiting or paused job to pending. Waiting work is live
// but blocked on something outside itself (permission, user); once that
// blocker clears (approval granted, user replied), Resume is how it moves
// back to the runnable queue. Resuming dead work is an explicit error:
// a terminal job never comes back — Retry an explicit failure or create a
// new job instead.
//
// The attempt time is cleared: an approval that landed is not a wait, and
// a job that parks to ask must not be held back behind a backoff window
// nobody set for it.
func (s *Store) Resume(id string) (Job, error) {
	t := now()
	res, err := s.db.Exec(`UPDATE jobs SET status=?, next_attempt_at=0, updated_at=? WHERE id=? AND status IN (?,?,?)`,
		string(StatusPending), t, id,
		string(StatusPaused), string(StatusWaitingPermission), string(StatusWaitingUser))
	if err != nil {
		return Job{}, fmt.Errorf("resume job: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.noopOrReject(id, "resume")
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
		return s.noopOrReject(id, "expire")
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
// interrupted, making them resumable via Retry. Called at startup. Each
// recovered job also rotates its generation: a zombie worker from before
// the crash still holds the old generation, and CheckGeneration will now
// refuse its late completions instead of letting them land on the resumed
// execution. Emits task.interrupted per recovered job.
func (s *Store) MarkInterrupted() (int, error) {
	rows, err := s.db.Query(`SELECT id FROM jobs WHERE status=?`, string(StatusRunning))
	if err != nil {
		return 0, fmt.Errorf("mark interrupted: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, fmt.Errorf("mark interrupted: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("mark interrupted: %w", err)
	}
	return len(ids), s.interruptRunning(ids)
}

// InterruptOne flags a single job the runner had to let go of — a shutdown
// under it, a caller cancelling — as interrupted instead of failed. The
// work was not defeated; our stop is not its failure, and charging it an
// attempt would eventually fail a job for a reason it never had.
func (s *Store) InterruptOne(id string) error {
	if id == "" {
		return fmt.Errorf("interrupt job: empty id")
	}
	return s.interruptRunning([]string{id})
}

func (s *Store) interruptRunning(ids []string) error {
	t := now()
	for _, id := range ids {
		gen := newGeneration()
		if _, err := s.db.Exec(`UPDATE jobs SET status=?, error=?, finished_at=?, generation=?, updated_at=? WHERE id=? AND status=?`,
			string(StatusInterrupted), "interrupted by restart", t, gen, t, id, string(StatusRunning)); err != nil {
			return fmt.Errorf("mark interrupted: %w", err)
		}
		// Only a job that actually moved is worth an event: InterruptOne
		// can be called on work that has since been cancelled or finished,
		// and announcing a restart for it would be a lie about history.
		if j, err := s.Get(id); err == nil && j.Status == StatusInterrupted {
			s.emit(EventInterrupted, j)
		}
	}
	return nil
}

// jobColumns is the single column list every read and write uses, so a
// schema change fails loudly in one place instead of silently shifting.
const jobColumns = `id, kind, status, progress, checkpoints, payload, session_key, error, attempts, created_at, started_at, finished_at, updated_at, owner, context_id, generation, evidence, resume_state, trajectory_id, next_attempt_at, max_attempts, failures`

// Get returns a job by id.
func (s *Store) Get(id string) (Job, error) {
	row := s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id=?`, id)
	return scanJob(row)
}

// ListActiveBySession returns the durable jobs for a session that are still in
// flight (not terminal), newest first. The agent loop surfaces a compact
// summary of these to the model instead of letting it reconstruct task state
// from conversation history.
func (s *Store) ListActiveBySession(sessionKey string) ([]Job, error) {
	if s == nil || sessionKey == "" {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT `+jobColumns+` FROM jobs WHERE session_key=? AND status NOT IN (?,?,?,?) ORDER BY updated_at DESC LIMIT 20`,
		sessionKey, string(StatusSucceeded), string(StatusFailed), string(StatusCancelled), string(StatusExpired))
	if err != nil {
		return nil, fmt.Errorf("list active jobs: %w", err)
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
	s.publishTaskEvent(kind, job)
}

// publishTaskEvent mirrors a lifecycle transition onto the canonical
// stream as restart-surviving evidence. Registration (pending) records
// task.created; only a running job records task.started. Bare progress
// heartbeats stay out of the warehouse — only real checkpoints land.
// Background-originated jobs carry no trajectory (honest empty); the
// turn-driven executor (Phase 5) will stamp the originating trajectory.
func (s *Store) publishTaskEvent(kind string, job Job) {
	if s.stream == nil || kind == "" {
		return
	}
	var typ cevents.Type
	switch kind {
	case EventStarted:
		typ = cevents.TaskCreated
		if job.Status == StatusRunning {
			typ = cevents.TaskStarted
		}
	case EventProgress:
		return // heartbeat; checkpoints publish via EventCheckpointed
	case EventCheckpointed:
		typ = cevents.TaskCheckpointed
	case EventDone:
		typ = cevents.TaskCompleted
	case EventFailed:
		typ = cevents.TaskFailed
	case EventCancelled:
		typ = cevents.TaskCancelled
	case EventRetrying:
		typ = cevents.TaskRetrying
	case EventWaiting:
		typ = cevents.TaskWaiting
	case EventPaused:
		typ = cevents.TaskPaused
	case EventResumed:
		typ = cevents.TaskResumed
	case EventExpired:
		typ = cevents.TaskExpired
	case EventInterrupted:
		typ = cevents.TaskInterrupted
	default:
		return
	}
	s.stream.Publish(&cevents.Event{
		Type: typ, SessionID: job.SessionKey, TrajectoryID: job.TrajectoryID,
		Status: string(job.Status),
		Payload: map[string]interface{}{
			"job_id":   job.ID,
			"kind":     job.Kind,
			"owner":    job.Owner,
			"context":  job.ContextID,
			"attempts": job.Attempts,
			"progress": job.Progress,
			"error":    job.Error,
		},
	})
}

func scanJob(rs interface{ Scan(...interface{}) error }) (Job, error) {
	var j Job
	var status, checkpoints, payload string
	var startedAt, finishedAt sql.NullInt64
	var sessionKey, jobErr, owner, contextID, generation, evidence, resumeState, trajectoryID sql.NullString
	if err := rs.Scan(&j.ID, &j.Kind, &status, &j.Progress, &checkpoints, &payload, &sessionKey, &jobErr, &j.Attempts, &j.CreatedAt, &startedAt, &finishedAt, &j.UpdatedAt,
		&owner, &contextID, &generation, &evidence, &resumeState, &trajectoryID, &j.NextAttemptAt, &j.MaxAttempts, &j.Failures); err != nil {
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
	j.TrajectoryID = trajectoryID.String
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

// PruneFinished deletes jobs that reached a terminal outcome before the
// cutoff. Interrupted jobs are restartable and are never pruned, nor are
// live jobs. Returns the number removed.
func (s *Store) PruneFinished(before time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM jobs WHERE status IN (?,?,?,?) AND finished_at IS NOT NULL AND finished_at < ?`,
		string(StatusSucceeded), string(StatusFailed), string(StatusCancelled), string(StatusExpired), before.Unix())
	if err != nil {
		return 0, fmt.Errorf("prune jobs: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
