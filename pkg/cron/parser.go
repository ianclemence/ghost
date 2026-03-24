package cron

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ParseSchedule converts a natural language string or raw cron expression into a standard cron expression.
func ParseSchedule(schedule string) string {
	schedule = strings.TrimSpace(strings.ToLower(schedule))

	if schedule == "" {
		return ""
	}

	// 1. Check if it's already a valid cron expression (5 parts separated by spaces)
	parts := strings.Fields(schedule)
	if len(parts) == 5 {
		// Passthrough for raw cron (e.g. "0 7 * * *")
		return schedule
	}

	// 2. Parse "Every X min/hour"
	if strings.HasPrefix(schedule, "every ") {
		return parseEvery(schedule)
	}

	// 3. Parse specific days and times (e.g., "Monday 9am", "Saturday 8am")
	dayMap := map[string]string{
		"sunday":    "0",
		"monday":    "1",
		"tuesday":   "2",
		"wednesday": "3",
		"thursday":  "4",
		"friday":    "5",
		"saturday":  "6",
	}

	for day, dayInt := range dayMap {
		if strings.HasPrefix(schedule, day) {
			timeStr := strings.TrimSpace(strings.TrimPrefix(schedule, day))
			hour, minute := parseTime(timeStr)
			if hour != -1 {
				return fmt.Sprintf("%v %v * * %v", minute, hour, dayInt)
			}
		}
	}

	// 4. Parse "1st of month, 9am"
	if strings.HasPrefix(schedule, "1st") || strings.HasPrefix(schedule, "first") {
		timeRegex := regexp.MustCompile(`(?:of month,?\s*)?(.*)`)
		matches := timeRegex.FindStringSubmatch(strings.TrimPrefix(strings.TrimPrefix(schedule, "1st"), "first"))
		if len(matches) > 1 {
			hour, minute := parseTime(matches[1])
			if hour != -1 {
				return fmt.Sprintf("%v %v 1 * *", minute, hour)
			}
		}
	}

	// 5. Parse multiple times (e.g., "8am, 5pm", "9am, 1pm, 5pm")
	if strings.Contains(schedule, ",") {
		timeParts := strings.Split(schedule, ",")
		hours := []string{}
		minutes := "0" // assume 0 for minutes when multiple times are provided for simplicity, or handle complex mins

		for _, p := range timeParts {
			h, m := parseTime(strings.TrimSpace(p))
			if h != -1 {
				hours = append(hours, strconv.Itoa(h))
				if m != 0 {
					minutes = strconv.Itoa(m)
				}
			}
		}

		if len(hours) > 0 {
			return fmt.Sprintf("%v %s * * *", minutes, strings.Join(hours, ","))
		}
	}

	// 6. Parse single time (e.g., "7am", "10:30pm", "midnight")
	hour, minute := parseTime(schedule)
	if hour != -1 {
		return fmt.Sprintf("%v %v * * *", minute, hour)
	}

	// Fallback to passthrough (though invalid, cronService will handle error)
	return schedule
}

// parseEvery parses statements like "every 30 min" or "every 2 hours"
func parseEvery(schedule string) string {
	schedule = strings.TrimPrefix(schedule, "every ")
	
	valRegex := regexp.MustCompile(`^(\d+)\s*(min|minute|minutes|hr|hour|hours)$`)
	matches := valRegex.FindStringSubmatch(schedule)
	
	if len(matches) > 2 {
		val := matches[1]
		unit := matches[2]
		
		if strings.HasPrefix(unit, "min") {
			return fmt.Sprintf("*/%s * * * *", val)
		} else if strings.HasPrefix(unit, "h") {
			return fmt.Sprintf("0 */%s * * *", val)
		}
	}

	return ""
}

// parseTime handles "7am", "10pm", "10:30pm", "midnight", "noon"
// Returns hour, minute. Returns -1 for hour if parsing fails.
func parseTime(t string) (int, int) {
	t = strings.TrimSpace(t)
	if t == "midnight" {
		return 0, 0
	}
	if t == "noon" {
		return 12, 0
	}

	timeRegex := regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$`)
	matches := timeRegex.FindStringSubmatch(t)

	if len(matches) > 1 {
		hourStr := matches[1]
		minStr := matches[2]
		ampm := matches[3]

		hour, _ := strconv.Atoi(hourStr)
		minute := 0
		if minStr != "" {
			minute, _ = strconv.Atoi(minStr)
		}

		if ampm == "pm" && hour < 12 {
			hour += 12
		} else if ampm == "am" && hour == 12 {
			hour = 0
		}

		return hour, minute
	}

	return -1, 0
}
