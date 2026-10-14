package workflow

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// RunRecord is one appended ledger row: the governance-by-construction
// trail (which model, on what inputs, under which workflow version).
type RunRecord struct {
	RunID        string    `json:"run_id"`
	WorkflowName string    `json:"workflow_name"`
	WorkflowVer  string    `json:"workflow_version"`
	Digest       string    `json:"digest"`
	NodeID       string    `json:"node_id"`
	Decision     string    `json:"decision,omitempty"`
	Confidence   float64   `json:"confidence,omitempty"`
	Escalated    bool      `json:"escalated,omitempty"`
	Reason       string    `json:"reason,omitempty"`
	InputHash    string    `json:"input_hash"`
	OutputHash   string    `json:"output_hash"`
	CostUSD      float64   `json:"cost_usd,omitempty"`
	LatencyMs    int64     `json:"latency_ms,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Ledger is an append-only SQLite trail. No UPDATE/DELETE API exists on
// purpose: history is the product.
type Ledger struct{ db *sql.DB }

// OpenLedger creates the ledger DB (WAL, busy timeout) and schema.
func OpenLedger(path string) (*Ledger, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	schema := `CREATE TABLE IF NOT EXISTS workflow_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id TEXT NOT NULL, workflow_name TEXT NOT NULL,
		workflow_version TEXT NOT NULL, digest TEXT NOT NULL,
		node_id TEXT NOT NULL, decision TEXT, confidence REAL,
		escalated INTEGER, reason TEXT,
		input_hash TEXT, output_hash TEXT,
		cost_usd REAL, latency_ms INTEGER, created_at TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_runs_run ON workflow_runs(run_id);
	CREATE INDEX IF NOT EXISTS idx_runs_wf ON workflow_runs(workflow_name, workflow_version);`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Ledger{db: db}, nil
}

// Close releases the DB.
func (l *Ledger) Close() error { return l.db.Close() }

// HashState redacts then hashes a state map for the trail. Secrets never
// reach the ledger: keys shaped like credentials are replaced before hash.
func HashState(state map[string]interface{}) string {
	scrubbed := map[string]interface{}{}
	for k, v := range state {
		if isSecretKey(k) {
			scrubbed[k] = "«redacted»"
			continue
		}
		scrubbed[k] = v
	}
	raw, _ := json.Marshal(scrubbed)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}

func isSecretKey(k string) bool {
	l := len(k)
	_ = l
	switch k {
	case "token", "secret", "password", "apikey", "api_key", "authorization", "private_key":
		return true
	}
	return false
}

// Append writes one outcome row. Input/output hashes are computed by the
// caller (HashState before/after) so the ledger never sees raw payloads.
func (l *Ledger) Append(rec RunRecord) error {
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	rec.CreatedAt = rec.CreatedAt.UTC()
	esc := 0
	if rec.Escalated {
		esc = 1
	}
	_, err := l.db.Exec(`INSERT INTO workflow_runs
		(run_id, workflow_name, workflow_version, digest, node_id, decision, confidence, escalated, reason, input_hash, output_hash, cost_usd, latency_ms, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.RunID, rec.WorkflowName, rec.WorkflowVer, rec.Digest, rec.NodeID,
		rec.Decision, rec.Confidence, esc, rec.Reason,
		rec.InputHash, rec.OutputHash, rec.CostUSD, rec.LatencyMs,
		rec.CreatedAt.Format(time.RFC3339))
	return err
}

// EscalationRate reports escalated/total rows for a workflow version in a
// window. The steady-state rate is measured, never asserted.
func (l *Ledger) EscalationRate(workflow, version string, since time.Time) (esc, total int, err error) {
	row := l.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(escalated),0) FROM workflow_runs
		WHERE workflow_name=? AND workflow_version=? AND created_at>=?`,
		workflow, version, since.UTC().Format(time.RFC3339))
	if err := row.Scan(&total, &esc); err != nil {
		return 0, 0, fmt.Errorf("ledger: rate: %w", err)
	}
	return esc, total, nil
}
