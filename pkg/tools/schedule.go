package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
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

Use this tool only when the user asks to be reminded of something or to automate something recurring. Mentioning a habit or a schedule is not a request — narration ("every morning I feel groggy") must never create a schedule. ALWAYS use this tool instead of the cron tool for user scheduling requests.

Examples of valid requests:
- "Remind me tomorrow at 9 AM to send the report"
- "Remind me Friday at 3 PM to call Sarah"
- "Remind me in 2 hours to check the server"
- "Every Monday at 9 AM, prepare my weekly brief"
- "Every day at 8 AM remind me to check my keys"

IMPORTANT: This tool requires a time specification. If the user says "remind me to do X" without a time, ask them WHEN they want to be reminded. Do NOT create a schedule without a time.

MOVING a reminder: when the user says move/change/postpone/shift/delay an existing reminder, pass the "reschedule" parameter describing the OLD reminder. The old item is cancelled and replaced — never duplicated. Example: user said "move my 9pm Chelsea reminder to 7:45" → reschedule="9pm Chelsea reminder", message="Remind me at 7:45pm to watch Chelsea".

The tool will parse the natural language and create the appropriate scheduled item. It returns a human-readable confirmation. The confirmation always states the EXACT stored time including minutes — quote it back verbatim, never round it.

CHECKING what is scheduled: pass action="list" when the user asks what's scheduled, what reminders are pending, or to check whether reminders already fired ("check my reminders", "what's on today", "did X go off"). It returns pending items plus recently completed and failed ones with exact times — one-time reminders stay visible as completed after they fire. No message is needed for a list.`
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
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"create", "list"},
				"description": "create (default): store the request in message. list: return what is scheduled (pending, recently completed, failed) — message is not needed.",
			},
		},
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
	// Read side first: listing needs no session context and no message,
	// so "check my reminders" works wherever the turn runs.
	if action, _ := args["action"].(string); strings.EqualFold(strings.TrimSpace(action), "list") {
		return t.listSchedules()
	}

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
		// Never echo the content back into the example: content often
		// carries its own time phrase, and the splice produced garbage
		// like "tomorrow at 9 AM to at 9pm tonight …". Name the accepted
		// shapes instead — the model retries from them.
		return ErrorResult("I couldn't understand when that should happen. Say the time with the day — for example \"tomorrow at 9 AM to call Sam\", \"at 8:30 PM today\", or \"tonight at 9\".")
	}

	// The owner cares WHAT Ghost does, not the schedule restated as a name.
	// Prefer a title derived from the action content; fall back to the parsed
	// schedule phrase only when there is no content to name it after. The
	// schedule itself is carried separately and rendered as its own sentence.
	title := derivedItemTitle(content)
	if title == "" {
		title = parsed.Title
	}

	item := &scheduled.ScheduledItem{
		Type:        scheduled.TypeReminder,
		Title:       title,
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

// listSchedules returns what is on the schedule: pending items first,
// then recently completed and failed ones. Fired one-shots are retained
// as completed (scheduled.Service.handleSuccess), so "check my reminders"
// has a real answer — what will still fire, and what already did — instead
// of "I can't read the schedule back".
func (t *ScheduleTool) listSchedules() *ToolResult {
	lister, ok := t.service.(scheduleLister)
	if !ok {
		return ErrorResult("I can't read the schedule back from this store.")
	}
	pending, err := lister.ListItems("", scheduled.StateScheduled, 100)
	if err != nil {
		return ErrorResult("I couldn't read the schedule right now. Please try again.")
	}
	completed, err := lister.ListItems("", scheduled.StateCompleted, 10)
	if err != nil {
		completed = nil
	}
	failed, err := lister.ListItems("", scheduled.StateFailed, 10)
	if err != nil {
		failed = nil
	}
	if len(pending) == 0 && len(completed) == 0 && len(failed) == 0 {
		return SilentResult("Nothing is scheduled — no pending, completed, or failed items on file.")
	}

	// Soonest first for pending; most recent first for history.
	sort.SliceStable(pending, func(i, j int) bool {
		a, b := pending[i].NextRunAt, pending[j].NextRunAt
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return a.Before(*b)
		}
	})
	sort.SliceStable(completed, func(i, j int) bool {
		a, b := completed[i].LastRunAt, completed[j].LastRunAt
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		default:
			return a.After(*b)
		}
	})

	var b strings.Builder
	if len(pending) == 0 {
		b.WriteString("No pending schedules.\n")
	} else {
		fmt.Fprintf(&b, "%d pending:\n", len(pending))
		for _, it := range pending {
			fmt.Fprintf(&b, "- %s — %s\n", scheduleHeadline(it), scheduleWhen(it))
		}
	}
	if len(completed) > 0 {
		b.WriteString("Recently completed:\n")
		for _, it := range completed {
			fmt.Fprintf(&b, "- %s — fired %s\n", scheduleHeadline(it), storedTimeLabel(it.LastRunAt, it.Timezone))
		}
	}
	if len(failed) > 0 {
		b.WriteString("Failed:\n")
		for _, it := range failed {
			msg := strings.TrimSpace(it.LastError)
			if i := strings.IndexByte(msg, '\n'); i >= 0 {
				msg = strings.TrimSpace(msg[:i])
			}
			if msg == "" {
				msg = "no error detail"
			}
			fmt.Fprintf(&b, "- %s — last attempt failed: %s\n", scheduleHeadline(it), msg)
		}
	}
	return SilentResult(strings.TrimSpace(b.String()))
}

// scheduleHeadline names an item for a listing.
func scheduleHeadline(it *scheduled.ScheduledItem) string {
	if title := strings.TrimSpace(it.Title); title != "" {
		return title
	}
	if d := strings.TrimSpace(it.Description); d != "" {
		return d
	}
	return "untitled item"
}

// scheduleWhen renders when a stored item will fire, exactly (minutes are
// load-bearing — see formatScheduleForUser), in the item's own timezone.
func scheduleWhen(it *scheduled.ScheduledItem) string {
	switch it.Schedule.Kind {
	case scheduled.ScheduleAt:
		if it.Schedule.At == nil {
			return "time unknown"
		}
		return storedTimeLabel(it.Schedule.At, it.Timezone)
	case scheduled.ScheduleCron:
		if it.Timezone != "" && it.Timezone != "UTC" {
			return fmt.Sprintf("recurring (cron %s) — %s", it.Schedule.Expr, it.Timezone)
		}
		return "recurring (cron " + it.Schedule.Expr + ")"
	case scheduled.ScheduleEvery:
		return "every " + it.Schedule.Every.String()
	default:
		return "no schedule"
	}
}

// storedTimeLabel renders an instant in the given IANA timezone with the
// zone named, so a stored UTC row still reads as the owner's local time.
func storedTimeLabel(t *time.Time, tz string) string {
	if t == nil {
		return "unknown time"
	}
	when := *t
	label := ""
	if tz != "" && tz != "UTC" {
		if loc, err := time.LoadLocation(tz); err == nil {
			when = when.In(loc)
			label = " (" + tz + ")"
		} else {
			label = " (UTC)"
		}
	} else {
		label = " (UTC)"
	}
	return when.Format("Mon Jan 2, 3:04 PM") + label
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

// derivedItemTitle builds a short, human title for a scheduled item from the
// action content ("Prepare my weekly design review brief"). It strips a
// leading directive verb filler and caps length so the Things list reads as
// a task name, not a paragraph. Returns "" when content is too thin to name
// anything meaningful, so the caller can fall back to the schedule phrase.
func derivedItemTitle(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	// Drop a trailing schedule clause if the content accidentally includes it.
	lower := strings.ToLower(s)
	for _, cut := range []string{", every ", " every monday", " every tuesday", " every wednesday", " every thursday", " every friday", " every saturday", " every sunday", " every day", " every week"} {
		if idx := strings.Index(lower, cut); idx > 0 {
			s = strings.TrimSpace(s[:idx])
			break
		}
	}
	if len(s) < 3 {
		return ""
	}
	// Capitalize the first letter for presentation.
	runes := []rune(s)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] = runes[0] - 32
	}
	out := string(runes)
	const max = 80
	if len(out) > max {
		out = strings.TrimSpace(out[:max]) + "\u2026"
	}
	return out
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

			return stripSchedulePhrase(content)
		}
	}

	lowerMsg := strings.ToLower(message)
	if idx := strings.Index(lowerMsg, ", "); idx > 0 {
		return strings.TrimSpace(message[idx+2:])
	}

	return stripSchedulePhrase(message)
}

// stripSchedulePhrase cuts a leading schedule phrase and its connector
// from reminder content so the title names the ACTION, not the clock:
// "at 9pm tonight about dinner with Jas" → "Dinner with Jas". Only
// day-anchored phrases are cut (time-then-day, day-then-time, a
// parenthetical date, then a to/that/about/for connector) — a bare
// "9 am meeting" is content, not a schedule. Returns the input unchanged
// when nothing safe can be cut.
var (
	leadingTimeDayRe   = regexp.MustCompile(`(?i)^(?:at\s+)?\d{1,2}(?::\d{2})?\s*(?:am|pm)?\s+(?:tonight|today|tomorrow)\b[\s,]*`)
	leadingDayTimeRe   = regexp.MustCompile(`(?i)^(?:tonight|today|tomorrow|next\s+\w+day|[a-z]{3,9}day)\s+(?:at\s+)?\d{1,2}(?::\d{2})?\s*(?:am|pm)?\b[\s,]*`)
	leadingDateParenRe = regexp.MustCompile(`^\([^)]*\)\s*`)
	leadingConnectorRe = regexp.MustCompile(`(?i)^(?:to|that|about|for)\s+`)
)

func stripSchedulePhrase(content string) string {
	s := strings.TrimSpace(content)
	for i := 0; i < 6 && s != ""; i++ {
		var cut string
		switch {
		case leadingTimeDayRe.MatchString(s):
			cut = leadingTimeDayRe.FindString(s)
		case leadingDayTimeRe.MatchString(s):
			cut = leadingDayTimeRe.FindString(s)
		case leadingDateParenRe.MatchString(s):
			cut = leadingDateParenRe.FindString(s)
		case leadingConnectorRe.MatchString(s):
			cut = leadingConnectorRe.FindString(s)
		default:
			return s
		}
		next := strings.TrimSpace(s[len(cut):])
		if next == s { // no progress — never spin on the same text
			return s
		}
		s = next
	}
	return s
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
