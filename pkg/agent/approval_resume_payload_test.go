package agent

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

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

// approvingTool records that the paused call ran, to prove a button-driven
// approval executes it.
type approvingTool struct {
	mu   sync.Mutex
	runs int
}

func (a *approvingTool) Name() string        { return "probe_approve" }
func (a *approvingTool) Description() string { return "test approval tool" }
func (a *approvingTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (a *approvingTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	a.mu.Lock()
	a.runs++
	a.mu.Unlock()
	return tools.UserResult("done")
}
func (a *approvingTool) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.runs
}

// A button approval must run the paused call. Resolving the broker request
// alone cleared the card while the action never executed, so the
// conversation kept showing "waiting for your approval" after a tap.
func TestExecuteApprovedRequestRunsThePausedCallOnce(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: ws, Model: "mock-model", MaxTokens: 1024, MaxToolIterations: 5,
	}}}
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &simpleMockProvider{response: "done and reported"})
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

	tool := &approvingTool{}
	al.tools.Register(tool)

	res := al.governance.AuthorizeStandalone("req-a", "sess-a", "web.read", "probe_approve",
		map[string]interface{}{}, permissions.RiskConsequential)
	if res.Allowed || res.PendingID == "" {
		t.Fatalf("want a durable approval, got %+v", res)
	}

	// The button resolves the request and then resumes it.
	resolved, err := broker.ResolveAuto(res.PendingID, permissions.GrantOnce)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !al.ExecuteApprovedRequest(resolved, permissions.GrantOnce, "mobile", "default") {
		t.Fatal("approving a request with a continuation must resume it")
	}

	deadline := time.Now().Add(5 * time.Second)
	for tool.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if tool.count() != 1 {
		t.Fatalf("paused call ran %d times, want 1", tool.count())
	}

	// allow_once is exactly once: a second tap has nothing left to run.
	if al.ExecuteApprovedRequest(resolved, permissions.GrantOnce, "mobile", "default") {
		t.Fatal("allow_once resumed twice")
	}
	if tool.count() != 1 {
		t.Fatalf("paused call ran %d times after a second tap, want 1", tool.count())
	}
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
