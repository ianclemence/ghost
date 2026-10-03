package routines

import (
	"strings"
	"testing"
	"time"
)

func TestRoutineIntentFull(t *testing.T) {
	in := ParseIntent("Every Monday at 9 remind me to review my finances", time.Now(), "Asia/Bangkok")
	if !in.IsRoutine || in.NeedsClarification {
		t.Fatalf("must be a complete routine: %+v", in)
	}
	if !strings.Contains(strings.ToLower(in.Task), "review my finances") {
		t.Fatalf("task wrong: %q", in.Task)
	}
	if in.Timezone != "Asia/Bangkok" {
		t.Fatal("timezone must carry through")
	}
}

func TestRoutineIntentNoTask(t *testing.T) {
	in := ParseIntent("Every Monday at 9", time.Now(), "UTC")
	if !in.IsRoutine || !in.NeedsClarification {
		t.Fatalf("must clarify: %+v", in)
	}
}

func TestOneTimeNotRoutine(t *testing.T) {
	for _, text := range []string{
		"Remind me tomorrow",
		"Remind me tomorrow at 9 to buy milk",
		"remind me in an hour to call",
		"at 9pm remind me to lock up",
	} {
		if in := ParseIntent(text, time.Now(), "UTC"); in.IsRoutine {
			t.Fatalf("%q must stay a one-time reminder", text)
		}
	}
}

func TestNonRoutineChat(t *testing.T) {
	for _, text := range []string{"what's the weather", "remember I like tea", "hello"} {
		if in := ParseIntent(text, time.Now(), "UTC"); in.IsRoutine {
			t.Fatalf("%q is not a routine", text)
		}
	}
}

func TestRoutineExtractsTaskFromDaypart(t *testing.T) {
	in := ParseIntent("Every Monday morning remind me to review my finances", time.Now(), "UTC")
	if !in.IsRoutine || in.NeedsClarification {
		t.Fatalf("must be routine: %+v", in)
	}
	if !strings.Contains(strings.ToLower(in.Task), "review my finances") {
		t.Fatalf("task must be the activity, got %q", in.Task)
	}
	if strings.Contains(strings.ToLower(in.Task), "monday") {
		t.Fatalf("schedule clause leaked into task: %q", in.Task)
	}
	if in.ScheduleText != "every Monday at 9 AM" {
		t.Fatalf("schedule text must resolve morning to a time, got %q", in.ScheduleText)
	}
}

func TestTimeOfDayWordLeavesTheTask(t *testing.T) {
	now := time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	in := ParseIntent("Every day at 7:30 in the morning, check the Bangkok weather and tell me if I need an umbrella.", now, "Asia/Bangkok")
	if in.Task != "check the Bangkok weather and tell me if I need an umbrella" {
		t.Fatalf("task = %q", in.Task)
	}
}
