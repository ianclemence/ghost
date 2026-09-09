package ghoststate

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/schema"
)

// An unmigrated database must fail the export loudly, naming the drifted
// table — never silently write column names as values (SQLite's
// double-quoted string fallback makes SELECT-the-missing-column succeed
// with garbage, so the check has to happen before the dump).
func TestExportFailsOnJobsShapeDrift(t *testing.T) {
	ws := t.TempDir()
	if _, err := EnsureIdentity(ws); err != nil {
		t.Fatalf("identity: %v", err)
	}
	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO jobs (id, kind, status, created_at, updated_at) VALUES ('j1', 'test', 'pending', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	_, err = Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(testConfigDir(t), "config.json"),
		Destination: filepath.Join(t.TempDir(), "ghost.ghost"),
		Passphrase:  testPassphrase,
	})
	if err == nil {
		t.Fatal("export of drifted jobs table must fail")
	}
	if !strings.Contains(err.Error(), `"jobs"`) || !strings.Contains(err.Error(), "drift") {
		t.Fatalf("error must name the drifted table, got: %v", err)
	}
}

// A migrated database exports and reimports with the v2/v3 state intact:
// job scope columns survive, consumer checkpoints survive, and the
// deliberately unrestored runtime tables are recorded as rebound.
func TestMigratedDBExportsDurableWork(t *testing.T) {
	ws := t.TempDir()
	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIdentity(ws); err != nil {
		t.Fatalf("identity: %v", err)
	}
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.MigrateToCurrent(raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	seeds := []string{
		`INSERT INTO jobs (id, kind, status, created_at, updated_at, owner, context_id, generation, evidence, resume_state) VALUES ('j1', 'test', 'waiting_for_permission', 1, 1, 'ian', 'personal', 'gen-1', 'need camera', 'cursor:step2')`,
		`INSERT INTO computer_leases (id, resource_id, owner, task_id, state) VALUES ('l1', 'r1', 'ian', 'j1', 'active')`,
		`INSERT INTO browser_sessions (id, profile, owner, context_id, task_id) VALUES ('s1', 'p', 'ian', 'personal', 'j1')`,
		`INSERT INTO event_consumers (consumer, last_seq, updated_at) VALUES ('c1', 41, 'x')`,
		`INSERT INTO event_claims (event_id, consumer, claimed_at) VALUES ('e1', 'c1', 'x')`,
		`INSERT INTO permission_requests (id, request_id, agent_id, capability, action, risk, status, created_at, expires_at) VALUES ('pr1', 'req-1', 'a', 'browser', 'browser_click', 'consequential', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z')`,
	}
	for _, s := range seeds {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
	raw.Close()
	d.Close()

	cfgDir := testConfigDir(t)
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	m, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("export migrated db: %v", err)
	}
	rebound := strings.Join(m.Rebound, "\n")
	for _, want := range []string{"computer_leases", "browser_sessions"} {
		if !strings.Contains(rebound, want) {
			t.Fatalf("manifest must record %s as rebound: %v", want, m.Rebound)
		}
	}

	targetWS := t.TempDir()
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("import: %v", err)
	}
	d2, err := db.NewDB(targetWS)
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var owner, ctx, gen, ev, resume, status string
	if err := d2.QueryRow(`SELECT status, owner, context_id, generation, evidence, resume_state FROM jobs WHERE id='j1'`).Scan(&status, &owner, &ctx, &gen, &ev, &resume); err != nil {
		t.Fatalf("read restored job: %v", err)
	}
	if status != "waiting_for_permission" || resume != "cursor:step2" {
		t.Fatalf("restored job lost live status/resume: %q %q", status, resume)
	}
	if owner != "ian" || ctx != "personal" || gen != "gen-1" || ev != "need camera" {
		t.Fatalf("job scope lost: %q %q %q %q", owner, ctx, gen, ev)
	}
	// A pending approval is executable live state: it must NOT come back.
	if n := count(t, d2, `SELECT COUNT(*) FROM permission_requests`); n != 0 {
		t.Fatalf("permission_requests: got %d, want 0 (approvals never restore)", n)
	}
	var seq int64
	if err := d2.QueryRow(`SELECT last_seq FROM event_consumers WHERE consumer='c1'`).Scan(&seq); err != nil || seq != 41 {
		t.Fatalf("consumer checkpoint lost: %d %v", seq, err)
	}
	if n := count(t, d2, `SELECT COUNT(*) FROM event_claims`); n != 1 {
		t.Fatalf("event_claims: got %d, want 1", n)
	}
	// Runtime holds must NOT cross machines: a restored lease for a dead
	// task would resurrect authority it no longer owns.
	if n := count(t, d2, `SELECT COUNT(*) FROM computer_leases`); n != 0 {
		t.Fatalf("computer_leases: got %d, want 0", n)
	}
	if n := count(t, d2, `SELECT COUNT(*) FROM browser_sessions`); n != 0 {
		t.Fatalf("browser_sessions: got %d, want 0", n)
	}
}
