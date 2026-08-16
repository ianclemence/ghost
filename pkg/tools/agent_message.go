package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type AgentMessageTool struct {
}

func NewAgentMessageTool() *AgentMessageTool {
	return &AgentMessageTool{}
}

func (t *AgentMessageTool) Name() string {
	return "agent_message"
}

func (t *AgentMessageTool) Description() string {
	return "Send a message to another bot profile. The message will be delivered via the agent CLI."
}

func (t *AgentMessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"to": map[string]interface{}{
				"type":        "string",
				"description": "Target profile name",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Message to send",
			},
			"sender": map[string]interface{}{
				"type":        "string",
				"description": "Sender profile name (for attribution)",
			},
		},
		"required": []string{"to", "message"},
	}
}

func (t *AgentMessageTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	to, _ := args["to"].(string)
	message, _ := args["message"].(string)
	sender, _ := args["sender"].(string)

	if to == "" {
		return ErrorResult("to is required")
	}
	if message == "" {
		return ErrorResult("message is required")
	}

	if sender == "" {
		sender = "unknown"
	}

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

	return UserResult(fmt.Sprintf("Message delivered to '%s': %s", to, truncate(message, 100)))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
