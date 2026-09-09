package tasks

import (
	"database/sql"
	"fmt"
)

// Schema DDL for the durable jobs table. This is the canonical current
// state: fresh callers (tests, provisioning) use EnsureSchema, and the
// migration chain converges existing databases to the identical shape
// (migration v1 created the base table, v2 added the scope/generation
// columns). A test in schema_test.go proves the two paths converge.
const jobsTableDDL = `CREATE TABLE IF NOT EXISTS jobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	status TEXT NOT NULL,
	progress REAL NOT NULL DEFAULT 0,
	checkpoints JSON,
	payload JSON,
	session_key TEXT,
	error TEXT,
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at DATETIME NOT NULL,
	started_at DATETIME,
	finished_at DATETIME,
	updated_at DATETIME NOT NULL,
	owner TEXT NOT NULL DEFAULT '',
	context_id TEXT NOT NULL DEFAULT '',
	generation TEXT NOT NULL DEFAULT '',
	evidence TEXT NOT NULL DEFAULT '',
	resume_state TEXT NOT NULL DEFAULT ''
)`

const jobsIndexesDDL = `CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_owner ON jobs(owner)`

// queryExecer is satisfied by both *sql.DB and *sql.Tx, so schema helpers
// work identically inside and outside migration transactions.
type queryExecer interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// EnsureSchema creates the current-state jobs table and indexes. Idempotent.
func EnsureSchema(db *sql.DB) error {
	return ensureSchemaOn(db)
}

func ensureSchemaOn(ex queryExecer) error {
	if _, err := ex.Exec(jobsTableDDL); err != nil {
		return fmt.Errorf("jobs schema: %w", err)
	}
	if _, err := ex.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`); err != nil {
		return fmt.Errorf("jobs status index: %w", err)
	}
	if _, err := ex.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at)`); err != nil {
		return fmt.Errorf("jobs created_at index: %w", err)
	}
	if _, err := ex.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_owner ON jobs(owner)`); err != nil {
		return fmt.Errorf("jobs owner index: %w", err)
	}
	return nil
}

// v2Columns are the columns migration v2 adds to pre-existing jobs tables.
var v2Columns = []struct{ name, ddl string }{
	{"owner", `ALTER TABLE jobs ADD COLUMN owner TEXT NOT NULL DEFAULT ''`},
	{"context_id", `ALTER TABLE jobs ADD COLUMN context_id TEXT NOT NULL DEFAULT ''`},
	{"generation", `ALTER TABLE jobs ADD COLUMN generation TEXT NOT NULL DEFAULT ''`},
	{"evidence", `ALTER TABLE jobs ADD COLUMN evidence TEXT NOT NULL DEFAULT ''`},
	{"resume_state", `ALTER TABLE jobs ADD COLUMN resume_state TEXT NOT NULL DEFAULT ''`},
}

// EnsureV2Columns brings a v1-era jobs table to current shape. Each ALTER
// is preceded by an explicit PRAGMA column check (never error-swallowing),
// so the whole function is idempotent and safe to retry.
func EnsureV2Columns(ex queryExecer) error {
	existing, err := tableColumns(ex, "jobs")
	if err != nil {
		return err
	}
	for _, col := range v2Columns {
		if existing[col.name] {
			continue
		}
		if _, err := ex.Exec(col.ddl); err != nil {
			return fmt.Errorf("add jobs.%s: %w", col.name, err)
		}
	}
	if _, err := ex.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_owner ON jobs(owner)`); err != nil {
		return fmt.Errorf("jobs owner index: %w", err)
	}
	return nil
}

func tableColumns(ex queryExecer, table string) (map[string]bool, error) {
	// Table name is internal, never user input; PRAGMA has no parameters.
	rows, err := ex.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	var cid int
	var name, ctype string
	var notnull int
	var dflt sql.NullString
	var pk int
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan pragma: %w", err)
		}
		out[name] = true
	}
	return out, rows.Err()
}
