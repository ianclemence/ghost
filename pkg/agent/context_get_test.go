package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/providers"
)

func mustPCValue(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	raw, err := personalcontext.RawValue(v)
	if err != nil {
		t.Fatalf("RawValue(%v): %v", v, err)
	}
	return raw
}

func writeBlockerFile(t *testing.T, workspace string) error {
	t.Helper()
	return os.WriteFile(filepath.Join(workspace, "personal-context"), []byte("blocked"), 0644)
}

// scriptedContextGetProvider lets the model request context_get through the
// real agent tool interface: the first Chat call requests the tool, later
// calls answer with a fixed final message. It records the tool definitions it
// was given and the messages it received so tests can assert the model saw the
// tool and got the structured result back.
//
// Only Chat calls that carry a system message are recorded as model turns. The
// agent's async autoJournal goroutine also calls the provider to summarize the
// session after a turn, and that call carries no system message; recording it
// would clobber the turn's last messages and race the test's assertions.
type scriptedContextGetProvider struct {
	calls        int
	firstTools   []providers.ToolDefinition
	lastMessages []providers.Message
	toolCall     providers.ToolCall
	finalAnswer  string
}

func (m *scriptedContextGetProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	isTurnCall := false
	for _, msg := range messages {
		if msg.Role == "system" {
			isTurnCall = true
			break
		}
	}
	if isTurnCall {
		m.calls++
		m.lastMessages = messages
	}
	if m.calls == 1 {
		m.firstTools = tools
		return &providers.LLMResponse{Content: "", ToolCalls: []providers.ToolCall{m.toolCall}}, nil
	}
	return &providers.LLMResponse{Content: m.finalAnswer, ToolCalls: []providers.ToolCall{}}, nil
}

func (m *scriptedContextGetProvider) GetDefaultModel() string {
	return "mock-model"
}

func newTestAgentLoopWithProvider(t *testing.T, workspace string, provider providers.LLMProvider) *AgentLoop {
	t.Helper()
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         workspace,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	al, _ := NewAgentLoop(cfg, msgBus, provider)
	t.Cleanup(func() { al.Stop() })
	return al
}

// TestContextGetToolRegistered verifies context_get is exposed through the
// real Ghost tool mechanism: registered on the agent registry and present in
// the provider tool definitions the model receives.
func TestContextGetToolRegistered(t *testing.T) {
	ws := t.TempDir()
	al := newTestAgentLoop(t, ws)

	tool, ok := al.tools.Get("context_get")
	if !ok {
		t.Fatal("context_get tool not registered on the agent registry")
	}
	if tool.Name() != "context_get" {
		t.Fatalf("registered tool name = %q, want context_get", tool.Name())
	}

	found := false
	for _, def := range al.tools.ToProviderDefs() {
		if def.Function.Name == "context_get" {
			found = true
			if def.Function.Description == "" {
				t.Error("context_get definition has an empty description")
			}
			if def.Function.Parameters == nil {
				t.Error("context_get definition has no parameters schema")
			}
		}
	}
	if !found {
		t.Error("context_get missing from the provider tool definitions (model cannot see it)")
	}
}

// TestContextGetModelInvokesTool verifies the model can actually see and
// invoke context_get through the normal agent tool interface: the provider
// requests the tool, the loop executes it against the real workspace store,
// and the structured result (entry + provenance) is fed back.
func TestContextGetModelInvokesTool(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &scriptedContextGetProvider{
		toolCall: providers.ToolCall{
			ID:   "call_context_get",
			Name: "context_get",
			Arguments: map[string]interface{}{
				"predicate": "fact/work",
			},
		},
		finalAnswer: "You work at Acme.",
	}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	// Seed current context directly through the store the agent owns.
	if _, err := al.pcStore.Create(personalcontext.Entry{
		ID:         "ec_work",
		Kind:       personalcontext.KindFact,
		Subject:    "user",
		Predicate:  "fact/work",
		Value:      mustPCValue(t, "Acme"),
		Status:     personalcontext.StatusCurrent,
		Confidence: 0.95,
		Sources: []personalcontext.Source{{
			Type:      personalcontext.SourceConversation,
			Kind:      personalcontext.SourceUserDeclared,
			Ref:       "s1:m1",
			Timestamp: time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("seed context: %v", err)
	}

	resp := runTurn(t, al, "Where do I work?", "s1")
	if !strings.Contains(resp, "Acme") {
		t.Errorf("final response = %q, want it to mention Acme", resp)
	}

	// The model saw the tool definition before requesting it.
	found := false
	for _, def := range provider.firstTools {
		if def.Function.Name == "context_get" {
			found = true
		}
	}
	if !found {
		t.Error("context_get not in the tool definitions the model received")
	}

	// The structured tool result with entry data and provenance was fed back.
	sawToolResult := false
	for _, m := range provider.lastMessages {
		if m.Role != "tool" {
			continue
		}
		sawToolResult = true
		if !strings.Contains(m.Content, "fact/work") || !strings.Contains(m.Content, "Acme") {
			t.Errorf("tool result missing entry data: %q", m.Content)
		}
		if !strings.Contains(m.Content, "s1:m1") {
			t.Errorf("tool result missing provenance ref: %q", m.Content)
		}
	}
	if !sawToolResult {
		t.Error("no tool result message was fed back to the model")
	}
}

// TestContextGetAbsentWhenStoreUnavailable verifies that when the Personal
// Context store cannot open, the agent still runs and simply has no
// context_get tool (the agent is never broken by a missing store).
func TestContextGetAbsentWhenStoreUnavailable(t *testing.T) {
	ws := t.TempDir()
	if err := writeBlockerFile(t, ws); err != nil {
		t.Fatalf("block store: %v", err)
	}

	al := newTestAgentLoop(t, ws)
	if _, ok := al.tools.Get("context_get"); ok {
		t.Error("context_get should not be registered when the personal context store is unavailable")
	}

	if resp := runTurn(t, al, "hello", "s1"); resp == "" {
		t.Fatal("turn produced no response when the store is unavailable")
	}
}
