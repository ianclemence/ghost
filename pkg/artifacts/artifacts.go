// Package artifacts is Ghost's runtime-validated handoff primitive:
// "things Ghost hands you" (documents, images, links, result summaries).
//
// Authority rule: the model proposes, the runtime disposes. An artifact
// exists only after this package validates it against workspace bounds,
// the protected estate, and (for files) actual existence. Mobile renders
// what this package returns and never infers success from model prose.
//
// Actions are render hints computed server-side (preview/open/download
// applicability) plus no execution authority: consequential follow-ups
// ("book it") travel as new conversation turns through the normal
// capability + Permission Broker path. The client decides nothing.
package artifacts

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/redact"
)

// Kinds are deliberately coarse. Semantic rendering (email looks like
// email) is a presentation concern; authority needs only to know how to
// validate, persist, and preview the payload.
const (
	KindFile = "file"
	KindText = "text"
	KindLink = "link"
)

// States.
const (
	StateAvailable   = "available"
	StateUnavailable = "unavailable"
)

// Action kinds are client render hints, never execution grants.
const (
	ActionPreview  = "preview"
	ActionOpen     = "open"
	ActionDownload = "download"
)

const (
	maxTitleLen   = 200
	maxSummaryLen = 2000
	maxTextLen    = 65536
	maxFileBytes  = 8 << 20
)

// protectedPrefixes mirrors the workspace file boundary
// (cmd/ghost/internal_api.go workspaceFileProtected) plus the memory
// ScopeGuard estate (pkg/tools/privacy_guard.go): nothing under these
// paths may ever become a user-facing handoff.
var protectedPrefixes = []string{
	"personal-context/",
	"state/",
	"events/",
	"knowledge/self/",
}

// Action is one backend-declared affordance on an artifact.
type Action struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// Artifact is the canonical, runtime-validated handoff record.
type Artifact struct {
	ID          string    `json:"id"`
	SessionKey  string    `json:"session_key"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary,omitempty"`
	Path        string    `json:"path,omitempty"` // workspace-relative, file kind only
	Text        string    `json:"text,omitempty"` // text kind only, bounded
	URL         string    `json:"url,omitempty"`  // link kind only
	State       string    `json:"state"`
	Reason      string    `json:"reason,omitempty"`
	Actions     []Action  `json:"actions"`
	EvidenceRef string    `json:"evidence_request_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// Input is the model proposal. Exactly one of Path/Text/URL must be set.
type Input struct {
	SessionKey  string
	Kind        string
	Title       string
	Summary     string
	Path        string
	Text        string
	URL         string
	EvidenceRef string
}

// Store persists artifacts in the Ghost database.
type Store struct {
	db        *sql.DB
	workspace string
}

// EnsureSchema creates the artifacts table and index if missing.
// Idempotent: safe to call from migrations and store construction.
func EnsureSchema(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS artifacts (
		id TEXT PRIMARY KEY,
		session_key TEXT NOT NULL,
		kind TEXT NOT NULL,
		title TEXT NOT NULL,
		summary TEXT NOT NULL DEFAULT '',
		path TEXT NOT NULL DEFAULT '',
		text TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		state TEXT NOT NULL DEFAULT 'available',
		reason TEXT NOT NULL DEFAULT '',
		actions TEXT NOT NULL DEFAULT '[]',
		evidence_request_id TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("artifacts: schema: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_artifacts_session ON artifacts(session_key)`); err != nil {
		return fmt.Errorf("artifacts: index: %w", err)
	}
	return nil
}

// NewStore opens the store, creating the table if needed.
func NewStore(db *sql.DB, workspace string) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("artifacts: no database")
	}
	if err := EnsureSchema(db); err != nil {
		return nil, err
	}
	return &Store{db: db, workspace: workspace}, nil
}

// Publish validates a model proposal and, only on success, persists the
// canonical artifact. Validation failure returns an error and persists
// nothing: unvalidated proposals never become artifacts.
func (s *Store) Publish(in Input) (*Artifact, error) {
	session := strings.TrimSpace(in.SessionKey)
	if session == "" {
		return nil, fmt.Errorf("artifacts: session is required")
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, fmt.Errorf("artifacts: title is required")
	}
	if len([]rune(title)) > maxTitleLen {
		return nil, fmt.Errorf("artifacts: title too long")
	}
	summary := boundString(in.Summary, maxSummaryLen)
	a := &Artifact{
		ID:          newID(),
		SessionKey:  session,
		Kind:        kind,
		Title:       title,
		Summary:     redact.Text(summary),
		EvidenceRef: strings.TrimSpace(in.EvidenceRef),
		State:       StateAvailable,
		CreatedAt:   time.Now().UTC(),
	}
	set := 0
	switch kind {
	case KindFile:
		rel, err := s.resolveFile(in.Path)
		if err != nil {
			return nil, err
		}
		a.Path = rel
		a.Actions = fileActions(rel)
		set++
	case KindText:
		text := strings.TrimSpace(in.Text)
		if text == "" {
			return nil, fmt.Errorf("artifacts: text is required")
		}
		a.Text = redact.Text(boundString(text, maxTextLen))
		set++
	case KindLink:
		u, err := url.ParseRequestURI(strings.TrimSpace(in.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("artifacts: link must be an http(s) URL")
		}
		a.URL = u.String()
		a.Actions = []Action{{ID: "open", Label: "Open", Kind: ActionOpen}}
		set++
	default:
		return nil, fmt.Errorf("artifacts: unknown kind %q", in.Kind)
	}
	if set != 1 {
		return nil, fmt.Errorf("artifacts: exactly one payload is required")
	}
	actionsJSON, _ := json.Marshal(a.Actions)
	if _, err := s.db.Exec(`INSERT INTO artifacts
		(id, session_key, kind, title, summary, path, text, url, state, reason, actions, evidence_request_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionKey, a.Kind, a.Title, a.Summary, a.Path, a.Text, a.URL,
		a.State, a.Reason, string(actionsJSON), a.EvidenceRef, a.CreatedAt); err != nil {
		return nil, fmt.Errorf("artifacts: persist: %w", err)
	}
	return a, nil
}

// Get returns one artifact, lazily revalidating file existence so a
// deleted or moved file surfaces as unavailable with a reason instead
// of a broken handoff.
func (s *Store) Get(id string) (*Artifact, error) {
	a, err := s.load("SELECT id, session_key, kind, title, summary, path, text, url, state, reason, actions, evidence_request_id, created_at FROM artifacts WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("artifacts: not found")
	}
	if a.Kind == KindFile && a.State == StateAvailable {
		if _, err := s.resolveFile(a.Path); err != nil {
			a.State = StateUnavailable
			a.Reason = "the file is no longer available"
			a.Actions = nil
		}
	}
	return a, nil
}

// List returns a session's artifacts newest first. Unknown sessions yield
// nothing: conversation isolation is by session key, the same unit the
// mobile contract uses for history and activity.
func (s *Store) List(sessionKey string, limit int) ([]Artifact, error) {
	if strings.TrimSpace(sessionKey) == "" {
		return nil, fmt.Errorf("artifacts: session is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, session_key, kind, title, summary, path, text, url, state, reason, actions, evidence_request_id, created_at
		FROM artifacts WHERE session_key = ? ORDER BY created_at DESC, rowid DESC LIMIT ?`, sessionKey, limit)
	if err != nil {
		return nil, fmt.Errorf("artifacts: list: %w", err)
	}
	defer rows.Close()
	out := []Artifact{}
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// resolveFile validates a workspace-relative file reference: clean,
// confined, present, a regular file, bounded, and outside the protected
// estate. It returns the clean relative path.
func (s *Store) resolveFile(p string) (string, error) {
	rel := filepath.ToSlash(filepath.Clean(strings.TrimSpace(p)))
	if rel == "" || rel == "." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", fmt.Errorf("artifacts: path escapes the workspace")
	}
	for _, prefix := range protectedPrefixes {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			return "", fmt.Errorf("artifacts: that file is internal and cannot be shared")
		}
	}
	lower := strings.ToLower(rel)
	if strings.HasSuffix(lower, ".db") || strings.HasSuffix(lower, ".db-wal") ||
		strings.HasSuffix(lower, ".db-shm") || strings.HasSuffix(lower, ".sqlite") {
		return "", fmt.Errorf("artifacts: database files cannot be shared")
	}
	absWs, err := filepath.Abs(s.workspace)
	if err != nil {
		return "", fmt.Errorf("artifacts: workspace unavailable")
	}
	abs := filepath.Join(absWs, filepath.FromSlash(rel))
	// Separator-boundary confinement: a sibling whose name merely starts
	// with the workspace path must not escape (same rule as the file tools).
	if abs != absWs && !strings.HasPrefix(abs, absWs+string(os.PathSeparator)) {
		return "", fmt.Errorf("artifacts: path escapes the workspace")
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("artifacts: file does not exist")
	}
	if fi.Size() > maxFileBytes {
		return "", fmt.Errorf("artifacts: file too large to share")
	}
	return rel, nil
}

// fileActions computes render hints from the validated path.
func fileActions(rel string) []Action {
	lower := strings.ToLower(rel)
	actions := []Action{{ID: "preview", Label: "Preview", Kind: ActionPreview}}
	switch {
	case strings.HasSuffix(lower, ".pdf"),
		strings.HasSuffix(lower, ".png"),
		strings.HasSuffix(lower, ".jpg"),
		strings.HasSuffix(lower, ".jpeg"),
		strings.HasSuffix(lower, ".gif"),
		strings.HasSuffix(lower, ".webp"),
		strings.HasSuffix(lower, ".txt"),
		strings.HasSuffix(lower, ".md"),
		strings.HasSuffix(lower, ".csv"),
		strings.HasSuffix(lower, ".json"):
		actions = append(actions, Action{ID: "open", Label: "Open", Kind: ActionOpen})
	}
	return append(actions, Action{ID: "download", Label: "Download", Kind: ActionDownload})
}

func (s *Store) load(query, arg string) (*Artifact, error) {
	rows, err := s.db.Query(query, arg)
	if err != nil {
		return nil, fmt.Errorf("artifacts: read: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	a, err := scanArtifact(rows)
	if err != nil {
		return nil, err
	}
	return a, rows.Err()
}

type artifactScanner interface {
	Scan(dest ...interface{}) error
}

func scanArtifact(rows artifactScanner) (*Artifact, error) {
	var a Artifact
	var actionsJSON string
	if err := rows.Scan(&a.ID, &a.SessionKey, &a.Kind, &a.Title, &a.Summary,
		&a.Path, &a.Text, &a.URL, &a.State, &a.Reason, &actionsJSON,
		&a.EvidenceRef, &a.CreatedAt); err != nil {
		return nil, fmt.Errorf("artifacts: scan: %w", err)
	}
	a.Actions = []Action{}
	if actionsJSON != "" {
		_ = json.Unmarshal([]byte(actionsJSON), &a.Actions)
	}
	return &a, nil
}

func boundString(s string, max int) string {
	runes := []rune(s)
	if len(runes) > max {
		runes = runes[:max]
	}
	return string(runes)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("art-%d", time.Now().UnixNano())
	}
	return "art-" + hex.EncodeToString(b[:])
}
