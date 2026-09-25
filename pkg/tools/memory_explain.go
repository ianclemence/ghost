package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/personalcontext"
)

// MemoryExplainTool answers "why do you believe that?" with the receipt: the
// verbatim quote, the message it came from, confidence, provenance, and
// whether the belief was later corrected or forgotten. Read-only; it never
// edits memory and never invents evidence.
type MemoryExplainTool struct {
	workspace string
	// channel/chatID are set per call by the registry (ContextualTool) so the
	// receipt can also be published as a rich card to the surface in use.
	channel string
	chatID  string
	// publish emits the receipt card; nil means text-only (CLI, tests).
	publish func(channel, chatID, sessionID string, c cards.Card)
}

func NewMemoryExplainTool(workspace string) *MemoryExplainTool {
	return &MemoryExplainTool{workspace: workspace}
}

// SetContext implements tools.ContextualTool: the registry hands each tool
// the surface the turn came from.
func (t *MemoryExplainTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

// SetPublisher wires the card emitter (the agent loop supplies the bus).
func (t *MemoryExplainTool) SetPublisher(fn func(channel, chatID, sessionID string, c cards.Card)) {
	t.publish = fn
}

func (t *MemoryExplainTool) Name() string { return "memory_explain" }

func (t *MemoryExplainTool) Description() string {
	return "Explain why Ghost believes something: the verbatim quote, the conversation it came from, confidence, and whether it was superseded or forgotten. Use when the user asks \"why do you think that?\" or \"where did you learn that?\". Give the receipt plainly; if there is no quote (an older belief), say so instead of inventing one."
}

func (t *MemoryExplainTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Belief id to explain (preferred when known).",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search words to find the belief to explain when the id is unknown.",
			},
		},
	}
}

func (t *MemoryExplainTool) Timeout() time.Duration { return 5 * time.Second }

func (t *MemoryExplainTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	store, err := personalcontext.Open(t.workspace)
	if err != nil {
		return ErrorResult("I can't read my memory notes right now.")
	}
	id := strings.TrimSpace(sarg(args, "id"))
	query := strings.TrimSpace(sarg(args, "query"))

	if id != "" {
		ex, err := store.Explain(id)
		if err != nil {
			return ErrorResult(fmt.Sprintf("I don't have a memory with id %q.", id))
		}
		t.publishReceipt(ctx, ex)
		return NewToolResult(formatExplanation(ex))
	}

	if query == "" {
		return ErrorResult("Give me either an id or a search phrase.")
	}
	needle := strings.ToLower(query)
	var matches []personalcontext.Entry
	for _, e := range store.Current() {
		hay := strings.ToLower(personalcontext.Title(e) + " " + personalcontext.Summary(e) + " " + personalcontext.Value(e))
		if strings.Contains(hay, needle) {
			matches = append(matches, e)
			if len(matches) == 3 {
				break
			}
		}
	}
	if len(matches) == 0 {
		return NewToolResult(fmt.Sprintf("Nothing in my memory matches %q.", query))
	}
	if len(matches) == 1 {
		ex, err := store.Explain(matches[0].ID)
		if err != nil {
			return ErrorResult("I found the memory but couldn't read its receipt.")
		}
		t.publishReceipt(ctx, ex)
		return NewToolResult(formatExplanation(ex))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "A few memories match %q. Ask about one by id for its full receipt:\n", query)
	for _, e := range matches {
		fmt.Fprintf(&sb, "- %s: %s (id %s)\n", personalcontext.Title(e), personalcontext.Value(e), e.ID)
	}
	return NewToolResult(strings.TrimSpace(sb.String()))
}

// formatExplanation renders a receipt as plain facts the model can relay.
func formatExplanation(ex personalcontext.Explanation) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Belief: %s — %s\n", ex.Kind, ex.Title)
	if ex.Value != "" {
		fmt.Fprintf(&sb, "Value: %s\n", ex.Value)
	}
	fmt.Fprintf(&sb, "Confidence: %.0f%%\n", ex.Confidence*100)
	fmt.Fprintf(&sb, "Recorded: %s\n", ex.Entry.CreatedAt.Format("2006-01-02"))
	if len(ex.MessageIDs) > 0 {
		fmt.Fprintf(&sb, "From message: %s\n", strings.Join(ex.MessageIDs, ", "))
	}
	if ex.Quote != "" {
		fmt.Fprintf(&sb, "Quote: %q\n", ex.Quote)
	} else {
		sb.WriteString("Quote: none captured (recorded before receipts existed)\n")
	}
	switch {
	case ex.ForgottenAt != nil:
		reason := ex.ForgottenReason
		if reason == "" {
			reason = "asked to forget it"
		}
		fmt.Fprintf(&sb, "Status: forgotten on %s (%s)\n", ex.ForgottenAt.Format("2006-01-02"), reason)
	case ex.SupersededBy != nil:
		fmt.Fprintf(&sb, "Status: replaced by a newer belief (%s: %s)\n",
			ex.SupersededBy.ID, personalcontext.Value(*ex.SupersededBy))
	default:
		sb.WriteString("Status: current\n")
	}
	return strings.TrimSpace(sb.String())
}

// publishReceipt emits the receipt as a rich card for surfaces that render
// cards (the phone). Text-only callers leave publish nil. The card is
// informational: forgetting stays an explicit owner act, never a button.
func (t *MemoryExplainTool) publishReceipt(ctx context.Context, ex personalcontext.Explanation) {
	if t.publish == nil || t.channel == "" || t.chatID == "" {
		return
	}
	card, err := cards.New(cards.KindMemoryReceipt, "Why Ghost knows this", ex.Quote)
	if err != nil {
		return
	}
	status := "current"
	switch {
	case ex.ForgottenAt != nil:
		status = "forgotten"
	case ex.SupersededBy != nil:
		status = "replaced"
	}
	card.Data = map[string]interface{}{
		"claim_id":   ex.Entry.ID,
		"quote":      ex.Quote,
		"confidence": ex.Confidence,
		"status":     status,
		"source":     strings.Join(ex.MessageIDs, ", "),
		"value":      ex.Value,
		"kind":       ex.Kind,
		"learned":    ex.Entry.CreatedAt.Format("2006-01-02"),
	}
	t.publish(t.channel, t.chatID, SessionKeyFromContext(ctx), card)
}
