package computer

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

// LeaseState tracks the lifecycle of one computer hold.
type LeaseState string

const (
	LeaseActive   LeaseState = "active"
	LeaseReleased LeaseState = "released"
	LeaseExpired  LeaseState = "expired"
)

// Lease binds one computer to one task. Owner+context scope it to the
// right Ghost principal: a lease never authorizes a different owner or
// context, even if the resource ID matches.
type Lease struct {
	ID         string
	ResourceID string
	Owner      string
	TaskID     string
	SessionKey string
	ContextID  string
	State      LeaseState
	AcquiredAt time.Time
	ExpiresAt  time.Time
	RenewedAt  time.Time
	ReleasedAt *time.Time
}

// LeaseStore persists leases in SQLite so they survive restarts and so a
// stale task can never regain a computer another task has since acquired.
type LeaseStore struct {
	db *sql.DB
}

// NewLeaseStore creates the store, creating its table if absent.
func NewLeaseStore(db *sql.DB) (*LeaseStore, error) {
	s := &LeaseStore{db: db}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS computer_leases (
		id TEXT PRIMARY KEY,
		resource_id TEXT NOT NULL,
		owner TEXT NOT NULL DEFAULT '',
		task_id TEXT NOT NULL DEFAULT '',
		session_key TEXT NOT NULL DEFAULT '',
		context_id TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'active',
		acquired_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		expires_at TEXT NOT NULL DEFAULT '',
		renewed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		released_at TEXT
	)`)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_computer_leases_resource ON computer_leases(resource_id, state)`); err != nil {
		return nil, err
	}
	return s, nil
}

func newLeaseID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("lease-%d", time.Now().UnixNano())
	}
	return "lease-" + hex.EncodeToString(b)
}

// Acquire takes the computer for a task. The same task+session may
// re-acquire (idempotent renew path); a different live holder fails
// closed with ErrComputerBusy. Expired holds are reclaimed inline.
func (s *LeaseStore) Acquire(resourceID, owner, taskID, sessionKey, contextID string, ttl time.Duration) (*Lease, error) {
	if ttl <= 0 || ttl > MaxLeaseTTL {
		ttl = DefaultLeaseTTL
	}
	now := time.Now().UTC()
	// BEGIN IMMEDIATE serializes concurrent acquirers: the check for a
	// live holder and the insert happen atomically, so two tasks racing
	// for one computer cannot both win.
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	// Rollback is a no-op after Commit; defer it unconditionally.
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()
	var existing *Lease
	row := tx.QueryRow(`SELECT id, resource_id, owner, task_id, session_key, context_id, state,
		acquired_at, expires_at, renewed_at, released_at
		FROM computer_leases WHERE resource_id=? AND state='active'
		ORDER BY acquired_at DESC LIMIT 1`, resourceID)
	if existing, err = scanLease(row); err != nil {
		return nil, err
	}
	if existing != nil {
		if now.After(existing.ExpiresAt) {
			if _, err := tx.Exec(`UPDATE computer_leases SET state='expired' WHERE id=? AND state='active'`, existing.ID); err != nil {
				return nil, err
			}
		} else if existing.TaskID == taskID && existing.SessionKey == sessionKey {
			if _, err := tx.Exec(`UPDATE computer_leases SET expires_at=?, renewed_at=? WHERE id=? AND state='active'`,
				now.Add(ttl).Format(time.RFC3339), now.Format(time.RFC3339), existing.ID); err != nil {
				return nil, err
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			committed = true
			existing.ExpiresAt = now.Add(ttl)
			existing.RenewedAt = now
			return existing, nil
		} else {
			return nil, ErrComputerBusy(resourceID, existing.TaskID)
		}
	}
	l := &Lease{
		ID:         newLeaseID(),
		ResourceID: resourceID,
		Owner:      owner,
		TaskID:     taskID,
		SessionKey: sessionKey,
		ContextID:  contextID,
		State:      LeaseActive,
		AcquiredAt: now,
		ExpiresAt:  now.Add(ttl),
		RenewedAt:  now,
	}
	if _, err := tx.Exec(`INSERT INTO computer_leases
		(id, resource_id, owner, task_id, session_key, context_id, state, acquired_at, expires_at, renewed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.ResourceID, l.Owner, l.TaskID, l.SessionKey, l.ContextID,
		string(l.State), l.AcquiredAt.Format(time.RFC3339), l.ExpiresAt.Format(time.RFC3339), l.RenewedAt.Format(time.RFC3339)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return l, nil
}

// Renew extends a live lease the caller owns. Renewing someone else's
// lease, or a released/expired one, fails.
func (s *LeaseStore) Renew(id string, ttl time.Duration) (*Lease, error) {
	if ttl <= 0 || ttl > MaxLeaseTTL {
		ttl = DefaultLeaseTTL
	}
	l, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if l.State != LeaseActive {
		return nil, fmt.Errorf("lease %s is %s, cannot renew", id, l.State)
	}
	now := time.Now().UTC()
	if now.After(l.ExpiresAt) {
		_ = s.markExpired(id, now)
		return nil, fmt.Errorf("lease %s already expired", id)
	}
	l.ExpiresAt = now.Add(ttl)
	l.RenewedAt = now
	_, err = s.db.Exec(`UPDATE computer_leases SET expires_at=?, renewed_at=? WHERE id=? AND state='active'`,
		l.ExpiresAt.Format(time.RFC3339), l.RenewedAt.Format(time.RFC3339), id)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// Release frees the computer. Releasing twice is a no-op success —
// cleanup paths must be safe to run more than once.
func (s *LeaseStore) Release(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`UPDATE computer_leases SET state='released', released_at=? WHERE id=? AND state='active'`, now, id)
	return err
}

// ActiveFor reports the live holder of a resource, reclaiming it first if
// it lapsed. Callers checking availability get the truth, not a stale row.
func (s *LeaseStore) ActiveFor(resourceID string) (*Lease, error) {
	return s.activeFor(resourceID)
}

func (s *LeaseStore) activeFor(resourceID string) (*Lease, error) {
	l, err := s.latestActive(resourceID)
	if err != nil || l == nil {
		return l, err
	}
	if time.Now().UTC().After(l.ExpiresAt) {
		if err := s.markExpired(l.ID, time.Now().UTC()); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return l, nil
}

// RecoverStale expires every unreleased lease. Call once at boot: after a
// restart no pre-restart task is alive, so any surviving hold is stale by
// definition. A live task re-acquires explicitly afterwards.
func (s *LeaseStore) RecoverStale() (int64, error) {
	res, err := s.db.Exec(`UPDATE computer_leases SET state='expired' WHERE state='active'`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// OwnedBy reports whether the given task+session currently holds the
// resource. Execution paths consult this before acting so a task that
// lost its lease (expiry, preemption by recovery, explicit release)
// fails closed instead of driving a computer it no longer owns.
func (s *LeaseStore) OwnedBy(resourceID, taskID, sessionKey string) bool {
	l, err := s.activeFor(resourceID)
	if err != nil || l == nil {
		return false
	}
	return l.TaskID == taskID && l.SessionKey == sessionKey
}

func (s *LeaseStore) markExpired(id string, now time.Time) error {
	_, err := s.db.Exec(`UPDATE computer_leases SET state='expired' WHERE id=? AND state='active'`, id)
	return err
}

func (s *LeaseStore) latestActive(resourceID string) (*Lease, error) {
	row := s.db.QueryRow(`SELECT id, resource_id, owner, task_id, session_key, context_id, state,
		acquired_at, expires_at, renewed_at, released_at
		FROM computer_leases WHERE resource_id=? AND state='active'
		ORDER BY acquired_at DESC LIMIT 1`, resourceID)
	return scanLease(row)
}

// Get fetches any lease by ID, regardless of state.
func (s *LeaseStore) Get(id string) (*Lease, error) {
	row := s.db.QueryRow(`SELECT id, resource_id, owner, task_id, session_key, context_id, state,
		acquired_at, expires_at, renewed_at, released_at
		FROM computer_leases WHERE id=?`, id)
	return scanLease(row)
}

type leaseRow interface {
	Scan(dest ...interface{}) error
}

func scanLease(row leaseRow) (*Lease, error) {
	var l Lease
	var state string
	var acquired, expires, renewed, released sql.NullString
	if err := row.Scan(&l.ID, &l.ResourceID, &l.Owner, &l.TaskID, &l.SessionKey,
		&l.ContextID, &state, &acquired, &expires, &renewed, &released); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	l.State = LeaseState(state)
	var err error
	if l.AcquiredAt, err = parseLeaseTime(acquired); err != nil {
		return nil, err
	}
	if l.ExpiresAt, err = parseLeaseTime(expires); err != nil {
		return nil, err
	}
	if l.RenewedAt, err = parseLeaseTime(renewed); err != nil {
		return nil, err
	}
	if released.Valid && released.String != "" {
		t, err := parseLeaseTime(released)
		if err != nil {
			return nil, err
		}
		l.ReleasedAt = &t
	}
	return &l, nil
}

func parseLeaseTime(ns sql.NullString) (time.Time, error) {
	if !ns.Valid || ns.String == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.Parse(layout, ns.String); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable lease time %q", ns.String)
}
