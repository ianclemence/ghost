package browser

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"time"
)

// Session binds one browsing flow to exactly one owner+context+task.
// The profile (cookie jar, storage) belongs to the session's context:
// two contexts never share a profile, so a personal login can never leak
// into a work session through a shared cookie store.
type Session struct {
	ID        string
	Profile   string
	Owner     string
	ContextID string
	TaskID    string
	CreatedAt time.Time
	LastUsed  time.Time
	ExpiresAt time.Time
}

// DefaultSessionTTL bounds how long an idle browser session survives.
const DefaultSessionTTL = 30 * time.Minute

// safeID matches owner/context/task/profile identifiers safe for paths
// and SQL alike. Anything else is rejected rather than sanitized into
// something surprising.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func checkID(what, v string) error {
	if !safeID.MatchString(v) {
		return fmt.Errorf("browser: invalid %s %q", what, v)
	}
	return nil
}

// ProfileDir resolves the isolated storage directory for a context's
// profile under base. The layout (base/context/profile) makes cross
// context sharing structurally impossible.
func ProfileDir(base, contextID, profile string) (string, error) {
	if err := checkID("context", contextID); err != nil {
		return "", err
	}
	if err := checkID("profile", profile); err != nil {
		return "", err
	}
	if base == "" {
		return "", fmt.Errorf("browser: empty profile base")
	}
	return filepath.Join(base, contextID, profile), nil
}

// SessionStore persists browser sessions in SQLite so expiry and cleanup
// survive restarts. Cookies and storage live in profile dirs, never in
// this table and never in backups as active sessions.
type SessionStore struct {
	db      *sql.DB
	baseDir string
}

// EnsureSchema creates the session ledger table and index. It is the one
// canonical DDL: stores, migrations, and tests all call it, so the schema
// cannot drift between them. Idempotent.
func EnsureSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS browser_sessions (
		id TEXT PRIMARY KEY,
		profile TEXT NOT NULL DEFAULT '',
		owner TEXT NOT NULL DEFAULT '',
		context_id TEXT NOT NULL DEFAULT '',
		task_id TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		last_used_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		expires_at TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		return fmt.Errorf("browser session table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_browser_sessions_owner ON browser_sessions(owner, context_id)`); err != nil {
		return fmt.Errorf("browser session index: %w", err)
	}
	return nil
}

// NewSessionStore creates the store, creating its table if absent.
func NewSessionStore(db *sql.DB, baseDir string) (*SessionStore, error) {
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	return &SessionStore{db: db, baseDir: baseDir}, nil
}

func newSessionID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return "sess-" + hex.EncodeToString(b)
}

// GetOrCreate returns the live session for an owner+context+task, or
// mints one bound to the context's isolated profile. A task reuses its
// own session across turns; a different task never inherits it.
func (s *SessionStore) GetOrCreate(owner, contextID, taskID, profile string, ttl time.Duration) (*Session, error) {
	if err := checkID("owner", owner); err != nil {
		return nil, err
	}
	if err := checkID("context", contextID); err != nil {
		return nil, err
	}
	if err := checkID("task", taskID); err != nil {
		return nil, err
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	now := time.Now().UTC()
	var existing *Session
	row := s.db.QueryRow(`SELECT id, profile, owner, context_id, task_id, created_at, last_used_at, expires_at
		FROM browser_sessions WHERE owner=? AND context_id=? AND task_id=? AND expires_at > ?
		ORDER BY last_used_at DESC LIMIT 1`,
		owner, contextID, taskID, now.Format(time.RFC3339))
	if sess, err := scanSession(row); err == nil && sess != nil {
		existing = sess
	} else if err != nil {
		return nil, err
	}
	if existing != nil {
		if _, err := s.db.Exec(`UPDATE browser_sessions SET last_used_at=?, expires_at=? WHERE id=?`,
			now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339), existing.ID); err != nil {
			return nil, err
		}
		existing.LastUsed = now
		existing.ExpiresAt = now.Add(ttl)
		return existing, nil
	}
	profileDir, err := ProfileDir(s.baseDir, contextID, profile)
	if err != nil {
		return nil, err
	}
	sess := &Session{
		ID:        newSessionID(),
		Profile:   profileDir,
		Owner:     owner,
		ContextID: contextID,
		TaskID:    taskID,
		CreatedAt: now,
		LastUsed:  now,
		ExpiresAt: now.Add(ttl),
	}
	_, err = s.db.Exec(`INSERT INTO browser_sessions
		(id, profile, owner, context_id, task_id, created_at, last_used_at, expires_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		sess.ID, sess.Profile, sess.Owner, sess.ContextID, sess.TaskID,
		sess.CreatedAt.Format(time.RFC3339), sess.LastUsed.Format(time.RFC3339), sess.ExpiresAt.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// Touch refreshes a session's lifetime.
func (s *SessionStore) Touch(id string, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE browser_sessions SET last_used_at=?, expires_at=?
		WHERE id=? AND expires_at > ?`,
		now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339), id, now.Format(time.RFC3339))
	return err
}

// Close ends a session now (task done, cancelled, or context revoked).
func (s *SessionStore) Close(id string) error {
	_, err := s.db.Exec(`UPDATE browser_sessions SET expires_at=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// ExpireSweep removes long-dead sessions so the table stays bounded.
// Only rows expired well past their TTL are deleted; live ones stay.
func (s *SessionStore) ExpireSweep(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM browser_sessions WHERE expires_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type sessionRow interface {
	Scan(dest ...interface{}) error
}

func scanSession(row sessionRow) (*Session, error) {
	var s Session
	var created, used, expires string
	if err := row.Scan(&s.ID, &s.Profile, &s.Owner, &s.ContextID, &s.TaskID,
		&created, &used, &expires); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var err error
	if s.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return nil, err
	}
	if s.LastUsed, err = time.Parse(time.RFC3339, used); err != nil {
		return nil, err
	}
	if s.ExpiresAt, err = time.Parse(time.RFC3339, expires); err != nil {
		return nil, err
	}
	return &s, nil
}
