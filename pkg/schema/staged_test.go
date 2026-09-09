package schema

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ianclemence/ghost/pkg/migrations"
	_ "modernc.org/sqlite"
)

// A real install that ran on a v2 build (schema_migrations = {1,2}, v2
// tables/columns present, real rows) upgrades to v3 without losing state
// and gains only the v3 objects.
func TestV2ToV3StagedUpgrade(t *testing.T) {
	db, path := openRaw(t)

	// Simulate the earlier build: migrate to v2 only.
	v2, err := migrations.Migrate(db, registry()[:2])
	if err != nil {
		t.Fatalf("migrate to v2: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("v2 = %d", v2)
	}

	// A v2 device carries real durable-work state.
	seeds := []string{
		`INSERT INTO jobs (id, kind, status, created_at, updated_at, owner, context_id, generation, evidence, resume_state)
		 VALUES ('j-v2', 'browse', 'waiting_for_permission', 1, 1, 'ian', 'work', 'gen-9', 'need approval', 'cursor:2')`,
		`INSERT INTO computer_leases (id, resource_id, owner, task_id, state) VALUES ('l-v2', 'r-1', 'ian', 'j-v2', 'active')`,
		`INSERT INTO browser_sessions (id, profile, owner, context_id, task_id) VALUES ('s-v2', 'p', 'ian', 'work', 'j-v2')`,
		`INSERT INTO canonical_events (id, type, request_id, timestamp, status) VALUES ('e-v2', 'permission.requested', 'req-9', '2026-01-01T00:00:00Z', 'pending')`,
	}
	for _, s := range seeds {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("seed v2 state: %v\n%s", err, s)
		}
	}

	// Upgrade to head (v3).
	head, err := MigrateToCurrent(db)
	if err != nil {
		t.Fatalf("migrate to v3: %v", err)
	}
	if head != CurrentVersion {
		t.Fatalf("head = %d, want %d", head, CurrentVersion)
	}

	// v3 objects exist.
	tables := tablesOf(t, db)
	for _, tbl := range []string{"event_consumers", "event_claims"} {
		if !tables[tbl] {
			t.Fatalf("v3 table %s missing", tbl)
		}
	}

	// v2 data intact.
	var owner, gen, resume string
	if err := db.QueryRow(`SELECT owner, generation, resume_state FROM jobs WHERE id='j-v2'`).Scan(&owner, &gen, &resume); err != nil {
		t.Fatalf("job lost: %v", err)
	}
	if owner != "ian" || gen != "gen-9" || resume != "cursor:2" {
		t.Fatalf("job fields drifted: %q %q %q", owner, gen, resume)
	}
	var leaseState, sessOwner string
	if err := db.QueryRow(`SELECT state FROM computer_leases WHERE id='l-v2'`).Scan(&leaseState); err != nil || leaseState != "active" {
		t.Fatalf("lease lost: %q %v", leaseState, err)
	}
	if err := db.QueryRow(`SELECT owner FROM browser_sessions WHERE id='s-v2'`).Scan(&sessOwner); err != nil || sessOwner != "ian" {
		t.Fatalf("session lost: %q %v", sessOwner, err)
	}
	_ = path
}

// A database file that is not SQLite (or is corrupt) fails loudly at
// migration time instead of being silently treated as empty.
func TestMalformedDBFailsLoudly(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "ghost.db")
	if err := os.WriteFile(bad, []byte("this is not a sqlite database at all"), 0644); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+bad)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := MigrateToCurrent(db); err == nil {
		t.Fatal("corrupt database must fail migration loudly")
	}
}
