package doctor

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
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

	reg := tools.NewToolRegistry()
	reg.Register(tools.NewSessionSearchTool(database.DB))

	runner := New(database.DB, &testProvider{}, reg, t.TempDir())
	results := runner.RunAll(context.Background())
	if len(results) != 5 {
		t.Fatalf("expected 5 checks, got %d", len(results))
	}
	for _, check := range results {
		if check.Status == "error" {
			t.Fatalf("unexpected error status for check %s: %s", check.Name, check.Message)
		}
	}
}
