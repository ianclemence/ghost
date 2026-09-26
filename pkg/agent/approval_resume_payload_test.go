package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/tools"
	_ "modernc.org/sqlite"
)

// payloadTool stands in for an approved pull of a web page: its result is
// a page-sized raw payload, exactly what a curl through exec returns.
type payloadTool struct{ output string }

func (p *payloadTool) Name() string        { return "probe_payload" }
func (p *payloadTool) Description() string { return "test payload tool" }
func (p *payloadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (p *payloadTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return &tools.ToolResult{ForLLM: p.output, ForUser: p.output}
}

func short(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}

// An approved execution's output is evidence for the model, never Ghost's
// reply. The resume path used to return the tool's raw bytes as the turn,
// so an owner who approved a read of a web page got a wall of HTML and
// JSON-LD streamed and stored as something Ghost said — and the rundown
// the model had promised never arrived.
func TestApprovalResumePayloadIsNotTheReply(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: ws, Model: "mock-model", MaxTokens: 1024, MaxToolIterations: 5,
	}}}
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &simpleMockProvider{response: "Here is the actual rundown."})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Stop() })

	raw, err := sql.Open("sqlite", "file:"+ws+"/ghost.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	if _, err := schema.MigrateToCurrent(raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	broker, err := permissions.Open(raw, permissions.ModeAsk, 0)
	if err != nil {
		t.Fatal(err)
	}
	events, err := cevents.Open(raw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	al.SetGovernance(NewGovernance(events, broker, "g1", "agent-main"))

	payload := `AI News Today: 14 Biggest Stories{"@context":"https://schema.org","@type":"Organization"}`
	al.tools.Register(&payloadTool{output: payload})

	res := al.governance.AuthorizeStandalone("req-p", "sess-p", "web.read", "probe_payload",
		map[string]interface{}{}, permissions.RiskConsequential)
	if res.Allowed || res.PendingID == "" {
		t.Fatalf("want a durable approval, got %+v", res)
	}

	var chunks []string
	resp, err := al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat-1",
		SessionKey: "sess-p",
		Content:    "allow once",
	}, func(s string) { chunks = append(chunks, s) }, nil)
	if err != nil {
		t.Fatalf("processMessage: %v", err)
	}

	streamed := strings.Join(chunks, "")
	if streamed == "" {
		t.Fatal("the owner must see a reply")
	}
	if strings.Contains(streamed, "schema.org") {
		t.Errorf("raw payload streamed to the owner as Ghost's reply: %q", short(streamed))
	}
	if strings.Contains(resp, "schema.org") {
		t.Errorf("raw payload returned as the reply: %q", short(resp))
	}
	if !strings.Contains(resp, "actual rundown") {
		t.Errorf("reply must be the model's answer, got %q", short(resp))
	}
}
