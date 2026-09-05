package scheduled

import (
	"testing"
	"time"
)

func TestDefaultPolicies(t *testing.T) {
	if DefaultMissedPolicy(TypeReminder) != MissedNotify {
		t.Fatal("reminders must default to notify")
	}
	if DefaultMissedPolicy(TypeAutomation) != MissedNext {
		t.Fatal("automations must default to next (no double brief)")
	}
}

func TestClassifyMissed(t *testing.T) {
	now := time.Now()
	// One-shot 1h late -> run once.
	d := ClassifyMissed(TypeTask, "", now.Add(-time.Hour), now)
	if !d.ShouldRun || d.Policy != MissedRunOnce {
		t.Fatalf("fresh miss should run once: %+v", d)
	}
	// One-shot 48h late -> notify, never execute stale.
	d = ClassifyMissed(TypeTask, "", now.Add(-48*time.Hour), now)
	if d.ShouldRun || !d.NotifyUser {
		t.Fatalf("stale miss must notify, not run: %+v", d)
	}
	// Automation missed -> next, no catch-up duplicates.
	d = ClassifyMissed(TypeAutomation, "", now.Add(-time.Hour), now)
	if d.ShouldRun || d.Policy != MissedNext {
		t.Fatalf("automation must wait for next: %+v", d)
	}
	// Reminder missed -> notify.
	d = ClassifyMissed(TypeReminder, "", now.Add(-time.Hour), now)
	if !d.NotifyUser || d.ShouldRun {
		t.Fatalf("reminder must notify: %+v", d)
	}
	// Explicit skip honored.
	d = ClassifyMissed(TypeReminder, MissedSkip, now.Add(-time.Hour), now)
	if d.NotifyUser || d.ShouldRun {
		t.Fatalf("explicit skip honored: %+v", d)
	}
}

func TestResolveTimezone(t *testing.T) {
	if got := ResolveTimezone("Asia/Bangkok", "Europe/London", "UTC"); got != "Asia/Bangkok" {
		t.Fatalf("explicit wins, got %s", got)
	}
	if got := ResolveTimezone("", "Europe/London", "UTC"); got != "Europe/London" {
		t.Fatalf("user config second, got %s", got)
	}
	if got := ResolveTimezone("", "", ""); got != "UTC" {
		t.Fatalf("fallback UTC, got %s", got)
	}
	if got := ResolveTimezone("Not/AZone", "", ""); got != "UTC" {
		t.Fatalf("invalid zone falls back, got %s", got)
	}
}

func TestInZoneDisplay(t *testing.T) {
	utc := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	bkk := InZone(utc, "Asia/Bangkok")
	if bkk.Hour() != 19 {
		t.Fatalf("Bangkok is UTC+7, got %v", bkk)
	}
	// Invalid zone never fails.
	_ = InZone(utc, "bogus")
}

func TestExecutionKeyDeterministic(t *testing.T) {
	at := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	a := ExecutionKey("item-1", at)
	b := ExecutionKey("item-1", at)
	if a == "" || a != b {
		t.Fatal("key must be deterministic")
	}
	if c := ExecutionKey("item-1", at.Add(time.Minute)); a == c {
		t.Fatal("different occurrences must differ")
	}
	if c := ExecutionKey("item-2", at); a == c {
		t.Fatal("different items must differ")
	}
}
