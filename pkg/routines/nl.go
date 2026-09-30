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

// ParseIntent extracts routine intent deterministically (no LLM).
//
// The schedule is found by scheduled.ParseRecurrence, which understands
// weekdays, weekends, several named days, dayparts and either order of day and
// time. Whatever is left of the sentence once those words are taken out is the
// task, so "remind me every weekday at 8am to take my vitamins" and "at 8am
// every weekday remind me to take my vitamins" both mean the same thing.
func ParseIntent(text string, now time.Time, timezone string) Intent {
	if timezone == "" {
		timezone = "UTC"
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return Intent{}
	}
	rec, ok := scheduled.ParseRecurrence(trimmed, now, timezone)
	if !ok {
		// One-time patterns ("remind me tomorrow at 9") are reminders, and
		// anything else is not a schedule at all.
		return Intent{}
	}
	task := extractTask(scheduled.RemoveSpans(trimmed, rec.Spans))
	if task == "" {
		return Intent{IsRoutine: true, NeedsClarification: true,
			Schedule: rec.Parsed.Schedule, ScheduleText: rec.Clause, Timezone: timezone}
	}
	return Intent{IsRoutine: true, Task: task,
		Schedule: rec.Parsed.Schedule, ScheduleText: rec.Clause, Timezone: timezone}
}

// extractTask turns the sentence left after the schedule words were removed
// into the task. Leading reminder phrasing is normalized away.
func extractTask(rest string) string {
	rest = strings.Trim(strings.TrimSpace(rest), " .,;:-")
	lower := strings.ToLower(rest)
	// Peel leading conversational connectives and filler that precede the
	// actual task. "Also remind me to X", "and can you X", "then X" all
	// mean X; leaving the connective produced titles like "Also remind me
	// to send a status update".
	connectives := []string{"also ", "and also ", "and ", "then ", "plus "}
	changed := true
	for changed {
		changed = false
		for _, c := range connectives {
			if strings.HasPrefix(lower, c) {
				rest = strings.TrimSpace(rest[len(c):])
				lower = strings.ToLower(rest)
				changed = true
			}
		}
	}
	for _, prefix := range []string{"remind me to ", "remind me ", "please ", "can you ", "could you ", "would you ", "to "} {
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
	// Collapse internal whitespace left by clause removal ("Also remind me
	//   to send" -> "Also remind me to send").
	rest = strings.Join(strings.Fields(rest), " ")
	if len(rest) < 3 {
		return ""
	}
	// What is left may only be a stray timing hint ("around lunchtime"), which
	// is not something to do.
	lower = strings.ToLower(rest)
	for _, p := range []string{"around ", "about ", "at ", "by ", "before ", "after ", "sometime ", "roughly "} {
		if strings.HasPrefix(lower, p) && len(strings.Fields(rest)) <= 3 {
			return ""
		}
	}
	return rest
}
