package tools

import (
	"context"
	"testing"
)

func TestEnhancedMessageToolName(t *testing.T) {
	tool := NewMessageTool()
	if tool.Name() != "message" {
		t.Fatalf("expected name 'message', got %s", tool.Name())
	}
}

func TestEnhancedMessageToolDescription(t *testing.T) {
	tool := NewMessageTool()
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestEnhancedMessageToolParameters(t *testing.T) {
	tool := NewMessageTool()
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
}

func TestEnhancedMessageToolSend(t *testing.T) {
	tool := NewMessageTool()
	var sentChannel, sentChatID, sentContent string

	tool.SetSendCallback(func(channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		sentContent = content
		return nil
	})

	tool.SetContext("telegram", "12345")

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content": "Hello, World!",
	})

	if result.IsError {
		t.Fatalf("expected no error, got %s", result.ForLLM)
	}
	if sentChannel != "telegram" {
		t.Fatalf("expected channel 'telegram', got %s", sentChannel)
	}
	if sentChatID != "12345" {
		t.Fatalf("expected chat_id '12345', got %s", sentChatID)
	}
	if sentContent != "Hello, World!" {
		t.Fatalf("expected content 'Hello, World!', got %s", sentContent)
	}
}

func TestEnhancedMessageToolSendWithTarget(t *testing.T) {
	tool := NewMessageTool()
	var sentChannel, sentChatID string

	tool.SetSendCallback(func(channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		return nil
	})

	tool.SetContext("telegram", "12345")

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content":  "Hello, World!",
		"channel":  "whatsapp",
		"chat_id":  "67890",
	})

	if result.IsError {
		t.Fatalf("expected no error, got %s", result.ForLLM)
	}
	if sentChannel != "whatsapp" {
		t.Fatalf("expected channel 'whatsapp', got %s", sentChannel)
	}
	if sentChatID != "67890" {
		t.Fatalf("expected chat_id '67890', got %s", sentChatID)
	}
}

func TestEnhancedMessageToolReact(t *testing.T) {
	tool := NewMessageTool()
	var reactedMessageID, reactedEmoji string

	tool.SetReactionCallback(func(channel, chatID, messageID, emoji string) error {
		reactedMessageID = messageID
		reactedEmoji = emoji
		return nil
	})

	tool.SetContext("telegram", "12345")

	result := tool.Execute(context.Background(), map[string]interface{}{
		"action":     "react",
		"emoji":      "👍",
		"message_id": "msg-123",
	})

	if result.IsError {
		t.Fatalf("expected no error, got %s", result.ForLLM)
	}
	if reactedEmoji != "👍" {
		t.Fatalf("expected emoji '👍', got %s", reactedEmoji)
	}
	if reactedMessageID != "msg-123" {
		t.Fatalf("expected message_id 'msg-123', got %s", reactedMessageID)
	}
}

func TestEnhancedMessageToolReactMissingEmoji(t *testing.T) {
	tool := NewMessageTool()

	result := tool.Execute(context.Background(), map[string]interface{}{
		"action":     "react",
		"message_id": "msg-123",
	})

	if !result.IsError {
		t.Fatal("expected error for missing emoji")
	}
}

func TestEnhancedMessageToolReactMissingMessageID(t *testing.T) {
	tool := NewMessageTool()

	result := tool.Execute(context.Background(), map[string]interface{}{
		"action": "react",
		"emoji":  "👍",
	})

	if !result.IsError {
		t.Fatal("expected error for missing message_id")
	}
}

func TestEnhancedMessageToolList(t *testing.T) {
	tool := NewMessageTool()

	tool.SetListTargetsCallback(func() []TargetInfo {
		return []TargetInfo{
			{Channel: "telegram", ChatID: "12345", Name: "John"},
			{Channel: "whatsapp", ChatID: "67890", Name: "Jane", Alias: "jane"},
		}
	})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"action":  "list",
		"content": "",
	})

	if result.IsError {
		t.Fatalf("expected no error, got %s", result.ForLLM)
	}
}

func TestEnhancedMessageToolResolveName(t *testing.T) {
	tool := NewMessageTool()

	tool.UpdateNameCache([]TargetInfo{
		{Channel: "telegram", ChatID: "12345", Name: "John", Alias: "johnny"},
	})

	channel, chatID, found := tool.ResolveName("John")
	if !found {
		t.Fatal("expected to find name 'John'")
	}
	if channel != "telegram" {
		t.Fatalf("expected channel 'telegram', got %s", channel)
	}
	if chatID != "12345" {
		t.Fatalf("expected chat_id '12345', got %s", chatID)
	}

	channel, chatID, found = tool.ResolveName("johnny")
	if !found {
		t.Fatal("expected to find alias 'johnny'")
	}
	if channel != "telegram" {
		t.Fatalf("expected channel 'telegram', got %s", channel)
	}

	_, _, found = tool.ResolveName("nonexistent")
	if found {
		t.Fatal("expected not to find name 'nonexistent'")
	}
}

func TestEnhancedMessageToolSendWithNameResolution(t *testing.T) {
	tool := NewMessageTool()
	var sentChannel, sentChatID string

	tool.SetSendCallback(func(channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		return nil
	})

	tool.UpdateNameCache([]TargetInfo{
		{Channel: "telegram", ChatID: "12345", Name: "John"},
	})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content": "Hello, John!",
		"name":    "John",
	})

	if result.IsError {
		t.Fatalf("expected no error, got %s", result.ForLLM)
	}
	if sentChannel != "telegram" {
		t.Fatalf("expected channel 'telegram', got %s", sentChannel)
	}
	if sentChatID != "12345" {
		t.Fatalf("expected chat_id '12345', got %s", sentChatID)
	}
}

func TestEnhancedMessageToolNoCallback(t *testing.T) {
	tool := NewMessageTool()
	tool.SetContext("telegram", "12345")

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content": "Hello, World!",
	})

	if !result.IsError {
		t.Fatal("expected error for no callback")
	}
}

func TestEnhancedMessageToolNoTarget(t *testing.T) {
	tool := NewMessageTool()

	result := tool.Execute(context.Background(), map[string]interface{}{
		"content": "Hello, World!",
	})

	if !result.IsError {
		t.Fatal("expected error for no target")
	}
}

func TestEnhancedMessageToolHasSentInRound(t *testing.T) {
	tool := NewMessageTool()

	if tool.HasSentInRound() {
		t.Fatal("expected false initially")
	}

	tool.SetSendCallback(func(channel, chatID, content string) error {
		return nil
	})

	tool.SetContext("telegram", "12345")
	tool.Execute(context.Background(), map[string]interface{}{
		"content": "Hello, World!",
	})

	if !tool.HasSentInRound() {
		t.Fatal("expected true after sending")
	}
}

func TestEnhancedMessageToolSetContext(t *testing.T) {
	tool := NewMessageTool()
	tool.SetContext("telegram", "12345")

	if tool.defaultChannel != "telegram" {
		t.Fatalf("expected channel 'telegram', got %s", tool.defaultChannel)
	}
	if tool.defaultChatID != "12345" {
		t.Fatalf("expected chat_id '12345', got %s", tool.defaultChatID)
	}
}
