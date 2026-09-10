package ghoststate

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/schema"
	_ "modernc.org/sqlite"
)

// Database table snapshots close the gap between "the database is a runtime
// index" and the durable policy that actually lives in SQLite: routines,
// scheduled automations, standing permission grants, and execution evidence
// have no file-backed form, so without snapshots an export would silently
// drop them. Conversations keep their versioned JSONL path; everything
// below travels as explicit, validated row snapshots.
//
// Deliberately NOT snapshotted:
//   - sessions, messages            → versioned conversations/*.jsonl
//   - messages_fts*                 → FTS index rebuilt by insert triggers
//   - permission_requests           → 15-minute ephemeral approvals
//   - paired_devices, pending_pairings → must rebind on the new machine
//   - sqlite_sequence               → autoincrement bookkeeping
//   - ghost.db-wal, ghost.db-shm   → disposable runtime files

const (
	dbSnapshotsDirLogical = "db"
	dbSnapshotFormat      = "ghost-db-snapshot"
	dbSnapshotVersion     = 1
)

// snapshotTable describes one snapshotted table: explicit columns in
// SELECT order plus a deterministic row order. Column lists are explicit
// (never SELECT *) so schema drift fails loudly instead of shifting data.
type snapshotTable struct {
	Name    string
	Columns []string
	OrderBy string
}

var snapshotTables = []snapshotTable{
	{Name: "artifacts", Columns: []string{
		"id", "session_key", "kind", "title", "summary",
		"path", "text", "url", "state", "reason", "actions",
		"evidence_request_id", "created_at",
	}, OrderBy: "created_at, rowid"},
	{Name: "scheduled_items", Columns: []string{
		"id", "type", "title", "description", "state",
		"schedule_kind", "schedule_at", "schedule_every", "schedule_expr",
		"timezone",
		"action_kind", "action_content", "action_command", "action_deliver", "action_skills",
		"channel", "chat_id", "delivery_mode",
		"source", "created_by", "created_at", "updated_at",
		"next_run_at", "last_run_at", "run_count", "delete_after_run",
		"retry_count", "max_retries", "last_error",
		"parent_id", "occurrence_id",
	}, OrderBy: "id"},
	{Name: "routine_meta", Columns: []string{
		"item_id", "ghost_id", "owner_id",
		"instruction", "allowed_capabilities",
		"created_at", "updated_at",
	}, OrderBy: "item_id"},
	{Name: "permission_grants", Columns: []string{
		"capability", "action", "scope", "created_at",
	}, OrderBy: "capability, action, scope"},
	{Name: "execution_history", Columns: []string{
		"id", "item_id", "execution_id",
		"scheduled_at", "started_at", "completed_at",
		"status", "error", "channel", "delivered_at", "delivery_status",
	}, OrderBy: "scheduled_at, execution_id"},
	{Name: "canonical_events", Columns: []string{
		"seq", "id", "type", "request_id", "session_id",
		"conversation_id", "ghost_id", "agent_id", "routine_id",
		"timestamp", "visibility", "status", "payload",
		"trajectory_id",
	}, OrderBy: "seq"},
	{Name: "memory_chunks", Columns: []string{
		"id", "content", "embedding", "created_at", "source",
	}, OrderBy: "id"},
	{Name: "kv_store", Columns: []string{
		"key", "value", "updated_at",
	}, OrderBy: "key"},
	{Name: "tool_usage", Columns: []string{
		"name", "state", "use_count", "last_used_at", "created_at", "pinned",
	}, OrderBy: "name"},
	{Name: "jobs", Columns: []string{
		"id", "kind", "status", "progress", "checkpoints", "payload",
		"session_key", "error", "attempts", "created_at", "started_at",
		"finished_at", "updated_at",
		"owner", "context_id", "generation", "evidence", "resume_state",
		"trajectory_id",
	}, OrderBy: "id"},
	{Name: "event_consumers", Columns: []string{
		"consumer", "last_seq", "updated_at",
	}, OrderBy: "consumer"},
	{Name: "event_claims", Columns: []string{
		"event_id", "consumer", "claimed_at",
	}, OrderBy: "event_id, consumer"},
}

// tableSnapshotFile is the on-disk form of one table snapshot.
type tableSnapshotFile struct {
	Format  string          `json:"format"`
	Version int             `json:"version"`
	Table   string          `json:"table"`
	Columns []string        `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

func snapshotLogicalPath(table string) string {
	return dbSnapshotsDirLogical + "/" + table + ".json"
}

// listTables returns user table names in deterministic order.
func listTables(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	return out, nil
}

func presentSet(present []string, name string) bool {
	for _, n := range present {
		if n == name {
			return true
		}
	}
	return false
}

// Tables covered by the versioned conversations path, never snapshotted.
var conversationTables = map[string]bool{"sessions": true, "messages": true}

// documentedNonsnapshotTables are live tables deliberately excluded from
// snapshots, recorded in the manifest so the exclusion is explicit rather
// than silent. Reasons: ephemeral TTL state, or credentials that must
// rebind on the new machine.
var documentedNonsnapshotTables = map[string]string{
	"permission_requests": "ephemeral approvals (15-minute TTL)",
	"paired_devices":      "device credentials (re-pair after restore)",
	"pending_pairings":    "ephemeral pairing handshakes",
	// Durable-work runtime state (migration v2/v3): never restored.
	"computer_leases":  "lease holds die with the tasks that held them; every boot expires survivors via RecoverStale, so restoring old holds could only resurrect authority for dead tasks",
	"browser_sessions": "session rows point at profile dirs and expire by TTL; sessions re-mint on demand after restore",
}

// stageTableSnapshots dumps every whitelisted table from the live database.
// Tables are classified closed-world: anything present that is neither
// snapshotted, conversation-backed, an FTS/SQLite internal, nor documented
// above fails the export, so a future schema addition can never silently
// disappear from backups. Whitelisted tables absent from this database
// (older or partial schemas) are skipped: there are no rows to lose, and
// the closed-world check above is what guards against silent loss.
func stageTableSnapshots(staging map[string]string, stagingDir string, m *Manifest, dbPath string) error {
	conn, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open database for snapshot: %w", err)
	}
	defer conn.Close()

	present, err := listTables(conn)
	if err != nil {
		return err
	}
	whitelisted := map[string]bool{}
	for _, t := range snapshotTables {
		whitelisted[t.Name] = true
	}
	for _, name := range present {
		switch {
		case whitelisted[name], conversationTables[name]:
		case strings.HasPrefix(name, "messages_fts"), strings.HasPrefix(name, "sqlite_"):
		case name == "schema_migrations":
			// Migration bookkeeping is recomputed by MigrateToCurrent on
			// import; carrying a version number across machines would be
			// stale by design.
		case documentedNonsnapshotTables[name] != "":
			m.Rebound = append(m.Rebound, name+" ("+documentedNonsnapshotTables[name]+")")
		default:
			return fmt.Errorf("database table %q is not covered by Ghost State export; refusing to silently drop it", name)
		}
	}

	for _, t := range snapshotTables {
		if !presentSet(present, t.Name) {
			continue
		}
		if err := assertTableShape(conn, t); err != nil {
			return err
		}
		rows, err := dumpTable(conn, t)
		if err != nil {
			return err
		}
		data, err := json.Marshal(tableSnapshotFile{
			Format:  dbSnapshotFormat,
			Version: dbSnapshotVersion,
			Table:   t.Name,
			Columns: t.Columns,
			Rows:    rows,
		})
		if err != nil {
			return fmt.Errorf("marshal %s snapshot: %w", t.Name, err)
		}
		if err := stageEntry(staging, stagingDir, m, snapshotLogicalPath(t.Name), CategoryPortable, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

// assertTableShape requires the live table's columns to exactly match the
// snapshot whitelist, in order. Without this, SQLite's double-quoted
// string fallback turns a missing column into a string literal: the
// export would succeed while writing the column NAME as every row's
// value — silent corruption instead of a loud failure. Schema drift must
// fail here, at export time, naming the table.
func assertTableShape(conn *sql.DB, t snapshotTable) error {
	// Table names are internal whitelist constants, never user input;
	// PRAGMA has no parameter binding so the name is interpolated after a
	// quote check.
	if strings.Contains(t.Name, "'") {
		return fmt.Errorf("inspect %s: unsafe table name", t.Name)
	}
	rows, err := conn.Query(`SELECT name FROM pragma_table_info('` + t.Name + `')`)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", t.Name, err)
	}
	defer rows.Close()
	var have []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return fmt.Errorf("inspect %s: %w", t.Name, err)
		}
		have = append(have, n)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("inspect %s: %w", t.Name, err)
	}
	if len(have) != len(t.Columns) {
		return fmt.Errorf("table %q shape drift: has %d columns %v, snapshot wants %d %v; migrate the database before export",
			t.Name, len(have), have, len(t.Columns), t.Columns)
	}
	for i := range have {
		if have[i] != t.Columns[i] {
			return fmt.Errorf("table %q shape drift at position %d: has %q, snapshot wants %q; migrate the database before export",
				t.Name, i, have[i], t.Columns[i])
		}
	}
	return nil
}

func dumpTable(conn *sql.DB, t snapshotTable) ([][]interface{}, error) {
	q := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s",
		strings.Join(quoteColumns(t.Columns), ", "),
		quoteIdent(t.Name), t.OrderBy)
	rows, err := conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("dump %s: %w", t.Name, err)
	}
	defer rows.Close()
	out := [][]interface{}{}
	for rows.Next() {
		vals := make([]interface{}, len(t.Columns))
		ptrs := make([]interface{}, len(t.Columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan %s: %w", t.Name, err)
		}
		for i, v := range vals {
			nv, err := normalizeSnapshotValue(t.Name, v)
			if err != nil {
				return nil, err
			}
			vals[i] = nv
		}
		out = append(out, vals)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", t.Name, err)
	}
	return out, nil
}

// normalizeSnapshotValue converts driver values to JSON-stable forms.
// All snapshot columns are TEXT/INTEGER/REAL; raw []byte (a BLOB the
// schema does not declare) fails the export rather than corrupt data.
func normalizeSnapshotValue(table string, v interface{}) (interface{}, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case int64, float64, string, bool:
		return x, nil
	case time.Time:
		// The driver parses DATETIME columns into time.Time; canonicalize
		// to RFC3339 UTC so snapshots are deterministic text.
		return x.UTC().Format(time.RFC3339), nil
	case []byte:
		s := string(x)
		for i := 0; i < len(s); i++ {
			if s[i] < 0x20 && s[i] != '\n' && s[i] != '\r' && s[i] != '\t' {
				return nil, fmt.Errorf("non-text blob in %s snapshot column", table)
			}
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unexpected driver type %T in %s snapshot column", v, table)
	}
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteColumns(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = quoteIdent(c)
	}
	return out
}

// rehydrateTableSnapshots restores whitelisted tables into a fresh database
// (schema created by db.NewDB). Each table is replaced wholesale inside one
// transaction per table; any validation failure aborts that table before a
// single row is written, and any write failure rolls it back.
func rehydrateTableSnapshots(workspace string, files map[string][]byte) error {
	names := []string{}
	for name := range files {
		if strings.HasPrefix(name, dbSnapshotsDirLogical+"/") && strings.HasSuffix(name, ".json") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}

	database, err := db.NewDB(workspace)
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	defer database.Close()
	// The base schema from db.NewDB covers only core tables. Subsystem
	// tables (schedules, routines, permissions, events, tool usage) are
	// created by their owners at gateway startup; import must build the
	// same schema before snapshots can land, using the owners'
	// idempotent initializers rather than a copied DDL.
	if err := ensureSnapshotSchema(database.DB, workspace); err != nil {
		return err
	}

	// Apply in whitelist order (parents before children: scheduled_items
	// precedes execution_history) regardless of archive order.
	ordered := []string{}
	for _, t := range snapshotTables {
		p := snapshotLogicalPath(t.Name)
		for _, n := range names {
			if n == p {
				ordered = append(ordered, n)
			}
		}
	}
	for _, n := range names {
		known := false
		for _, p := range ordered {
			if p == n {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("archive contains unknown table snapshot %q", n)
		}
	}
	for _, n := range ordered {
		if err := rehydrateTableSnapshot(database, n, files[n]); err != nil {
			return err
		}
	}
	return nil
}

func rehydrateTableSnapshot(database *db.DB, name string, data []byte) error {
	var sf tableSnapshotFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return fmt.Errorf("parse %s: %w", name, err)
	}
	if sf.Format != dbSnapshotFormat {
		return fmt.Errorf("unrecognized snapshot format %q in %s", sf.Format, name)
	}
	if sf.Version != dbSnapshotVersion {
		return fmt.Errorf("unsupported snapshot version %d in %s (this build understands %d)", sf.Version, name, dbSnapshotVersion)
	}
	var want *snapshotTable
	for i := range snapshotTables {
		if snapshotTables[i].Name == sf.Table {
			want = &snapshotTables[i]
		}
	}
	if want == nil {
		return fmt.Errorf("snapshot for unknown table %q in %s", sf.Table, name)
	}
	if len(sf.Columns) != len(want.Columns) {
		// Backward tolerance: archives written before a trailing column
		// existed (e.g. pre-trajectory canonical_events) restore with NULLs
		// for the missing tail instead of refusing the whole archive.
		// Anything else (reordered, renamed, or removed columns) still fails
		// closed — silent shape drift is how canonical data gets lost.
		if len(sf.Columns) >= len(want.Columns) {
			return fmt.Errorf("column count mismatch for %s: archive has %d, schema has %d", sf.Table, len(sf.Columns), len(want.Columns))
		}
		for i := range sf.Columns {
			if sf.Columns[i] != want.Columns[i] {
				return fmt.Errorf("column mismatch for %s at position %d: archive has %q, schema has %q", sf.Table, i, sf.Columns[i], want.Columns[i])
			}
		}
		return rehydrateWithNullTail(database, want, sf)
	}
	for i := range sf.Columns {
		if sf.Columns[i] != want.Columns[i] {
			return fmt.Errorf("column mismatch for %s at position %d: archive has %q, schema has %q", sf.Table, i, sf.Columns[i], want.Columns[i])
		}
	}

	tx, err := database.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM " + quoteIdent(want.Name)); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear %s: %w", want.Name, err)
	}
	placeholders := make([]string, len(want.Columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(want.Name), strings.Join(quoteColumns(want.Columns), ", "), strings.Join(placeholders, ", ")))
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare %s: %w", want.Name, err)
	}
	defer stmt.Close()
	for ri, row := range sf.Rows {
		if len(row) != len(want.Columns) {
			tx.Rollback()
			return fmt.Errorf("row %d of %s has %d values, want %d", ri, want.Name, len(row), len(want.Columns))
		}
		if _, err := stmt.Exec(row...); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert %s row %d: %w", want.Name, ri, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", want.Name, err)
	}
	return nil
}

// rehydrateWithNullTail restores an archive whose columns are a strict
// prefix of the current schema (old backup, new code): present values are
// inserted, missing trailing columns become NULL. The caller has already
// verified the prefix matches positionally.
func rehydrateWithNullTail(database *db.DB, want *snapshotTable, sf tableSnapshotFile) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM " + quoteIdent(want.Name)); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear %s: %w", want.Name, err)
	}
	placeholders := make([]string, len(want.Columns))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(want.Name), strings.Join(quoteColumns(want.Columns), ", "), strings.Join(placeholders, ", ")))
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare %s: %w", want.Name, err)
	}
	defer stmt.Close()
	for ri, row := range sf.Rows {
		if len(row) != len(sf.Columns) {
			tx.Rollback()
			return fmt.Errorf("row %d of %s has %d values, want %d", ri, want.Name, len(row), len(sf.Columns))
		}
		padded := make([]interface{}, len(want.Columns))
		copy(padded, row)
		if _, err := stmt.Exec(padded...); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert %s row %d: %w", want.Name, ri, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", want.Name, err)
	}
	return nil
}

// snapshotDBPath locates the live database file for a workspace.
func snapshotDBPath(workspace string) string {
	return filepath.Join(workspace, "ghost.db")
}

// ensureSnapshotSchema builds the subsystem tables a snapshot restore
// needs through the canonical migration path, so imports and gateway
// startup can never disagree about the schema.
func ensureSnapshotSchema(raw *sql.DB, workspace string) error {
	if _, err := schema.MigrateToCurrent(raw); err != nil {
		return err
	}
	return nil
}
