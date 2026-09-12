package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// ScheduleService is the interface for creating scheduled items.
type ScheduleService interface {
	CreateItem(item *scheduled.ScheduledItem) error
}

// ScheduleTool provides natural-language scheduling capabilities for the agent.
// It parses natural language like "Remind me tomorrow at 9 AM" and creates
// the appropriate ScheduledItem.
type ScheduleTool struct {
	service ScheduleService
	channel string
	chatID  string
	tz      string
	mu      sync.RWMutex
}

// NewScheduleTool creates a new ScheduleTool.
func NewScheduleTool(service ScheduleService, timezone string) *ScheduleTool {
	if timezone == "" {
		timezone = "UTC"
	}
	return &ScheduleTool{
		service: service,
		tz:      timezone,
	}
}

// Name returns the tool name.
func (t *ScheduleTool) Name() string {
	return "schedule"
}

// Description returns the tool description.
func (t *ScheduleTool) Description() string {
	return `Create reminders and recurring automations from natural language. THIS IS THE PREFERRED TOOL FOR ALL SCHEDULING REQUESTS.

Use this tool when the user asks to be reminded of something, wants a recurring task, or describes a schedule. ALWAYS use this tool instead of the cron tool for user scheduling requests.

Examples of valid requests:
- "Remind me tomorrow at 9 AM to send the report"
- "Remind me Friday at 3 PM to call Sarah"
- "Remind me in 2 hours to check the server"
- "Every Monday at 9 AM, prepare my weekly brief"
- "Every day at 8 AM remind me to check my keys"

IMPORTANT: This tool requires a time specification. If the user says "remind me to do X" without a time, ask them WHEN they want to be reminded. Do NOT create a schedule without a time.

MOVING a reminder: when the user says move/change/postpone/shift/delay an existing reminder, pass the "reschedule" parameter describing the OLD reminder. The old item is cancelled and replaced — never duplicated. Example: user said "move my 9pm Chelsea reminder to 7:45" → reschedule="9pm Chelsea reminder", message="Remind me at 7:45pm to watch Chelsea".

The tool will parse the natural language and create the appropriate scheduled item. It returns a human-readable confirmation. The confirmation always states the EXACT stored time including minutes — quote it back verbatim, never round it.`
}

// Parameters returns the tool parameters schema.
func (t *ScheduleTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "The user's natural language scheduling request. Include the full original message.",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The reminder content or automation prompt. If not provided, derived from the message.",
			},
			"reschedule": map[string]interface{}{
				"type":        "string",
				"description": "MOVE an existing reminder instead of adding one: describe the old reminder (e.g. \"the 9pm Chelsea reminder\" or its item id). The old item is cancelled and replaced. ALWAYS use this when the user says move/change/postpone/shift a reminder.",
			},
		},
		"required": []string{"message"},
	}
}

// SetContext sets the current session context.
func (t *ScheduleTool) SetContext(channel, chatID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.channel = channel
	t.chatID = chatID
}

// Execute runs the tool with the given arguments.
// The timezone prefers the per-request device timezone carried on ctx (set by
// the chat handler from client metadata) and falls back to the tool default.
func (t *ScheduleTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	t.mu.RLock()
	channel := t.channel
	chatID := t.chatID
	tz := t.tz
	t.mu.RUnlock()
	if reqTZ := RequestTimezone(ctx); reqTZ != "" {
		tz = reqTZ
	}

	if channel == "" || chatID == "" {
		return ErrorResult("no session context. Use this tool in an active conversation.")
	}

	message, ok := args["message"].(string)
	if !ok || message == "" {
		return ErrorResult("message is required")
	}

	content, _ := args["content"].(string)
	if content == "" {
		content = extractReminderContent(message)
	}

	parsed, err := scheduled.ParseNaturalLanguage(message, time.Now(), tz)
	if err != nil {
		return ErrorResult(fmt.Sprintf("I couldn't understand the schedule. Please specify a time. For example: 'Remind me tomorrow at 9 AM to %s'", content))
	}

	item := &scheduled.ScheduledItem{
		Type:        scheduled.TypeReminder,
		Title:       parsed.Title,
		Description: message,
		State:       scheduled.StateScheduled,
		Timezone:    parsed.Timezone,
		Channel:     channel,
		ChatID:      chatID,
		Schedule:    parsed.Schedule,
		Action: scheduled.Action{
			Kind:    scheduled.ActionAgentTurn,
			Content: content,
		},
		DeliveryMode: scheduled.DeliverySmart,
		Source:       "user",
		CreatedBy:    "agent",
		MaxRetries:   3,
	}

	if parsed.IsRecurring {
		item.Type = scheduled.TypeAutomation
	}

	// Move, not copy: when rescheduling, the old item is cancelled first so
	// exactly one live item survives. A "moved" reminder that also still
	// fires at the old time is a bug, not a move.
	movedFrom := ""
	if rescheduleQuery, _ := args["reschedule"].(string); strings.TrimSpace(rescheduleQuery) != "" {
		old, note := cancelMatchingSchedule(t.service, strings.TrimSpace(rescheduleQuery))
		if old != nil {
			movedFrom = describeScheduleTime(old)
		} else if note != "" {
			// Service cannot cancel (minimal implementation): refuse to
			// create a likely duplicate rather than silently doubling.
			return ErrorResult(note)
		}
	}

	switch parsed.Schedule.Kind {
	case scheduled.ScheduleAt:
		item.NextRunAt = parsed.Schedule.At
	case scheduled.ScheduleCron:
		item.NextRunAt = scheduled.NextCronRun(parsed.Schedule.Expr, parsed.Timezone, time.Now())
	case scheduled.ScheduleEvery:
		next := time.Now().UTC().Add(parsed.Schedule.Every)
		item.NextRunAt = &next
	}

	// Dedupe: an active item with the same normalized content and same
	// schedule (cron expr or one-time title) is reused instead of doubled.
	// This prevents the "4× Monday brief" class of duplicates when the user
	// repeats a request or a retry re-fires the tool.
	if lister, ok := t.service.(interface {
		ListItems(scheduled.ItemType, scheduled.ItemState, int) ([]*scheduled.ScheduledItem, error)
	}); ok {
		if existing := findDuplicateSchedule(lister, item); existing != nil {
			return SilentResult("Already set — " + buildConfirmation(existing, parsed))
		}
	}

	if err := t.service.CreateItem(item); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to create schedule: %v", err))
	}

	confirm := buildConfirmation(item, parsed)
	if movedFrom != "" {
		confirm = fmt.Sprintf("Moved it — cancelled the old reminder (%s). %s", movedFrom, confirm)
	}
	return SilentResult(confirm)
}

// findDuplicateSchedule returns an active item matching the new one by
// normalized action content + schedule identity (cron expr for recurring,
// title+time for one-time). Nil when no duplicate.
func findDuplicateSchedule(lister interface {
	ListItems(scheduled.ItemType, scheduled.ItemState, int) ([]*scheduled.ScheduledItem, error)
}, item *scheduled.ScheduledItem) *scheduled.ScheduledItem {
	items, err := lister.ListItems("", scheduled.StateScheduled, 100)
	if err != nil {
		return nil
	}
	normContent := strings.ToLower(strings.TrimSpace(item.Action.Content))
	for _, it := range items {
		if it == nil || it.State != scheduled.StateScheduled {
			continue
		}
		if strings.ToLower(strings.TrimSpace(it.Action.Content)) != normContent {
			continue
		}
		if item.Schedule.Kind != it.Schedule.Kind {
			continue
		}
		if item.Schedule.Kind == scheduled.ScheduleCron && item.Schedule.Expr != it.Schedule.Expr {
			continue
		}
		return it
	}
	return nil
}

func extractReminderContent(message string) string {
	lower := strings.ToLower(message)

	prefixes := []string{
		"remind me to ",
		"remind me ",
		"create a reminder to ",
		"schedule a reminder to ",
		"set a reminder to ",
	}

	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			content := message[len(prefix):]

			if idx := strings.Index(content, " to "); idx > 0 {
				return strings.TrimSpace(content[idx+4:])
			}

			if idx := strings.Index(content, " at "); idx > 0 {
				rest := content[idx+4:]
				if idx2 := strings.Index(rest, " to "); idx2 > 0 {
					return strings.TrimSpace(rest[idx2+4:])
				}
			}

			if idx := strings.Index(content, " in "); idx > 0 {
				rest := content[idx+4:]
				if idx2 := strings.Index(rest, " to "); idx2 > 0 {
					return strings.TrimSpace(rest[idx2+4:])
				}
			}

			return strings.TrimSpace(content)
		}
	}

	lowerMsg := strings.ToLower(message)
	if idx := strings.Index(lowerMsg, ", "); idx > 0 {
		return strings.TrimSpace(message[idx+2:])
	}

	return message
}

func buildConfirmation(item *scheduled.ScheduledItem, parsed *scheduled.ParsedSchedule) string {
	tzLabel := ""
	if parsed.Timezone != "" && parsed.Timezone != "UTC" {
		tzLabel = fmt.Sprintf(" (%s)", parsed.Timezone)
	} else if parsed.Timezone == "UTC" {
		tzLabel = " (UTC)"
	}
	if parsed.IsRecurring {
		return fmt.Sprintf("Got it — I'll %s %s%s.", strings.ToLower(item.Title), formatScheduleForUser(parsed), tzLabel)
	}
	return fmt.Sprintf("Got it — I'll remind you %s%s to %s.", formatScheduleForUser(parsed), tzLabel, item.Action.Content)
}

func formatScheduleForUser(parsed *scheduled.ParsedSchedule) string {
	if parsed.IsRecurring {
		return fmt.Sprintf("every %s", strings.TrimPrefix(strings.TrimPrefix(parsed.Title, "Every "), "every "))
	}
	if parsed.IsOneTime {
		if t := parsed.Schedule.At; t != nil {
			// Minutes are load-bearing ("7:45 PM" vs "7 PM"). A
			// confirmation that drops them makes the model — and the
			// user — believe a different time was stored.
			return t.Format("Monday at 3:04 PM")
		}
	}
	return parsed.Title
}

// scheduleLister and scheduleCanceller are the optional service
// capabilities the reschedule flow needs. The production service
// implements both; minimal fakes may not.
type scheduleLister interface {
	ListItems(scheduled.ItemType, scheduled.ItemState, int) ([]*scheduled.ScheduledItem, error)
}

type scheduleCanceller interface {
	CancelItem(string) error
}

// cancelMatchingSchedule finds the live scheduled item best matching the
// user's description of the OLD reminder and cancels it. It returns the
// cancelled item (for the confirmation) or a non-empty note when the
// service cannot cancel — in which case the caller must NOT create the
// replacement, or the reminder would silently double.
func cancelMatchingSchedule(service ScheduleService, query string) (*scheduled.ScheduledItem, string) {
	lister, ok := service.(scheduleLister)
	if !ok {
		return nil, "I can't list existing reminders here, so I won't create a replacement that could duplicate one. Cancel the old reminder first, then ask again."
	}
	canceller, ok := service.(scheduleCanceller)
	if !ok {
		return nil, "I can't cancel the old reminder here, so I won't create a replacement that could duplicate it. Cancel the old reminder first, then ask again."
	}
	items, err := lister.ListItems("", scheduled.StateScheduled, 100)
	if err != nil || len(items) == 0 {
		return nil, ""
	}
	best := matchScheduleQuery(items, query)
	if best == nil {
		return nil, ""
	}
	if err := canceller.CancelItem(best.ID); err != nil {
		return nil, fmt.Sprintf("I couldn't cancel the old reminder (%v), so I didn't create a replacement.", err)
	}
	return best, ""
}

// describeScheduleTime renders when a stored item fires, for move
// confirmations. Always exact — see formatScheduleForUser.
func describeScheduleTime(item *scheduled.ScheduledItem) string {
	if item == nil {
		return "unknown time"
	}
	if item.Schedule.Kind == scheduled.ScheduleAt && item.Schedule.At != nil {
		return item.Schedule.At.Format("Monday at 3:04 PM")
	}
	if item.Title != "" {
		return item.Title
	}
	return "previous time"
}

// rescheduleStopwords are tokens ignored when matching an old reminder
// description: time expressions, politeness, and generic schedule nouns.
var rescheduleStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "my": true, "that": true, "this": true,
	"reminder": true, "remind": true, "reminds": true, "old": true, "previous": true,
	"move": true, "change": true, "shift": true, "postpone": true, "delay": true,
	"to": true, "at": true, "on": true, "for": true, "me": true, "it": true,
	"am": true, "pm": true, "o'clock": true, "oclock": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true,
	"friday": true, "saturday": true, "sunday": true, "today": true, "tonight": true,
}

// matchScheduleQuery scores live items against the user's description by
// shared significant words. Exact id match wins immediately. Nil when
// nothing scores — the caller then creates fresh rather than cancelling
// the wrong item.
func matchScheduleQuery(items []*scheduled.ScheduledItem, query string) *scheduled.ScheduledItem {
	q := strings.ToLower(strings.TrimSpace(query))
	var best *scheduled.ScheduledItem
	bestScore := 0
	for _, it := range items {
		if it == nil || it.State != scheduled.StateScheduled {
			continue
		}
		if it.ID == query || strings.EqualFold(it.ID, q) {
			return it
		}
		haystack := strings.ToLower(strings.TrimSpace(it.Title + " " + it.Description + " " + it.Action.Content))
		score := 0
		for _, w := range strings.Fields(q) {
			w = strings.Trim(w, ".,!?;:'\"()")
			if len(w) < 3 || rescheduleStopwords[w] {
				continue
			}
			if strings.Contains(haystack, w) {
				// Longer content words carry more signal than short ones.
				score += len(w)
			}
		}
		if score > bestScore {
			bestScore = score
			best = it
		}
	}
	// Require real signal: a single 3-letter overlap must not cancel
	// someone's reminder.
	if bestScore < 5 {
		return nil
	}
	return best
}
