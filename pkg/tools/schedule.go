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

The tool will parse the natural language and create the appropriate scheduled item. It returns a human-readable confirmation.`
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
func (t *ScheduleTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	t.mu.RLock()
	channel := t.channel
	chatID := t.chatID
	tz := t.tz
	t.mu.RUnlock()

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

	switch parsed.Schedule.Kind {
	case scheduled.ScheduleAt:
		item.NextRunAt = parsed.Schedule.At
	case scheduled.ScheduleCron:
		item.NextRunAt = scheduled.NextCronRun(parsed.Schedule.Expr, parsed.Timezone, time.Now())
	case scheduled.ScheduleEvery:
		next := time.Now().UTC().Add(parsed.Schedule.Every)
		item.NextRunAt = &next
	}

	if err := t.service.CreateItem(item); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to create schedule: %v", err))
	}

	confirm := buildConfirmation(item, parsed)
	return SilentResult(confirm)
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
			return t.Format("Monday at 3 PM")
		}
	}
	return parsed.Title
}
