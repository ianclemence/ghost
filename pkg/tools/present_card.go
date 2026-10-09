package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
)

// PresentCardTool lets Ghost show an answer as a card instead of writing it:
// a weather glance, a flight in progress, a plan, a comparison. The model
// fills blocks from a fixed catalog; the card catalog validates every field
// and refuses anything else, so a card can show things but never run, open or
// style anything. It renders to the owner's own screen only.
type PresentCardTool struct {
	channel string
	chatID  string
	publish func(channel, chatID, sessionID string, c cards.Card)
}

func NewPresentCardTool() *PresentCardTool { return &PresentCardTool{} }

// SetContext implements ContextualTool: the surface the turn came from.
func (t *PresentCardTool) SetContext(channel, chatID string) {
	t.channel = channel
	t.chatID = chatID
}

// SetPublisher wires the card emitter (the agent loop supplies the bus).
func (t *PresentCardTool) SetPublisher(fn func(channel, chatID, sessionID string, c cards.Card)) {
	t.publish = fn
}

func (t *PresentCardTool) Name() string { return "present_card" }

func (t *PresentCardTool) Description() string {
	return `Show the owner a card instead of a paragraph, when a glance beats reading: today's weather, a flight's progress, a plan for the day, a few options side by side, a checklist. Do NOT use it for an ordinary reply, an explanation, or anything that reads fine as a sentence.

A card is a title plus 1 to 8 blocks, in order. Block types and their fields:
- metric: label, value (a short number or word), optional unit, delta (e.g. "+2° vs yesterday"), tone
- facts: rows [{label, value, tone?}] (up to 10), for details like "Gate: B12"
- list: items [{title, subtitle?, trailing?, tone?}] (up to 12), for options or a checklist
- timeline: steps [{time?, title, detail?, state?}] (up to 8; state is done, now or next), for a journey or a day's plan
- progress: label, progress (0 to 1), optional caption
- note: text, tone, a one-line caution or tip
- text: text (a short sentence; keep it under 400 characters)
- code: language, code (up to 40 lines)
- compare: options [{label, detail?}] (2 to 4, the things compared), rows [{label, values [one per option], tone?}] (up to 8), pick? (0-based index of the one you recommend)
- chart: chart ("bar" or "line"), label, points [{label, value}] (2 to 24), unit?, caption?
- map: places [{name, detail?, lat, lon}] (up to 10), only with real coordinates you got from a tool
tone is one of neutral, good, warn, bad, info. Everything is plain text: no links, no markup, no HTML.

A card can also ASK, so the owner answers with a tap instead of typing. Input blocks each need a key (lowercase, the name the answer comes back under):
- choice: key, label, options [{id, label, detail?}] (2 to 8), multiple? (true lets them pick several)
- datetime: key, label, mode ("date", "time" or "datetime"), value? (a suggestion, in the form 2026-10-12 / 14:30 / 2026-10-12T14:30), earliest?, optional? (true when the owner may not know it, like someone's birthday: never make them invent one)
- slider: key, label, min, max, step?, number? (where it starts), unit?
- field: key, label, placeholder?, optional?, multiline? (short text)
- checklist: key, label?, checks [{id, label, done?}] (up to 20). A card whose only input is a checklist needs no submit: the owner ticks it as they go (groceries, packing) and the ticks are kept.
A card that asks needs exactly one action of kind "submit" (label like "Send" or "Book it"). The owner's answers come back to you as their next message ("Where: Cafe Brera / When: Fri 16 Oct, 19:30"). Ask with a card when the answer is a pick among options, a date, an amount, or a few short fields; ask in a sentence when it is open-ended.

Optionally add up to 3 actions: {id, label, kind} where kind is "reply" (with text: what is sent to you as if the owner typed it, e.g. "Book the 11:00"), "submit" (only on a card that asks), or "dismiss". An action never runs anything by itself.

Examples:
 weather: title "Nairobi today", blocks [metric {label "Now", value "24", unit "°C", tone good}, facts {rows [{label "Rain", value "10%"}]}, note {text "Dry until evening.", tone info}]
 flight: title "TP 1352", blocks [timeline {steps [{time "09:40", title "Depart Lisbon", state "done"}, {time "11:05", title "Land Nairobi", state "now"}]}, progress {label "Flight", progress 0.6}]

After presenting, add at most one short sentence of text. Never repeat what the card already shows.`
}

func (t *PresentCardTool) Parameters() map[string]interface{} {
	str := map[string]interface{}{"type": "string"}
	tone := map[string]interface{}{"type": "string", "enum": []string{"neutral", "good", "warn", "bad", "info"}}
	return map[string]interface{}{
		"type":     "object",
		"required": []string{"title", "blocks"},
		"properties": map[string]interface{}{
			"title": map[string]interface{}{"type": "string", "description": "A short headline, up to 80 characters."},
			"body":  map[string]interface{}{"type": "string", "description": "Optional one-line summary under the title."},
			"blocks": map[string]interface{}{
				"type":        "array",
				"description": "1 to 8 blocks, in reading order.",
				"items": map[string]interface{}{
					"type":     "object",
					"required": []string{"type"},
					"properties": map[string]interface{}{
						"type":        map[string]interface{}{"type": "string", "enum": []string{"text", "facts", "list", "metric", "progress", "note", "timeline", "code", "compare", "chart", "map", "choice", "datetime", "slider", "field", "checklist"}},
						"key":         str,
						"mode":        map[string]interface{}{"type": "string", "enum": []string{"date", "time", "datetime"}},
						"earliest":    str,
						"placeholder": str,
						"multiple":    map[string]interface{}{"type": "boolean"},
						"optional":    map[string]interface{}{"type": "boolean"},
						"multiline":   map[string]interface{}{"type": "boolean"},
						"min":         map[string]interface{}{"type": "number"},
						"max":         map[string]interface{}{"type": "number"},
						"step":        map[string]interface{}{"type": "number"},
						"number":      map[string]interface{}{"type": "number"},
						"pick":        map[string]interface{}{"type": "integer"},
						"chart":       map[string]interface{}{"type": "string", "enum": []string{"bar", "line"}},
						"options": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"id": str, "label": str, "detail": str}}},
						"checks": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"id": str, "label": str, "done": map[string]interface{}{"type": "boolean"}}}},
						"points": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"label": str, "value": map[string]interface{}{"type": "number"}}}},
						"places": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"name": str, "detail": str, "lat": map[string]interface{}{"type": "number"}, "lon": map[string]interface{}{"type": "number"}}}},
						"text":     str,
						"label":    str,
						"value":    str,
						"unit":     str,
						"delta":    str,
						"caption":  str,
						"language": str,
						"code":     str,
						"tone":     tone,
						"progress": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
						"rows": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"label": str, "value": str, "values": map[string]interface{}{"type": "array", "items": str}, "tone": tone}}},
						"items": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"title": str, "subtitle": str, "trailing": str, "tone": tone}}},
						"steps": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object",
							"properties": map[string]interface{}{"time": str, "title": str, "detail": str,
								"state": map[string]interface{}{"type": "string", "enum": []string{"done", "now", "next"}}}}},
					},
				},
			},
			"actions": map[string]interface{}{
				"type":        "array",
				"description": "Up to 3 choices. A reply sends its text as if the owner typed it; submit sends the answers of a card that asks.",
				"items": map[string]interface{}{"type": "object", "required": []string{"id", "label", "kind"},
					"properties": map[string]interface{}{
						"id": str, "label": str, "text": str,
						"kind":  map[string]interface{}{"type": "string", "enum": []string{"reply", "submit", "dismiss"}},
						"style": map[string]interface{}{"type": "string", "enum": []string{"primary", "secondary"}},
					}},
			},
		},
	}
}

func (t *PresentCardTool) Timeout() time.Duration { return 5 * time.Second }

func (t *PresentCardTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	spec := map[string]interface{}{"kind": string(cards.KindPresent)}
	for _, k := range []string{"title", "body", "blocks", "actions"} {
		if v, ok := args[k]; ok && v != nil {
			spec[k] = v
		}
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return ErrorResult("That card could not be built.")
	}
	card, err := cards.RenderSpec(raw)
	if err != nil {
		// The reason goes back to the model so it can fix the card and try again.
		return ErrorResult(fmt.Sprintf("The card was not shown: %v. Fix it and call present_card again, or answer in text.", err))
	}
	if t.publish == nil || t.channel == "" || t.chatID == "" {
		// A surface that cannot draw cards (a terminal, a test) gets the same
		// answer as plain text, so nothing is ever lost.
		return NewToolResult("This surface cannot show cards. Tell the owner this instead:\n" + card.TextFallback())
	}
	t.publish(t.channel, t.chatID, SessionKeyFromContext(ctx), card)
	if card.Asks() {
		return NewToolResult("The card is on the owner's screen and asks them; their answers arrive as their next message. Say at most one short sentence and stop here: do not guess the answers or go on without them.")
	}
	return NewToolResult("The card is on the owner's screen. Add at most one short sentence; do not repeat it.")
}
