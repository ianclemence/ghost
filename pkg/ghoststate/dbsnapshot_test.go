package ghoststate

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

func seedDurableRows(t *testing.T, ws string) {
	t.Helper()
	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()
	// The full production schema is assembled by each subsystem at
	// gateway startup (not by db.NewDB alone); initialize the same way
	// so snapshots are tested against the real DDL, not a copy of it.
	raw, err := sql.Open("sqlite", "file:"+filepath.Join(ws, "ghost.db"))
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	defer raw.Close()
	store := scheduled.NewStore(raw)
	if err := store.InitSchema(); err != nil {
		t.Fatalf("scheduled schema: %v", err)
	}
	if _, err := permissions.Open(raw, permissions.ModeAsk, 0); err != nil {
		t.Fatalf("permissions schema: %v", err)
	}
	if _, err := routines.New(raw, store); err != nil {
		t.Fatalf("routines schema: %v", err)
	}
	if _, err := cevents.Open(raw, t.TempDir()); err != nil {
		t.Fatalf("cevents schema: %v", err)
	}
	stmts := []string{
		`INSERT INTO scheduled_items (id, type, title, state, schedule_kind, schedule_expr, timezone, action_kind, action_content, source, created_by) VALUES ('routine-1', 'automation', 'Weekly brief', 'scheduled', 'cron', '0 9 * * MON', 'Asia/Bangkok', 'agent_turn', 'prepare my weekly brief', 'routine', 'owner')`,
		`INSERT INTO routine_meta (item_id, ghost_id, owner_id, instruction, allowed_capabilities, created_at, updated_at) VALUES ('routine-1', 'g-1', 'o-1', 'prepare my weekly brief', '["calendar.read"]', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO scheduled_items (id, type, title, state, schedule_kind, schedule_every, timezone, action_kind, action_content, source, created_by) VALUES ('auto-1', 'automation', 'Morning check', 'paused', 'every', 3600, 'UTC', 'message', 'good morning', 'user', 'owner')`,
		`INSERT INTO permission_grants (capability, action, scope, created_at) VALUES ('calendar', 'create', 'owner', '2026-01-01T00:00:00Z')`,
		`INSERT INTO permission_grants (capability, action, scope, created_at) VALUES ('email', 'send', 'contact:maria', '2026-01-01T00:00:00Z')`,
		`INSERT INTO permission_requests (id, request_id, agent_id, capability, action, risk, status, created_at, expires_at) VALUES ('pr-1', 'req-1', 'a', 'calendar', 'create', 'consequential', 'pending', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z')`,
		`INSERT INTO paired_devices (id, device_id, display_name, credential_hash, paired_at) VALUES ('pd-1', 'dev-1', 'Phone', 'hash-not-real', '2026-01-01T00:00:00Z')`,
		`INSERT INTO canonical_events (id, type, timestamp, status) VALUES ('ev-1', 'routine.completed', '2026-01-01T00:00:00Z', 'success')`,
		`INSERT INTO kv_store (key, value) VALUES ('k-1', '"v"')`,
	}
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
}

func exportImport(t *testing.T, ws, cfgDir string) string {
	t.Helper()
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	targetWS := t.TempDir()
	targetCfgDir := t.TempDir()
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(targetCfgDir, "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	return targetWS
}

func count(t *testing.T, d *db.DB, q string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("query %s: %v", q, err)
	}
	return n
}

// Routines, automations, standing grants, and evidence must survive a
// full export/import cycle with exact values; ephemeral approvals and
// device credentials must not.
func TestSnapshotRoundTripDurableState(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	seedDurableRows(t, ws)

	targetWS := exportImport(t, ws, cfgDir)

	d, err := db.NewDB(targetWS)
	if err != nil {
		t.Fatalf("open target db: %v", err)
	}
	defer d.Close()

	if n := count(t, d, `SELECT COUNT(*) FROM scheduled_items`); n != 2 {
		t.Fatalf("scheduled_items: got %d, want 2", n)
	}
	var title, tz, expr string
	if err := d.QueryRow(`SELECT title, timezone, schedule_expr FROM scheduled_items WHERE id='routine-1'`).Scan(&title, &tz, &expr); err != nil {
		t.Fatalf("read routine row: %v", err)
	}
	if title != "Weekly brief" || tz != "Asia/Bangkok" || expr != "0 9 * * MON" {
		t.Fatalf("routine row wrong: %q %q %q", title, tz, expr)
	}
	var instruction, allowed string
	if err := d.QueryRow(`SELECT instruction, allowed_capabilities FROM routine_meta WHERE item_id='routine-1'`).Scan(&instruction, &allowed); err != nil {
		t.Fatalf("read routine_meta: %v", err)
	}
	if instruction != "prepare my weekly brief" || allowed != `["calendar.read"]` {
		t.Fatalf("routine_meta wrong: %q %q", instruction, allowed)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM permission_grants`); n != 2 {
		t.Fatalf("permission_grants: got %d, want 2", n)
	}
	var scope string
	if err := d.QueryRow(`SELECT scope FROM permission_grants WHERE capability='email' AND action='send'`).Scan(&scope); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if scope != "contact:maria" {
		t.Fatalf("grant scope wrong: %q", scope)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM canonical_events`); n != 1 {
		t.Fatalf("canonical_events: got %d, want 1", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM kv_store`); n != 1 {
		t.Fatalf("kv_store: got %d, want 1", n)
	}

	// Ephemeral approvals and device credentials must NOT cross machines.
	if n := count(t, d, `SELECT COUNT(*) FROM permission_requests`); n != 0 {
		t.Fatalf("permission_requests: got %d, want 0 (ephemeral)", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM paired_devices`); n != 0 {
		t.Fatalf("paired_devices: got %d, want 0 (must re-pair)", n)
	}
}

// An unknown table with data fails the export instead of silently
// dropping it.
func TestExportFailsOnUnknownTable(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	if _, err := d.Exec(`CREATE TABLE future_feature (id TEXT PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO future_feature (id, payload) VALUES ('f-1', 'user data')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	d.Close()
	_, err = Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: filepath.Join(t.TempDir(), "ghost.ghost"),
		Passphrase:  testPassphrase,
	})
	if err == nil {
		t.Fatal("export with uncovered table must fail")
	}
	if got := err.Error(); !strings.Contains(got, "future_feature") {
		t.Fatalf("error must name the table, got: %v", err)
	}
}

// A snapshot whose columns drift from the schema fails the import.
func TestImportRejectsColumnDrift(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	seedDurableRows(t, ws)
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace: ws, ConfigPath: filepath.Join(cfgDir, "config.json"),
		Destination: archive, Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	// Column drift is enforced at rehydrate time against the live schema;
	// exercise the validator directly with a mismatched snapshot.
	d, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer d.Close()
	bad := `{"format":"ghost-db-snapshot","version":1,"table":"permission_grants","columns":["capability","action"],"rows":[["calendar","create"]]}`
	if err := rehydrateTableSnapshot(d, "db/permission_grants.json", []byte(bad)); err == nil {
		t.Fatal("column drift must fail import")
	}
	unknown := `{"format":"ghost-db-snapshot","version":1,"table":"evil","columns":[],"rows":[]}`
	if err := rehydrateTableSnapshot(d, "db/evil.json", []byte(unknown)); err == nil {
		t.Fatal("unknown table must fail import")
	}
}
