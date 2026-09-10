package agent

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

// panicProvider panics on its first model call, simulating a driver/provider
// failure deep inside a turn.
type panicProvider struct{}

func (p *panicProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	panic("provider exploded")
}
func (p *panicProvider) GetDefaultModel() string { return "mock-model" }

// A panic anywhere in turn processing must be contained and converted into a
// failed turn, never crash the appliance or claim success.
func TestTurnPanicContained(t *testing.T) {
	ws := t.TempDir()
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{
		Workspace: ws, Model: "mock-model", MaxTokens: 1024, MaxToolIterations: 5,
	}}}
	al, err := NewAgentLoop(cfg, bus.NewMessageBus(), &panicProvider{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { al.Stop() })

	msg := bus.InboundMessage{
		Channel: "web", SenderID: "t", ChatID: "chat",
		Content:    "tell me a long story about a fox",
		SessionKey: "sess-panic",
		Metadata:   map[string]string{"request_id": "req-panic"},
	}
	resp, err := al.processMessage(context.Background(), msg, nil, nil)
	if err == nil {
		t.Fatalf("a provider panic must produce a failed turn, got response %q", resp)
	}
}
