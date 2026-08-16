package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ianclemence/ghost/pkg/profiles"
)

type AgentMessageTool struct {
	manager *profiles.Manager
}

func NewAgentMessageTool(manager *profiles.Manager) *AgentMessageTool {
	return &AgentMessageTool{manager: manager}
}

func (t *AgentMessageTool) Name() string {
	return "agent_message"
}

func (t *AgentMessageTool) Description() string {
	return "Send messages to other bots via CLI handoff or shared channels. Supports @mentions and multi-bot channels."
}

func (t *AgentMessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Target profile name (for CLI handoff) or channel_id (for channel send)",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Message to send",
			},
			"sender": map[string]interface{}{
				"type":        "string",
				"description": "Sender profile name",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"cli", "channel"},
				"description": "Delivery mode: 'cli' for CLI handoff, 'channel' for shared channel",
			},
		},
		"required": []string{"to", "message", "sender"},
	}
}

func (t *AgentMessageTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	to, _ := args["to"].(string)
	message, _ := args["message"].(string)
	sender, _ := args["sender"].(string)
	mode, _ := args["mode"].(string)

	if to == "" {
		return ErrorResult("to is required")
	}
	if message == "" {
		return ErrorResult("message is required")
	}
	if sender == "" {
		sender = "unknown"
	}

	if mode == "channel" {
		return t.sendViaChannel(to, sender, message)
	}

	return t.sendViaCLI(to, sender, message)
}

func (t *AgentMessageTool) sendViaCLI(to, sender, message string) *ToolResult {
	formattedMsg := fmt.Sprintf("[Message from '%s'] %s", sender, message)

	cmd := exec.Command("ghost", "-p", to, "agent", "-c", "Agent Inbox", "-m", formattedMsg)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			errMsg = err.Error()
		}
		return ErrorResult(fmt.Sprintf("failed to deliver message to '%s': %s", to, errMsg))
	}

	return UserResult(fmt.Sprintf("Message delivered to '%s' via CLI", to))
}

func (t *AgentMessageTool) sendViaChannel(channelID, sender, message string) *ToolResult {
	if t.manager == nil {
		return ErrorResult("Profile manager not available")
	}

	if err := t.manager.SendChannelMessage(channelID, sender, message); err != nil {
		return ErrorResult(fmt.Sprintf("failed to send to channel: %v", err))
	}

	ch, err := t.manager.GetChannel(channelID)
	if err != nil {
		return UserResult(fmt.Sprintf("Message sent to channel '%s'", channelID))
	}

	return UserResult(fmt.Sprintf("Message sent to channel '%s' (members: %v)", ch.Name, ch.Members))
}
