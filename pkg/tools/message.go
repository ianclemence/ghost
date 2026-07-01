package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type SendCallback func(channel, chatID, content string) error
type ReactionCallback func(channel, chatID, messageID, emoji string) error
type ListTargetsCallback func() []TargetInfo

type TargetInfo struct {
	Channel string
	ChatID  string
	Name    string
	Alias   string
}

type MessageTool struct {
	sendCallback       SendCallback
	reactionCallback   ReactionCallback
	listTargetsCallback ListTargetsCallback
	defaultChannel     string
	defaultChatID      string
	sentInRound        bool
	nameCache          map[string]TargetInfo
	mu                 sync.RWMutex
}

func NewMessageTool() *MessageTool {
	return &MessageTool{
		nameCache: make(map[string]TargetInfo),
	}
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a message, react to a message, or list available targets. Supports name resolution for contacts."
}

func (t *MessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The message content to send",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Optional: resolve name to chat_id (e.g. 'John' -> chat_id)",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Optional: action to perform (send, react, list)",
				"enum":        []string{"send", "react", "list"},
			},
			"emoji": map[string]interface{}{
				"type":        "string",
				"description": "Optional: emoji for reaction (e.g. '👍', '❤️')",
			},
			"message_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: message ID to react to",
			},
		},
		"required": []string{"content"},
	}
}

func (t *MessageTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
	t.sentInRound = false
}

func (t *MessageTool) HasSentInRound() bool {
	return t.sentInRound
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) SetReactionCallback(callback ReactionCallback) {
	t.reactionCallback = callback
}

func (t *MessageTool) SetListTargetsCallback(callback ListTargetsCallback) {
	t.listTargetsCallback = callback
}

func (t *MessageTool) ResolveName(name string) (string, string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, target := range t.nameCache {
		if strings.EqualFold(target.Name, name) || strings.EqualFold(target.Alias, name) {
			return target.Channel, target.ChatID, true
		}
	}
	return "", "", false
}

func (t *MessageTool) UpdateNameCache(targets []TargetInfo) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nameCache = make(map[string]TargetInfo)
	for _, target := range targets {
		key := strings.ToLower(target.Name)
		t.nameCache[key] = target
		if target.Alias != "" {
			aliasKey := strings.ToLower(target.Alias)
			t.nameCache[aliasKey] = target
		}
	}
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	action, _ := args["action"].(string)
	if action == "" {
		action = "send"
	}

	switch action {
	case "list":
		return t.handleList(ctx)
	case "react":
		return t.handleReact(ctx, args)
	default:
		return t.handleSend(ctx, args)
	}
}

func (t *MessageTool) handleList(ctx context.Context) *ToolResult {
	if t.listTargetsCallback == nil {
		return &ToolResult{ForLLM: "List targets not configured", IsError: true}
	}

	targets := t.listTargetsCallback()
	t.UpdateNameCache(targets)

	var sb strings.Builder
	sb.WriteString("Available targets:\n")
	for _, target := range targets {
		if target.Alias != "" {
			sb.WriteString(fmt.Sprintf("- %s (%s) [%s] - %s\n", target.Name, target.Alias, target.Channel, target.ChatID))
		} else {
			sb.WriteString(fmt.Sprintf("- %s [%s] - %s\n", target.Name, target.Channel, target.ChatID))
		}
	}

	return &ToolResult{ForLLM: sb.String()}
}

func (t *MessageTool) handleReact(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.reactionCallback == nil {
		return &ToolResult{ForLLM: "Reaction callback not configured", IsError: true}
	}

	emoji, _ := args["emoji"].(string)
	if emoji == "" {
		return &ToolResult{ForLLM: "emoji is required for reaction", IsError: true}
	}

	messageID, _ := args["message_id"].(string)
	if messageID == "" {
		return &ToolResult{ForLLM: "message_id is required for reaction", IsError: true}
	}

	channel, chatID := t.resolveTarget(args)
	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified for reaction", IsError: true}
	}

	if err := t.reactionCallback(channel, chatID, messageID, emoji); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending reaction: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	return &ToolResult{
		ForLLM: fmt.Sprintf("Reaction %s sent to message %s", emoji, messageID),
		Silent: true,
	}
}

func (t *MessageTool) handleSend(ctx context.Context, args map[string]interface{}) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	channel, chatID := t.resolveTarget(args)
	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Message sending not configured", IsError: true}
	}

	if err := t.sendCallback(channel, chatID, content); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending message: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	t.sentInRound = true
	return &ToolResult{
		ForLLM: fmt.Sprintf("Message sent to %s:%s", channel, chatID),
		Silent: true,
	}
}

func (t *MessageTool) resolveTarget(args map[string]interface{}) (string, string) {
	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if name, ok := args["name"].(string); ok && name != "" {
		if resolvedChannel, resolvedChatID, found := t.ResolveName(name); found {
			if channel == "" {
				channel = resolvedChannel
			}
			if chatID == "" {
				chatID = resolvedChatID
			}
		}
	}

	if channel == "" {
		channel = t.defaultChannel
	}
	if chatID == "" {
		chatID = t.defaultChatID
	}

	return channel, chatID
}
