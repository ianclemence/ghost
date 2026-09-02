package scheduled

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ParsedSchedule represents a parsed natural language schedule.
type ParsedSchedule struct {
	Schedule  Schedule
	Title     string
	Timezone  string
	IsRecurring bool
	IsOneTime   bool
}

// ParseNaturalLanguage parses a natural language schedule description.
// It returns a ParsedSchedule or an error if the input cannot be parsed.
func ParseNaturalLanguage(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, error) {
	if timezone == "" {
		timezone = "UTC"
	}

	input = strings.TrimSpace(strings.ToLower(input))

	// Try to parse recurring schedules first
	if schedule, ok := parseRecurringSchedule(input, referenceTime, timezone); ok {
		return schedule, nil
	}

	// Try to parse one-time schedules
	if schedule, ok := parseOneTimeSchedule(input, referenceTime, timezone); ok {
		return schedule, nil
	}

	return nil, fmt.Errorf("cannot parse schedule from: %s", input)
}

// parseRecurringSchedule parses recurring schedule patterns.
func parseRecurringSchedule(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, bool) {
	// "every day at 8 AM"
	everyDayRe := regexp.MustCompile(`every\s+day\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyDayRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * *", minute, hour),
			},
			Title:       fmt.Sprintf("Every day at %s", formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every Monday at 9 AM"
	weekdayMap := map[string]string{
		"monday": "1", "tuesday": "2", "wednesday": "3",
		"thursday": "4", "friday": "5", "saturday": "6", "sunday": "0",
	}
	everyWeekdayRe := regexp.MustCompile(`every\s+(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyWeekdayRe.FindStringSubmatch(input); len(matches) >= 4 {
		dayNum := weekdayMap[matches[1]]
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * %s", minute, hour, dayNum),
			},
			Title:       fmt.Sprintf("Every %s at %s", matches[1], formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every week at 9 AM"
	everyWeekRe := regexp.MustCompile(`every\s+week\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyWeekRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d * * 1", minute, hour), // Monday
			},
			Title:       fmt.Sprintf("Every week at %s", formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every month on the 1st at 9 AM"
	everyMonthRe := regexp.MustCompile(`every\s+month\s+(?:on\s+)?(?:the\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := everyMonthRe.FindStringSubmatch(input); len(matches) >= 4 {
		day, _ := strconv.Atoi(matches[1])
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleCron,
				Expr: fmt.Sprintf("%d %d %d * *", minute, hour, day),
			},
			Title:       fmt.Sprintf("Every month on the %d at %s", day, formatTimeDisplay(hour, minute)),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every N hours"
	everyNHoursRe := regexp.MustCompile(`every\s+(\d+)\s+hours?`)
	if matches := everyNHoursRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		interval := time.Duration(n) * time.Hour

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind:  ScheduleEvery,
				Every: interval,
			},
			Title:       fmt.Sprintf("Every %d hours", n),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	// "every N minutes"
	everyNMinutesRe := regexp.MustCompile(`every\s+(\d+)\s+minutes?`)
	if matches := everyNMinutesRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		interval := time.Duration(n) * time.Minute

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind:  ScheduleEvery,
				Every: interval,
			},
			Title:       fmt.Sprintf("Every %d minutes", n),
			Timezone:    timezone,
			IsRecurring: true,
		}, true
	}

	return nil, false
}

// parseOneTimeSchedule parses one-time schedule patterns.
func parseOneTimeSchedule(input string, referenceTime time.Time, timezone string) (*ParsedSchedule, bool) {
	// "tomorrow at 9 AM"
	tomorrowRe := regexp.MustCompile(`tomorrow\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := tomorrowRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		nextDay := referenceTime.AddDate(0, 0, 1)
		at := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, time.UTC)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Tomorrow at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "today at 3 PM"
	todayRe := regexp.MustCompile(`today\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := todayRe.FindStringSubmatch(input); len(matches) >= 3 {
		hour := parseHour(matches[1], matches[3])
		minute := 0
		if matches[2] != "" {
			minute, _ = strconv.Atoi(matches[2])
		}

		at := time.Date(referenceTime.Year(), referenceTime.Month(), referenceTime.Day(), hour, minute, 0, 0, time.UTC)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Today at %s", formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "Friday at 3 PM"
	weekdayMap := map[string]time.Weekday{
		"monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
		"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday, "sunday": time.Sunday,
	}
	weekdayRe := regexp.MustCompile(`(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)
	if matches := weekdayRe.FindStringSubmatch(input); len(matches) >= 4 {
		targetDay := weekdayMap[matches[1]]
		hour := parseHour(matches[2], matches[4])
		minute := 0
		if matches[3] != "" {
			minute, _ = strconv.Atoi(matches[3])
		}

		// Find next occurrence of this weekday
		daysUntil := (int(targetDay) - int(referenceTime.Weekday()) + 7) % 7
		if daysUntil == 0 {
			daysUntil = 7 // Next week if same day
		}
		nextDay := referenceTime.AddDate(0, 0, daysUntil)
		at := time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, time.UTC)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("Next %s at %s", matches[1], formatTimeDisplay(hour, minute)),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "in 2 hours"
	inHoursRe := regexp.MustCompile(`in\s+(\d+)\s+hours?`)
	if matches := inHoursRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		at := referenceTime.Add(time.Duration(n) * time.Hour)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("In %d hours", n),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	// "in 30 minutes"
	inMinutesRe := regexp.MustCompile(`in\s+(\d+)\s+minutes?`)
	if matches := inMinutesRe.FindStringSubmatch(input); len(matches) >= 2 {
		n, _ := strconv.Atoi(matches[1])
		at := referenceTime.Add(time.Duration(n) * time.Minute)

		return &ParsedSchedule{
			Schedule: Schedule{
				Kind: ScheduleAt,
				At:   &at,
			},
			Title:     fmt.Sprintf("In %d minutes", n),
			Timezone:  timezone,
			IsOneTime: true,
		}, true
	}

	return nil, false
}

// computeNextDaily computes the next daily occurrence at the given time.
func computeNextDaily(referenceTime time.Time, hour, minute int, timezone string) time.Time {
	// Simplified: return tomorrow at the specified time
	nextDay := referenceTime.AddDate(0, 0, 1)
	return time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), hour, minute, 0, 0, time.UTC)
}

// parseHour parses an hour string with AM/PM indicator.
func parseHour(hourStr, ampm string) int {
	hour, _ := strconv.Atoi(hourStr)
	if strings.ToLower(ampm) == "pm" && hour < 12 {
		hour += 12
	} else if strings.ToLower(ampm) == "am" && hour == 12 {
		hour = 0
	}
	return hour
}

// formatTime formats hour and minute into a human-readable string.
func formatTimeDisplay(hour, minute int) string {
	ampm := "AM"
	h := hour
	if hour >= 12 {
		ampm = "PM"
		if hour > 12 {
			h = hour - 12
		}
	} else if hour == 0 {
		h = 12
	}

	if minute == 0 {
		return fmt.Sprintf("%d %s", h, ampm)
	}
	return fmt.Sprintf("%d:%02d %s", h, minute, ampm)
}
