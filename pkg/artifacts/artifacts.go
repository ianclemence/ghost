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
	"regexp"
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
	// What the owner pinned to the top of their shelf. Kept beside the
	// artifact, not in it: pinning is the owner's, the record is the Pod's.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS artifact_pins (
		artifact_id TEXT PRIMARY KEY,
		pinned_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("artifacts: pins: %w", err)
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

// ListAll returns the most recent artifacts across every conversation. The
// Desk uses it: the owner's work is one shelf, not one shelf per chat. It is
// a read that revalidates nothing (state is stored), so callers that render
// an artifact should still Get it before showing contents; ListAll is for the
// index.
func (s *Store) ListAll(limit int) ([]Artifact, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.listNewest(limit)
}

// listNewest is the newest limit artifacts in every conversation.
func (s *Store) listNewest(limit int) ([]Artifact, error) {
	rows, err := s.db.Query(`SELECT id, session_key, kind, title, summary, path, text, url, state, reason, actions, evidence_request_id, created_at
		FROM artifacts ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("artifacts: list all: %w", err)
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

// PruneDangling deletes file artifacts whose referenced file no longer exists
// in the workspace. Text and link artifacts have no file and are never pruned
// here. This bounds the table without ever discarding a live handoff.
func (s *Store) PruneDangling() (int, error) {
	rows, err := s.db.Query(`SELECT id, path FROM artifacts WHERE kind='file' AND path != ''`)
	if err != nil {
		return 0, fmt.Errorf("artifacts: prune scan: %w", err)
	}
	type ref struct{ id, path string }
	var dangling []ref
	for rows.Next() {
		var r ref
		if rows.Scan(&r.id, &r.path) == nil {
			full := filepath.Join(s.workspace, filepath.FromSlash(r.path))
			if _, err := os.Stat(full); os.IsNotExist(err) {
				dangling = append(dangling, r)
			}
		}
	}
	rows.Close()
	removed := 0
	for _, r := range dangling {
		if _, err := s.db.Exec(`DELETE FROM artifacts WHERE id=?`, r.id); err == nil {
			removed++
		}
	}
	return removed, nil
}

// ShelfItem is an artifact as the shelf shows it: whether it is pinned, and
// for a canvas or a document made in versions, how many there are (only the
// newest is listed).
type ShelfItem struct {
	Artifact
	Pinned   bool `json:"pinned"`
	Versions int  `json:"versions"`
}

// SetPinned pins or unpins an artifact. It reports false when there is no
// such artifact.
func (s *Store) SetPinned(id string, pinned bool) (bool, error) {
	if _, err := s.Get(id); err != nil {
		return false, nil
	}
	var err error
	if pinned {
		_, err = s.db.Exec(`INSERT OR IGNORE INTO artifact_pins (artifact_id, pinned_at) VALUES (?, ?)`, id, time.Now().UTC())
	} else {
		_, err = s.db.Exec(`DELETE FROM artifact_pins WHERE artifact_id = ?`, id)
	}
	return err == nil, err
}

// ShelfKinds are the shelf's filters.
const (
	ShelfPages      = "pages"     // canvases: things that run
	ShelfDocuments  = "documents" // files to read, print or send
	ShelfPictures   = "pictures"
	ShelfLinks      = "links"
	ShelfNotes      = "notes"      // written results
	ShelfMotion     = "motion"     // animated explainers and their videos
	ShelfDashboards = "dashboards" // live answers about the owner's data
)

// ShelfKindOf is the filter an artifact belongs to.
func ShelfKindOf(a Artifact) string {
	switch a.Kind {
	case KindLink:
		return ShelfLinks
	case KindText:
		return ShelfNotes
	}
	p := strings.ToLower(a.Path)
	switch {
	case strings.HasPrefix(p, "motion/") && (strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".mp4")):
		return ShelfMotion
	case strings.HasPrefix(p, "dashboards/") && strings.HasSuffix(p, ".json"):
		return ShelfDashboards
	case strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".htm"):
		return ShelfPages
	case strings.HasSuffix(p, ".png") || strings.HasSuffix(p, ".jpg") || strings.HasSuffix(p, ".jpeg") || strings.HasSuffix(p, ".webp") || strings.HasSuffix(p, ".gif"):
		return ShelfPictures
	}
	return ShelfDocuments
}

// Shelf lists everything Ghost has made, across every conversation, newest
// first with pinned things on top. Versions of the same canvas or document
// (same title, same kind of thing) are one entry: the newest, with the count.
// query matches the title or summary; kind narrows to one filter.
func (s *Store) Shelf(query, kind string, limit int) ([]ShelfItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	all, err := s.listNewest(2000)
	if err != nil {
		return nil, err
	}
	pinned := map[string]bool{}
	if rows, err := s.db.Query(`SELECT artifact_id FROM artifact_pins`); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				pinned[id] = true
			}
		}
		rows.Close()
	}
	q := strings.ToLower(strings.TrimSpace(query))
	seen := map[string]int{}
	var out []ShelfItem
	for _, a := range all {
		if a.State != StateAvailable && !pinned[a.ID] {
			continue
		}
		k := ShelfKindOf(a)
		if kind != "" && k != kind {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.Title+" "+a.Summary), q) {
			continue
		}
		group := shelfGroup(a, k)
		if group != "" {
			if i, ok := seen[group]; ok {
				out[i].Versions++
				if pinned[a.ID] {
					out[i].Pinned = true
				}
				continue
			}
			seen[group] = len(out)
		}
		out = append(out, ShelfItem{Artifact: a, Pinned: pinned[a.ID], Versions: 1})
	}
	// Pinned first; otherwise the order stays newest first.
	sorted := make([]ShelfItem, 0, len(out))
	for _, it := range out {
		if it.Pinned {
			sorted = append(sorted, it)
		}
	}
	for _, it := range out {
		if !it.Pinned {
			sorted = append(sorted, it)
		}
	}
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	return sorted, nil
}

// shelfGroup is what versions of one thing share (its kind and title), or ""
// for something that has no versions. A motion's video is not a version of
// the motion: it is listed on its own.
func shelfGroup(a Artifact, k string) string {
	switch {
	case k == ShelfPages,
		k == ShelfDocuments && strings.HasPrefix(a.Path, "documents/"),
		k == ShelfMotion && strings.HasSuffix(a.Path, ".json"),
		k == ShelfDashboards:
		return k + "|" + strings.ToLower(strings.TrimSpace(a.Title))
	}
	return ""
}

// madeDirs are where Ghost keeps what it made. Deleting one of those removes
// its file too; anything else (an upload, a file in the workspace) is only
// taken off the shelf and out of the conversation, and stays in Files.
var madeDirs = []string{"canvas/", "documents/", "motion/", "dashboards/"}

func madeByGhost(path string) bool {
	for _, d := range madeDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// Versions lists every version of the thing an artifact belongs to (itself
// alone when it has none), newest first.
func (s *Store) Versions(id string) ([]Artifact, error) {
	a, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	g := shelfGroup(*a, ShelfKindOf(*a))
	if g == "" {
		return []Artifact{*a}, nil
	}
	all, err := s.listNewest(2000)
	if err != nil {
		return nil, err
	}
	var out []Artifact
	for _, b := range all {
		if shelfGroup(b, ShelfKindOf(b)) == g {
			out = append(out, b)
		}
	}
	return out, nil
}

// Delete removes one artifact: it leaves the shelf and the conversation, and
// a file Ghost made goes with it (a document's source and Word copy, a
// motion's video). It reports how many artifacts were removed.
func (s *Store) Delete(id string) (int, error) {
	a, err := s.Get(id)
	if err != nil {
		return 0, err
	}
	if err := s.remove(*a); err != nil {
		return 0, err
	}
	return 1, nil
}

// DeleteAll removes the thing an artifact belongs to: every version, and
// what was kept for it across versions (a canvas's saved data).
func (s *Store) DeleteAll(id string) (int, error) {
	vs, err := s.Versions(id)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, v := range vs {
		if err := s.remove(v); err != nil {
			return n, err
		}
		n++
	}
	if len(vs) > 0 && strings.HasPrefix(vs[0].Path, "canvas/") {
		if m := canvasVersion.FindStringSubmatch(vs[0].Path); m != nil {
			_ = os.Remove(filepath.Join(s.workspace, "canvas", m[1]+".saved.json"))
		}
	}
	return n, nil
}

var canvasVersion = regexp.MustCompile(`^canvas/([a-z0-9-]+)-v\d+\.html$`)

func (s *Store) remove(a Artifact) error {
	if a.Kind == KindFile && madeByGhost(a.Path) {
		// Another artifact may point at the same file (a dashboard saved
		// again under its title): the file goes only with the last of them.
		var others int
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE path=? AND id!=?`, a.Path, a.ID).Scan(&others)
		if others == 0 {
			base := filepath.Join(s.workspace, filepath.FromSlash(a.Path))
			_ = os.Remove(base)
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			switch {
			case strings.HasPrefix(a.Path, "documents/"):
				_ = os.Remove(stem + ".md")
				_ = os.Remove(stem + ".docx")
			case strings.HasPrefix(a.Path, "motion/") && strings.HasSuffix(a.Path, ".json"):
				_ = os.Remove(stem + ".mp4")
				video := strings.TrimSuffix(a.Path, ".json") + ".mp4"
				_, _ = s.db.Exec(`DELETE FROM artifact_pins WHERE artifact_id IN (SELECT id FROM artifacts WHERE path=?)`, video)
				_, _ = s.db.Exec(`DELETE FROM artifacts WHERE path=?`, video)
			}
		}
	}
	if _, err := s.db.Exec(`DELETE FROM artifact_pins WHERE artifact_id=?`, a.ID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM artifacts WHERE id=?`, a.ID)
	return err
}
