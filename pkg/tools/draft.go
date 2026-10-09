package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
)

// DraftTool writes something for the owner to send (an email, a calendar
// invite, a text message) and puts it in front of them in full, editable. It
// never sends: the owner does, with the card's Send button, and the card then
// says what really happened. Where cards cannot be drawn, the draft comes back
// as text for the model to show and confirm in words.
type DraftTool struct {
	channel string
	chatID  string
	publish func(channel, chatID, sessionID string, c cards.Card)
}

func NewDraftTool() *DraftTool { return &DraftTool{} }

// SetContext implements ContextualTool.
func (t *DraftTool) SetContext(channel, chatID string) { t.channel, t.chatID = channel, chatID }

// SetPublisher wires the card emitter.
func (t *DraftTool) SetPublisher(fn func(channel, chatID, sessionID string, c cards.Card)) {
	t.publish = fn
}

func (t *DraftTool) Name() string { return "draft" }

func (t *DraftTool) Description() string {
	return `Write something for the owner to send and show it to them in full, editable, before anything leaves: an email, a calendar event or invite, a text message (sms), or an alarm for their phone. Use it whenever the owner asks you to write, reply to, email, text, message or invite someone, or to put something in their calendar. Nothing is sent by this tool: the owner reads it, can change any field, and taps Send themselves. Then tell them in one short sentence that it is ready for them to check.

Fields by kind:
- email: to (address, or several separated by commas), cc?, subject, body (plain text)
- event: subject (the event's title), start, end?, location?, body? (notes), all_day? ("true" for a whole day)
- sms: to (a name or a phone number), body
- alarm: start (the time, 07:30), subject? (a label, "Flight to Lamu")
Times are local: 2026-10-12T14:30, or 2026-10-12 for an all-day event. Write the body the way the owner writes: their voice, no signature unless they use one. If you do not know an email address, look it up (email_search) or ask; never invent one.`
}

func (t *DraftTool) Parameters() map[string]interface{} {
	s := map[string]interface{}{"type": "string"}
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"kind"},
		"properties": map[string]interface{}{
			"kind":     map[string]interface{}{"type": "string", "enum": []string{"email", "event", "sms", "alarm"}},
			"to":       s,
			"cc":       s,
			"subject":  s,
			"body":     s,
			"start":    s,
			"end":      s,
			"location": s,
			"all_day":  map[string]interface{}{"type": "boolean"},
		},
	}
}

func (t *DraftTool) Timeout() time.Duration { return 5 * time.Second }

func (t *DraftTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	kind := strings.TrimSpace(sarg(args, "kind"))
	fields := map[string]string{}
	for _, k := range []string{"to", "cc", "subject", "body", "start", "end", "location"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			fields[k] = v
		}
	}
	if b, ok := args["all_day"].(bool); ok && b && kind == cards.DraftEvent {
		fields["all_day"] = "true"
	}
	card, err := cards.NewDraft(kind, fields)
	if err != nil {
		return ErrorResult(fmt.Sprintf("The draft was not made: %v. Fix it and call draft again.", err))
	}
	if t.publish == nil || t.channel == "" || t.chatID == "" {
		// No card here (a terminal, a chat app): show it and confirm in words.
		return NewToolResult("This surface cannot show a draft card. Show the owner this draft and ask whether to send it; send it only after they say yes:\n" + draftText(card))
	}
	t.publish(t.channel, t.chatID, SessionKeyFromContext(ctx), card)
	return NewToolResult("The draft is on the owner's screen. It is sent only if they tap Send; you do not send it. Say in one short sentence that it is ready for them to check.")
}

func draftText(c cards.Card) string {
	var sb strings.Builder
	switch cards.DraftKindOf(c) {
	case cards.DraftEmail:
		fmt.Fprintf(&sb, "To: %s\n", cards.DraftField(c, "to"))
		if cc := cards.DraftField(c, "cc"); cc != "" {
			fmt.Fprintf(&sb, "Cc: %s\n", cc)
		}
		fmt.Fprintf(&sb, "Subject: %s\n\n%s", cards.DraftField(c, "subject"), cards.DraftField(c, "body"))
	case cards.DraftEvent:
		fmt.Fprintf(&sb, "%s\nStarts: %s", cards.DraftField(c, "subject"), cards.DraftField(c, "start"))
		if e := cards.DraftField(c, "end"); e != "" {
			fmt.Fprintf(&sb, "\nEnds: %s", e)
		}
		if l := cards.DraftField(c, "location"); l != "" {
			fmt.Fprintf(&sb, "\nWhere: %s", l)
		}
	case cards.DraftSMS:
		fmt.Fprintf(&sb, "To: %s\n%s", cards.DraftField(c, "to"), cards.DraftField(c, "body"))
	}
	return sb.String()
}
