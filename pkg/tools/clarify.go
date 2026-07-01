package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
)

type ClarifyTool struct {
	bus     *bus.MessageBus
	pending map[string]chan string
	timeout time.Duration
	mu      sync.Mutex
}

func NewClarifyTool(bus *bus.MessageBus) *ClarifyTool {
	return &ClarifyTool{
		bus:     bus,
		pending: make(map[string]chan string),
		timeout: 5 * time.Minute,
	}
}

func (t *ClarifyTool) Name() string {
	return "clarify"
}

func (t *ClarifyTool) Description() string {
	return "Ask the user a question to clarify ambiguous requests. Supports multiple-choice (up to 4 options) or open-ended questions."
}

func (t *ClarifyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"question": map[string]interface{}{
				"type":        "string",
				"description": "The question to ask the user",
			},
			"choices": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "string",
				},
				"description": "Optional multiple-choice options (up to 4). If empty, user can type freeform response.",
			},
		},
		"required": []string{"question"},
	}
}

func (t *ClarifyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	question, _ := args["question"].(string)
	if question == "" {
		return ErrorResult("question is required")
	}

	var choices []string
	if rawChoices, ok := args["choices"].([]interface{}); ok {
		for _, raw := range rawChoices {
			if s, ok := raw.(string); ok && s != "" {
				choices = append(choices, s)
			}
		}
	}

	questionID := fmt.Sprintf("q-%d", time.Now().UnixMilli())

	t.mu.Lock()
	t.pending[questionID] = make(chan string, 1)
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, questionID)
		t.mu.Unlock()
	}()

	t.bus.PublishOutbound(bus.OutboundMessage{
		Channel: "clarify",
		Content: question,
		Metadata: map[string]interface{}{
			"type":      "clarify_request",
			"question_id": questionID,
			"choices":   choices,
		},
	})

	select {
	case response := <-t.pending[questionID]:
		result := map[string]interface{}{
			"question":     question,
			"choices":      choices,
			"user_response": response,
		}
		raw, _ := json.Marshal(result)
		return UserResult(string(raw))
	case <-ctx.Done():
		return ErrorResult("clarify timed out or cancelled")
	case <-time.After(t.timeout):
		return ErrorResult("clarify timed out waiting for response")
	}
}

func (t *ClarifyTool) HandleResponse(questionID, response string) bool {
	t.mu.Lock()
	ch, ok := t.pending[questionID]
	t.mu.Unlock()

	if !ok {
		return false
	}

	select {
	case ch <- response:
		return true
	default:
		return false
	}
}

func (t *ClarifyTool) PendingCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.pending)
}
