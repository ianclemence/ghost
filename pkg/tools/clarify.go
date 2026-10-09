package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cards"
)

// ClarifyTool asks the owner something Ghost needs to go on. On a surface that
// draws cards the question is a card answered with a tap (or a few words);
// elsewhere it is the question as text. The turn waits for the answer for a
// while; once it stops waiting, the card stays answerable and the answer
// arrives as the owner's next message instead.
type ClarifyTool struct {
	bus     *bus.MessageBus
	pending map[string]chan string
	timeout time.Duration
	mu      sync.Mutex
	channel string
	chatID  string
	publish func(channel, chatID, sessionID string, c cards.Card)
}

func NewClarifyTool(bus *bus.MessageBus) *ClarifyTool {
	return &ClarifyTool{
		bus:     bus,
		pending: make(map[string]chan string),
		// Long enough for a person to read, think and tap; the card outlives it.
		timeout: 3 * time.Minute,
	}
}

// SetContext implements ContextualTool: the surface the turn came from.
func (t *ClarifyTool) SetContext(channel, chatID string) {
	t.mu.Lock()
	t.channel, t.chatID = channel, chatID
	t.mu.Unlock()
}

// SetPublisher wires the card emitter (the agent loop supplies the bus).
func (t *ClarifyTool) SetPublisher(fn func(channel, chatID, sessionID string, c cards.Card)) {
	t.publish = fn
}

// Waiting reports whether a turn is still waiting on questionID.
func (t *ClarifyTool) Waiting(questionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.pending[questionID]
	return ok
}

// Timeout implements TimeoutTool so the reliability wrapper applies a bounded
// per-attempt deadline matching the tool's own timeout instead of the generic
// 5-minute default.
func (t *ClarifyTool) Timeout() time.Duration {
	return t.timeout
}

func (t *ClarifyTool) Name() string {
	return "clarify"
}

func (t *ClarifyTool) Description() string {
	return "Ask the owner a question you need answered to go on, when the request is genuinely ambiguous. Give choices (2 to 8 short options) when the answer is one of a few; leave them out for an open question. On the phone it is a card they answer with a tap. Ask one question at a time, and only when guessing would be worse than asking."
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
				"description": "Optional choices (2 to 8). If empty, the owner answers in their own words.",
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
	ch := t.pending[questionID]
	channel, chatID := t.channel, t.chatID
	t.mu.Unlock()

	// Where cards are drawn, the question is one: it stays in the
	// conversation, answerable, after this turn stops waiting.
	if t.publish != nil && channel != "" && chatID != "" {
		if card, err := cards.NewQuestion(question, choices, questionID); err == nil {
			t.publish(channel, chatID, SessionKeyFromContext(ctx), card)
		}
	}

	defer func() {
		t.mu.Lock()
		delete(t.pending, questionID)
		t.mu.Unlock()
	}()

	t.bus.PublishOutbound(bus.OutboundMessage{
		Channel: "clarify",
		Content: question,
		Metadata: map[string]interface{}{
			"type":        "clarify_request",
			"question_id": questionID,
			"choices":     choices,
		},
	})

	select {
	case response := <-ch:
		result := map[string]interface{}{
			"question":      question,
			"choices":       choices,
			"user_response": response,
		}
		raw, _ := json.Marshal(result)
		return UserResult(string(raw))
	case <-ctx.Done():
		return ErrorResult("clarify timed out or cancelled")
	case <-time.After(t.timeout):
		return ErrorResult("The owner has not answered yet. The question stays on their screen; their answer will arrive as their next message. Stop here and say in one short sentence that you will carry on when they answer.")
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
