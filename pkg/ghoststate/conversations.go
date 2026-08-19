package ghoststate

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/ianclemence/ghost/pkg/db"
	_ "modernc.org/sqlite"
)

// The portable conversations sub-format. The format marker lives in the
// archive at conversations/format.json and is the version contract for the
// conversation files: imports refuse an unknown version instead of guessing.
const (
	conversationsFormat          = "ghost-conversations"
	conversationsVersion         = 1
	conversationsDirLogical      = "conversations"
	conversationsFormatLogical   = "conversations/format.json"
	conversationsSessionsLogical = "conversations/sessions"
)

type conversationFormatFile struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
}

// conversationSessionLine is the first line of a session file.
type conversationSessionLine struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// conversationMessageLine is one message line of a session file. Seq is the
// deterministic 0-based position of the message within its session.
type conversationMessageLine struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Meta      json.RawMessage `json:"meta"`
	Archived  bool            `json:"archived"`
	CreatedAt string          `json:"created_at"`
	Seq       int             `json:"seq"`
}

// conversationFileName derives a deterministic, collision-resistant archive
// name from a session id: a hash prefix keeps unusual ids (e.g. the colon in
// "telegram:8557670047") from colliding after sanitization.
func conversationFileName(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	slug := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, sessionID)
	if slug == "" {
		slug = "session"
	}
	return hex.EncodeToString(sum[:8]) + "-" + slug + ".jsonl"
}

// stageConversationsFromDB exports the sessions and messages of a SQLite
// workspace database as versioned, deterministic conversations JSONL. The
// database itself is not portable state — it is a runtime index — so it is
// recorded as rebound rather than embedded as an opaque binary snapshot.
func stageConversationsFromDB(staging map[string]string, stagingDir string, m *Manifest, dbPath string) error {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=query_only(1)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer conn.Close()

	formatData, err := json.Marshal(conversationFormatFile{Format: conversationsFormat, Version: conversationsVersion})
	if err != nil {
		return err
	}
	if err := stageEntry(staging, stagingDir, m, conversationsFormatLogical, CategoryPortable, formatData, 0644); err != nil {
		return err
	}

	rows, err := conn.Query(`SELECT id, COALESCE(summary, ''), COALESCE(created_at, ''), COALESCE(updated_at, '') FROM sessions ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read sessions: %w", err)
	}
	type sessionRow struct {
		id, summary, createdAt, updatedAt string
	}
	var sessions []sessionRow
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.summary, &r.createdAt, &r.updatedAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate sessions: %w", err)
	}
	rows.Close()

	for _, s := range sessions {
		data, err := conversationSessionBytes(conn, s.id, s.summary, s.createdAt, s.updatedAt)
		if err != nil {
			return err
		}
		logical := path.Join(conversationsSessionsLogical, conversationFileName(s.id))
		if err := stageEntry(staging, stagingDir, m, logical, CategoryPortable, data, 0644); err != nil {
			return err
		}
	}

	m.Rebound = append(m.Rebound, "ghost.db (runtime index; rehydrated from conversations/)")
	return nil
}

// conversationSessionBytes renders one session as JSONL: a session header line
// followed by one line per message, in the same chronological order the
// runtime returns (created_at ascending, ties by insertion order). The explicit
// seq makes the ordering an invariant of the file, not of timestamp parsing.
func conversationSessionBytes(conn *sql.DB, sessionID, summary, createdAt, updatedAt string) ([]byte, error) {
	var buf bytes.Buffer
	header, err := json.Marshal(conversationSessionLine{
		Type:      "session",
		SessionID: sessionID,
		Summary:   summary,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	})
	if err != nil {
		return nil, err
	}
	buf.Write(header)
	buf.WriteByte('\n')

	rows, err := conn.Query(`SELECT id, role, content, meta, COALESCE(archived, 0), COALESCE(created_at, '') FROM messages WHERE session_id = ? ORDER BY created_at ASC, rowid ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("read messages for %s: %w", sessionID, err)
	}
	defer rows.Close()

	seq := 0
	for rows.Next() {
		var id, role, content string
		var meta []byte
		var archived int
		var msgCreatedAt string
		if err := rows.Scan(&id, &role, &content, &meta, &archived, &msgCreatedAt); err != nil {
			return nil, fmt.Errorf("scan message for %s: %w", sessionID, err)
		}
		if len(meta) == 0 {
			meta = []byte("null")
		} else if !json.Valid(meta) {
			meta, err = json.Marshal(string(meta))
			if err != nil {
				return nil, err
			}
		}
		line, err := json.Marshal(conversationMessageLine{
			Type:      "message",
			ID:        id,
			SessionID: sessionID,
			Role:      role,
			Content:   content,
			Meta:      meta,
			Archived:  archived != 0,
			CreatedAt: msgCreatedAt,
			Seq:       seq,
		})
		if err != nil {
			return nil, err
		}
		buf.Write(line)
		buf.WriteByte('\n')
		seq++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages for %s: %w", sessionID, err)
	}
	return buf.Bytes(), nil
}

// rehydrateConversations rebuilds a fresh ghost.db from the portable
// conversations JSONL in an archive. The database schema and FTS index come
// from the db layer; the message triggers repopulate the full-text index as
// rows are inserted, so the runtime works immediately on the new machine.
func rehydrateConversations(workspace string, files map[string][]byte) error {
	raw, ok := files[conversationsFormatLogical]
	if !ok {
		return fmt.Errorf("archive is missing %s", conversationsFormatLogical)
	}
	var ff conversationFormatFile
	if err := json.Unmarshal(raw, &ff); err != nil {
		return fmt.Errorf("parse %s: %w", conversationsFormatLogical, err)
	}
	if ff.Format != conversationsFormat {
		return fmt.Errorf("unrecognized conversations format %q", ff.Format)
	}
	if ff.Version != conversationsVersion {
		return fmt.Errorf("unsupported conversations version %d (this build understands %d)", ff.Version, conversationsVersion)
	}

	var sessionFiles []string
	for name := range files {
		if strings.HasPrefix(name, conversationsSessionsLogical+"/") {
			sessionFiles = append(sessionFiles, name)
		}
	}
	sort.Strings(sessionFiles)

	database, err := db.NewDB(workspace)
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	defer database.Close()

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages`); err != nil {
		tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions`); err != nil {
		tx.Rollback()
		return err
	}
	for _, name := range sessionFiles {
		if err := rehydrateSessionFile(tx, files[name]); err != nil {
			tx.Rollback()
			return fmt.Errorf("rehydrate %s: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func rehydrateSessionFile(tx *sql.Tx, data []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)

	var header conversationSessionLine
	lineNo := 0
	prevSeq := -1
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var typ struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &typ); err != nil {
			return fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch typ.Type {
		case "session":
			if lineNo != 0 {
				return fmt.Errorf("line %d: session header must be the first line", lineNo)
			}
			if err := json.Unmarshal(line, &header); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			if header.SessionID == "" {
				return fmt.Errorf("line %d: session header missing session_id", lineNo)
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO sessions (id, summary, created_at, updated_at) VALUES (?, ?, ?, ?)`,
				header.SessionID, header.Summary, header.CreatedAt, header.UpdatedAt); err != nil {
				return fmt.Errorf("insert session %s: %w", header.SessionID, err)
			}
		case "message":
			var msg conversationMessageLine
			if err := json.Unmarshal(line, &msg); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			if msg.ID == "" {
				return fmt.Errorf("line %d: message missing id", lineNo)
			}
			if msg.SessionID != "" && msg.SessionID != header.SessionID {
				return fmt.Errorf("line %d: message %s session_id %q does not match header %q", lineNo, msg.ID, msg.SessionID, header.SessionID)
			}
			if msg.Seq <= prevSeq {
				return fmt.Errorf("line %d: message %s seq %d out of order after %d", lineNo, msg.ID, msg.Seq, prevSeq)
			}
			prevSeq = msg.Seq
			var meta interface{}
			if len(msg.Meta) > 0 && string(msg.Meta) != "null" {
				meta = string(msg.Meta)
			}
			if _, err := tx.Exec(`INSERT OR REPLACE INTO messages (id, session_id, role, content, meta, archived, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				msg.ID, header.SessionID, msg.Role, msg.Content, meta, msg.Archived, msg.CreatedAt); err != nil {
				return fmt.Errorf("insert message %s: %w", msg.ID, err)
			}
		default:
			return fmt.Errorf("line %d: unknown record type %q", lineNo, typ.Type)
		}
		lineNo++
	}
	return sc.Err()
}
