package agent

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/failurecorpus"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/turnlog"
	_ "modernc.org/sqlite"
)

type failingVerifier struct{}

func (f *failingVerifier) Name() string        { return "wtest" }
func (f *failingVerifier) Description() string { return "test" }
func (f *failingVerifier) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (f *failingVerifier) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.NewToolResult("ok")
}
func (f *failingVerifier) Verify(ctx context.Context, args map[string]interface{}) error {
	return fmt.Errorf("effect missing")
}

// A runtime verification failure must be captured locally for regression,
// with the turn's trajectory attached.
func TestRuntimeVerificationFailureCaptured(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: ws, Model: "mock-model", MaxTokens: 1024, MaxToolIterations: 5,
	}}}
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &simpleMockProvider{response: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Stop() })
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	al.SetGovernance(NewGovernance(st, nil, "g1", "agent-main"))

	al.tools.Register(&failingVerifier{})
	ctx := turnlog.WithTrajectoryID(context.Background(), "trj_vfail")
	res := al.tools.Execute(ctx, "wtest", map[string]interface{}{})
	if !res.IsError {
		t.Fatal("failed verification must surface as an error result")
	}

	if al.failureCorpus == nil {
		t.Fatal("failure corpus must be initialized")
	}
	recs, err := al.failureCorpus.List(10)
	if err != nil || len(recs) == 0 {
		t.Fatalf("expected a captured failure, got %v %v", recs, err)
	}
	got := recs[0]
	if got.Category != failurecorpus.CatVerification {
		t.Fatalf("category = %s, want verification", got.Category)
	}
	if got.TrajectoryID != "trj_vfail" {
		t.Fatalf("trajectory = %q, want trj_vfail", got.TrajectoryID)
	}
}
