package doctor

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/tools"
)

type testProvider struct{}

func (p *testProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: "ok"}, nil
}
func (p *testProvider) GetDefaultModel() string { return "test-model" }
func (p *testProvider) SupportsTools() bool     { return true }
func (p *testProvider) GetContextWindow() int   { return 4096 }

func TestDoctorRunAll(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewDB failed: %v", err)
	}
	defer database.Close()

	// checkBinaries scans PATH: an empty PATH means no binaries found, so
	// the row omits itself and the count stays deterministic on any machine
	// (machines with shadowed ghost installs report an 11th row live).
	t.Setenv("PATH", t.TempDir())

	reg := tools.NewToolRegistry()
	reg.Register(tools.NewSessionSearchTool(database.DB))

	// Production migrates before doctor ever runs; the test mirrors that.
	if _, err := schema.MigrateToCurrent(database.DB); err != nil {
		t.Fatalf("MigrateToCurrent: %v", err)
	}

	runner := New(database.DB, &testProvider{}, reg, t.TempDir())
	results := runner.RunAll(context.Background())
	// 14 registered checks minus 4 empty-info omissions on a bare workspace:
	// unbound vault, no connected-service skills, no golden history, no
	// metered turns. Diagnostics length scales with problems, not inventory.
	if len(results) != 10 {
		t.Fatalf("expected 10 checks, got %d", len(results))
	}
	names := map[string]bool{}
	for _, check := range results {
		names[check.Name] = true
		if check.Status == "error" {
			t.Fatalf("unexpected error status for check %s: %s", check.Name, check.Message)
		}
	}
	for _, want := range []string{"database", "schema", "clock", "provider", "tool_registry", "browser_env", "skill_dependencies", "disk_pressure", "resources", "routines_failing"} {
		if !names[want] {
			t.Fatalf("missing core check %q", want)
		}
	}
	for _, gone := range []string{"intelligence", "vault", "connected_services", "last_golden", "eval_spend", "calendar_oauth", "gmail_oauth", "github_token"} {
		if names[gone] {
			t.Fatalf("empty/info row %q must be omitted, not rendered", gone)
		}
	}
}
