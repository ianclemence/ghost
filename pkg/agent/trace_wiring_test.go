package agent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/turnlog"
	_ "modernc.org/sqlite"
)

// toolThenDoneProvider issues one list_dir call, then a final answer.
type toolThenDoneProvider struct{ called bool }

func (m *toolThenDoneProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	if !m.called {
		m.called = true
		return &providers.LLMResponse{
			Content: "",
			ToolCalls: []providers.ToolCall{{
				ID:   "call-1",
				Name: "list_dir",
				Arguments: map[string]interface{}{
					"path": ".",
				},
			}},
		}, nil
	}
	return &providers.LLMResponse{Content: "done", ToolCalls: nil}, nil
}

func (m *toolThenDoneProvider) GetDefaultModel() string { return "mock-model" }

// A full turn through processMessage must land its trace on ONE trajectory:
// agent.started → tool.completed → agent.completed, all carrying the turn's
// trajectory ID. This is the live-wiring proof for the emission path (no
// model needed).
func TestProcessMessageTraceLandsOnTrajectory(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         ws,
				Model:             "mock-model",
				MaxTokens:         1024,
				MaxToolIterations: 5,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al, err := NewAgentLoop(cfg, msgBus, &toolThenDoneProvider{})
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

	ctx, cancel := context.WithTimeout(
		turnlog.WithTrajectoryID(context.Background(), "trj_e2e1"), 60*time.Second)
	defer cancel()
	msg := bus.InboundMessage{
		Channel: "web", SenderID: "t", ChatID: "chat",
		Content:    "list the workspace",
		SessionKey: "sess-e2e",
		Metadata:   map[string]string{"request_id": "req-e2e-1"},
	}
	if _, err := al.processMessage(ctx, msg, nil, nil); err != nil {
		t.Fatalf("processMessage: %v", err)
	}

	got := st.ByTrajectory("trj_e2e1")
	if len(got) == 0 {
		t.Fatal("no events landed on the trajectory")
	}
	types := map[cevents.Type]bool{}
	for _, e := range got {
		types[e.Type] = true
		if e.TrajectoryID != "trj_e2e1" {
			t.Fatalf("event %s missing trajectory", e.Type)
		}
	}
	for _, want := range []cevents.Type{
		cevents.AgentStarted, cevents.ToolCompleted, cevents.AgentCompleted,
	} {
		if !types[want] {
			t.Fatalf("trace missing %s (have %v)", want, types)
		}
	}
}
