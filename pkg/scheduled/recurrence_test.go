package scheduled

import (
	"strings"
	"testing"
	"time"
)

var refTime = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func TestRecurrenceShapesPeopleActuallySay(t *testing.T) {
	cases := []struct {
		in, cron, clause string
	}{
		{"every weekday at 8am", "0 8 * * 1-5", "every weekday at 8 AM"},
		{"remind me on weekdays at 7:30 to run", "30 7 * * 1-5", "every weekday at 7:30 AM"},
		{"at 8am every weekday", "0 8 * * 1-5", "every weekday at 8 AM"},
		{"every weekend at 10am", "0 10 * * 0,6", "every weekend at 10 AM"},
		{"every Monday and Thursday at 7pm", "0 19 * * 1,4", "every Monday and Thursday at 7 PM"},
		{"Mondays at 9", "0 9 * * 1", "every Monday at 9 AM"},
		{"on Mondays and Wednesdays at 6pm", "0 18 * * 1,3", "every Monday and Wednesday at 6 PM"},
		{"every mon, wed & fri at 6am", "0 6 * * 1,3,5", "every Monday, Wednesday and Friday at 6 AM"},
		{"monday to Friday at 9am", "0 9 * * 1,2,3,4,5", "every Monday to Friday at 9 AM"},
		{"every morning", "0 9 * * *", "every day at 9 AM"},
		{"every evening", "0 19 * * *", "every day at 7 PM"},
		{"daily at 6:15 am", "15 6 * * *", "every day at 6:15 AM"},
		{"8pm every day", "0 20 * * *", "every day at 8 PM"},
		{"every day", "0 9 * * *", "every day at 9 AM"},
		{"every Friday", "0 9 * * 5", "every Friday at 9 AM"},
		{"every week at 10am", "0 10 * * 1", "every week at 10 AM"},
		{"monthly", "0 9 1 * *", "every month on the 1 at 9 AM"},
		{"every month on the 15th at 2:30 pm", "30 14 15 * *", "every month on the 15 at 2:30 PM"},
	}
	for _, c := range cases {
		r, ok := ParseRecurrence(c.in, refTime, "UTC")
		if !ok {
			t.Errorf("%q must be understood as recurring", c.in)
			continue
		}
		if r.Parsed.Schedule.Kind != ScheduleCron || r.Parsed.Schedule.Expr != c.cron {
			t.Errorf("%q -> %q, want %q", c.in, r.Parsed.Schedule.Expr, c.cron)
		}
		if r.Clause != c.clause {
			t.Errorf("%q clause %q, want %q", c.in, r.Clause, c.clause)
		}
	}
}

func TestRecurrenceIntervals(t *testing.T) {
	cases := []struct {
		in    string
		every time.Duration
	}{
		{"every 2 hours", 2 * time.Hour},
		{"every 30 minutes", 30 * time.Minute},
		{"every hour", time.Hour},
		{"hourly", time.Hour},
		{"every 3 days", 72 * time.Hour},
		{"every other day", 48 * time.Hour},
		{"every 2 weeks", 14 * 24 * time.Hour},
	}
	for _, c := range cases {
		r, ok := ParseRecurrence(c.in, refTime, "UTC")
		if !ok || r.Parsed.Schedule.Kind != ScheduleEvery || r.Parsed.Schedule.Every != c.every {
			t.Errorf("%q -> %+v", c.in, r)
		}
	}
}

// One-off phrasings must not turn into recurring schedules.
func TestOneOffPhrasesAreNotRecurring(t *testing.T) {
	for _, in := range []string{
		"remind me tomorrow at 9", "at 9 on Friday", "on Friday at 5pm", "next Monday", "this weekend",
		"remind me on monday to call", "the weekday is fine", "at 8pm tonight", "in 2 hours",
		"I sat there", "every so often I forget",
	} {
		if r, ok := ParseRecurrence(in, refTime, "UTC"); ok {
			t.Errorf("%q is not recurring, got %q", in, r.Clause)
		}
	}
}

func TestRemoveSpansLeavesTheTask(t *testing.T) {
	r, _ := ParseRecurrence("Remind me every weekday at 8am to take my vitamins", refTime, "UTC")
	if got := RemoveSpans("Remind me every weekday at 8am to take my vitamins", r.Spans); got != "Remind me to take my vitamins" {
		t.Fatalf("got %q", got)
	}
	r, _ = ParseRecurrence("at 8am every weekday remind me to stretch", refTime, "UTC")
	if got := RemoveSpans("at 8am every weekday remind me to stretch", r.Spans); !strings.Contains(got, "remind me to stretch") || strings.Contains(strings.ToLower(got), "weekday") {
		t.Fatalf("got %q", got)
	}
}
