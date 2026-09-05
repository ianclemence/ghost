package routines

// Natural-language routine intent: "Every Monday at 9 remind me to review
// my finances" must become a durable routine WITHOUT the user learning
// /v1/routines — and WITHOUT a second scheduler or LLM-driven scheduler
// internals. The existing scheduled.ParseNaturalLanguage owns schedule
// parsing; this file owns the routine-vs-reminder distinction and task
// extraction.
//
// Rules (honest ambiguity handling):
//   - recurring pattern + task  → routine proposal (confirm once, then create)
//   - recurring pattern, no task → clarify ("Every Monday at 9" → what happens?)
//   - one-time pattern ("remind me tomorrow", "at 9pm") → NOT a routine;
//     the existing one-shot reminder path owns it.
//   - reminders vs routines stay distinct: recurrence is the divider.

import (
	"regexp"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Intent is a parsed routine request.
type Intent struct {
	IsRoutine          bool
	Task               string
	Schedule           scheduled.Schedule
	ScheduleText       string // human schedule clause ("every Monday at 9")
	Timezone           string
	NeedsClarification bool // recurring but no task found
}

var recurRE = regexp.MustCompile(`(?i)\bevery\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday|morning|evening|night|afternoon|day|weekday|week|month|day)(\s+(at\s+\d{1,2}(:\d{2})?\s*(am|pm)?|morning|evening|afternoon|night))?`)
var dailyRE = regexp.MustCompile(`(?i)\b(daily|weekly|monthly|every day)(\s+(at\s+\d{1,2}(:\d{2})?\s*(am|pm)?|morning|evening|afternoon|night))?`)
var onetimeRE = regexp.MustCompile(`(?i)\b(tomorrow|today|tonight|next week|in an? hour|in \d+ (minutes?|hours?|days?)|at \d{1,2}(:\d{2})?\s*(am|pm)?\b)`)

// ParseIntent extracts routine intent deterministically (no LLM).
func ParseIntent(text string, now time.Time, timezone string) Intent {
	if timezone == "" {
		timezone = "UTC"
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Intent{}
	}
	// One-time patterns win: "remind me tomorrow (at 9)" is a reminder,
	// even if it contains a time.
	if m := onetimeRE.FindString(trimmed); m != "" && !isRecurring(trimmed) {
		return Intent{}
	}
	clause := recurringClause(trimmed)
	if clause == "" {
		return Intent{}
	}
	// Strip the task from the ORIGINAL clause ("Every Monday morning …"),
	// before normalization rewrites "morning" to "at 8" (which would not
	// match the user's text).
	task := extractTask(trimmed, clause)
	// Dayparts are times ("morning" = 8 AM) for the schedule parser.
	clause = normalizeDayparts(clause)
	parsed, err := scheduled.ParseNaturalLanguage(clause, now, timezone)
	if err != nil || !parsed.IsRecurring {
		// Fallback: treat explicit "every X" as weekly/daily intent only
		// when the schedule parser agrees it's recurring. Otherwise the
		// request is too ambiguous for a durable routine.
		return Intent{IsRoutine: true, NeedsClarification: true, ScheduleText: clause, Timezone: timezone}
	}
	if task == "" {
		return Intent{IsRoutine: true, NeedsClarification: true,
			Schedule: parsed.Schedule, ScheduleText: clause, Timezone: timezone}
	}
	return Intent{IsRoutine: true, Task: task,
		Schedule: parsed.Schedule, ScheduleText: clause, Timezone: timezone}
}

func isRecurring(text string) bool {
	return recurRE.MatchString(text) || dailyRE.MatchString(text)
}

// normalizeDayparts maps dayparts to explicit times so the schedule
// parser sees "every Monday at 8" instead of "every Monday morning".
func normalizeDayparts(clause string) string {
	lower := strings.ToLower(clause)
	replacements := map[string]string{
		"morning": "at 8", "afternoon": "at 14", "evening": "at 18", "night": "at 21",
	}
	for from, to := range replacements {
		if strings.HasSuffix(lower, " "+from) {
			return clause[:len(clause)-len(from)] + to
		}
	}
	return clause
}

func recurringClause(text string) string {
	if m := recurRE.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	if m := dailyRE.FindString(text); m != "" {
		return strings.TrimSpace(m)
	}
	return ""
}

// extractTask removes the schedule clause; the remainder is the task.
// Leading reminder phrasing is normalized but preserved as instruction.
func extractTask(text, clause string) string {
	rest := strings.TrimSpace(strings.Replace(text, clause, " ", 1))
	rest = strings.Trim(rest, " .,;")
	lower := strings.ToLower(rest)
	for _, prefix := range []string{"remind me to ", "remind me ", "please ", "can you ", "could you "} {
		if strings.HasPrefix(lower, prefix) {
			rest = strings.TrimSpace(rest[len(prefix):])
			lower = strings.ToLower(rest)
		}
	}
	// "to review my finances" → "review my finances" for the title, but
	// keep full instruction for execution.
	if strings.HasPrefix(lower, "to ") && len(rest) > 3 {
		rest = strings.TrimSpace(rest[3:])
	}
	if len(rest) < 3 {
		return ""
	}
	return rest
}
