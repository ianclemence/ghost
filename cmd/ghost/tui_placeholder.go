package main

import "time"

// composerHint is what the terminal's empty composer says. It follows the part
// of the day and changes from one day to the next, in the same words the phone
// app uses, so Ghost sounds like one voice wherever you open it. Every line is
// short enough to sit on one row of a narrow terminal.
func composerHint(now time.Time) string {
	morning := []string{"What's the plan today?", "What are we starting with?", "Anything to set up for today?"}
	afternoon := []string{"What needs doing today?", "Need anything looked up?", "What should I remember?"}
	evening := []string{"How did today go?", "Anything for tomorrow?", "What's still on your mind?"}
	night := []string{"Can't sleep? Tell me.", "Still up? Tell me.", "Something on your mind?"}
	h := now.Hour()
	var pool []string
	switch {
	case h >= 5 && h < 11:
		pool = morning
	case h >= 11 && h < 17:
		pool = afternoon
	case h >= 17 && h < 22:
		pool = evening
	default:
		pool = night
	}
	return pool[now.YearDay()%len(pool)]
}
