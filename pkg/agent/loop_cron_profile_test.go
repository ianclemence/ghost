package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

type cronProfileMockProvider struct {
	call int
}

func (m *cronProfileMockProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	m.call++
	if m.call == 1 {
		return &providers.LLMResponse{
			Content: "",
			ToolCalls: []providers.ToolCall{
				{
					ID:   "tc-1",
					Name: "shell",
					Arguments: map[string]interface{}{
						"command": "echo test",
					},
				},
			},
		}, nil
	}
	return &providers.LLMResponse{
		Content:   "done",
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *cronProfileMockProvider) GetDefaultModel() string { return "mock-model" }
func (m *cronProfileMockProvider) SupportsTools() bool     { return true }
func (m *cronProfileMockProvider) GetContextWindow() int   { return 4096 }

func TestCronTriggeredMessagesUseHeartbeatSafeProfile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-cron-profile-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "mock-model",
				MaxTokens:         4096,
				MaxToolIterations: 3,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &cronProfileMockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "tester",
		ChatID:     "direct",
		Content:    "run shell command",
		SessionKey: "cron-job-1",
	}, nil, nil)
	if err != nil {
		t.Fatalf("processMessage error: %v", err)
	}

	history := al.sessions.GetHistory("cron-job-1")
	foundBlockedMessage := false
	for _, msg := range history {
		if msg.Role == "tool" && strings.Contains(msg.Content, "tool shell not available in profile heartbeat-safe") {
			foundBlockedMessage = true
			break
		}
	}
	if !foundBlockedMessage {
		t.Fatalf("expected blocked tool message for heartbeat-safe profile in cron session history")
	}
}
