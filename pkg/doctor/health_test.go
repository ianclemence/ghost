package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/tools"
)

func healthFixture(t *testing.T) (*Doctor, string) {
	t.Helper()
	ws := t.TempDir()
	database, err := db.NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := schema.MigrateToCurrent(database.DB); err != nil {
		t.Fatal(err)
	}
	reg := tools.NewToolRegistry()
	d := New(database.DB, &testProvider{}, reg, ws)
	return d, ws
}

func TestHealthCheckCount(t *testing.T) {
	d, _ := healthFixture(t)
	results := d.RunAll(context.Background())
	// 14 registered minus 4 empty-info omissions on a bare workspace
	// (unbound vault, no service skills, no golden history, no spend).
	if len(results) != 10 {
		t.Fatalf("expected 10 checks, got %d", len(results))
	}
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
		if r.Name == "" || r.Status == "" {
			t.Fatalf("check must carry name and status: %+v", r)
		}
	}
	for _, want := range []string{"disk_pressure", "routines_failing"} {
		if !names[want] {
			t.Fatalf("missing health check %q", want)
		}
	}
	for _, gone := range []string{"vault", "last_golden", "eval_spend", "connected_services", "intelligence"} {
		if names[gone] {
			t.Fatalf("empty/info row %q must be omitted, not rendered", gone)
		}
	}
}

func TestHealthSeededRowsAppear(t *testing.T) {
	d, ws := healthFixture(t)
	state := filepath.Join(ws, "state")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	hist := `[{"at":"2026-09-15T00:00:00Z","model":"deepseek-flash","provider":"deepseek","suite_version":1,"summary":{"total":59,"passed":59,"failed":0,"hard_fails":0}}]`
	if err := os.WriteFile(filepath.Join(state, "golden-history.json"), []byte(hist), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.Exec(`INSERT INTO canonical_events (id, type, timestamp, status, payload) VALUES ('u1','usage.recorded','2026-09-15T00:00:00Z','recorded','{"cost_usd":0.01}')`); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range d.RunAll(context.Background()) {
		names[r.Name] = true
	}
	if !names["last_golden"] {
		t.Fatal("seeded golden history must surface a Golden row")
	}
	if !names["eval_spend"] {
		t.Fatal("metered turns must surface a Spend row")
	}
}

func TestRoutinesFailing(t *testing.T) {
	d, _ := healthFixture(t)
	if got := d.checkRoutinesFailing(context.Background()); got.Status != "ok" {
		t.Fatalf("clean store must be ok, got %+v", got)
	}
	if _, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS scheduled_items (id TEXT PRIMARY KEY, title TEXT DEFAULT '', state TEXT NOT NULL DEFAULT 'scheduled', last_error TEXT DEFAULT '', last_run_at DATETIME)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.Exec(`INSERT INTO scheduled_items (id, title, type, state, schedule_kind, action_kind, last_error, last_run_at) VALUES ('r1','Morning brief','reminder','failed','once','message','oauth expired','2026-09-15T08:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	got := d.checkRoutinesFailing(context.Background())
	if got.Status != "error" {
		t.Fatalf("failed routine must error, got %+v", got)
	}
	if !strings.Contains(got.Message, "Morning brief") || !strings.Contains(got.Message, "oauth expired") {
		t.Fatalf("message must name routine and error: %q", got.Message)
	}
}

func TestVaultUnboundAndAbsent(t *testing.T) {
	d, ws := healthFixture(t)
	if got := d.checkVault(context.Background()); got.Status != "info" {
		t.Fatalf("unbound vault must be info, got %+v", got)
	}
	cfgDir := t.TempDir()
	d.SetConfigPath(filepath.Join(cfgDir, "config.json"))
	if got := d.checkVault(context.Background()); got.Status != "ok" {
		t.Fatalf("absent secrets must be ok (first boot), got %+v", got)
	}
	// Unreadable garbage fails loudly, never silently.
	if err := os.WriteFile(filepath.Join(cfgDir, ".secrets.json"), []byte("{nope"), 0600); err != nil {
		t.Fatal(err)
	}
	_ = ws
	if got := d.checkVault(context.Background()); got.Status != "error" {
		t.Fatalf("corrupt secrets must error, got %+v", got)
	}
}

func TestLastGoldenAbsentAndScored(t *testing.T) {
	d, ws := healthFixture(t)
	if got := d.checkLastGolden(context.Background()); got.Status != "info" {
		t.Fatalf("absent history must be info, got %+v", got)
	}
	state := filepath.Join(ws, "state")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	hist := `[{"at":"2026-09-15T00:00:00Z","model":"deepseek-flash","provider":"deepseek","suite_version":1,"summary":{"suite_version":1,"model":"deepseek-flash","provider":"deepseek","total":59,"passed":59,"failed":0,"hard_fails":0}}]`
	if err := os.WriteFile(filepath.Join(state, "golden-history.json"), []byte(hist), 0600); err != nil {
		t.Fatal(err)
	}
	if got := d.checkLastGolden(context.Background()); got.Status != "ok" {
		t.Fatalf("clean score must be ok, got %+v", got)
	}
	bad := `[{"at":"2026-09-15T00:00:00Z","model":"m","provider":"p","suite_version":1,"summary":{"total":59,"passed":59,"failed":0,"hard_fails":2}}]`
	if err := os.WriteFile(filepath.Join(state, "golden-history.json"), []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if got := d.checkLastGolden(context.Background()); got.Status != "warning" {
		t.Fatalf("hard fails must warn even at full pass, got %+v", got)
	}
}

func TestEvalSpendEmpty(t *testing.T) {
	d, _ := healthFixture(t)
	if got := d.checkEvalSpend(context.Background()); got.Status != "info" {
		t.Fatalf("uninstrumented spend must be info, got %+v", got)
	}
}
