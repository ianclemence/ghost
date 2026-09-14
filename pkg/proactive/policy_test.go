package proactive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsWhenMissing(t *testing.T) {
	p := Load(t.TempDir())
	if p.QuietStart != 23*60 || p.QuietEnd != 8*60 {
		t.Fatalf("default quiet must be 23:00-08:00, got %d-%d", p.QuietStart, p.QuietEnd)
	}
	if p.MaxPushesPerDay != 5 {
		t.Fatalf("default budget must be 5, got %d", p.MaxPushesPerDay)
	}
}

func TestLoadCustomValues(t *testing.T) {
	dir := t.TempDir()
	body := "# P\n- `quiet_hours: 22:00-07:00`\n- `max_pushes_per_day: 3`\n" +
		"- `cooldown_per_topic: 2h`\n- `dedupe_window: 12h`\n" +
		"- `morning_briefing: 07:30`\n- `evening_reflection: 21:45`\n"
	if err := os.WriteFile(filepath.Join(dir, "PROACTIVE_PREFERENCES.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	p := Load(dir)
	if p.QuietStart != 22*60 || p.QuietEnd != 7*60 {
		t.Fatalf("custom quiet not parsed: %+v", p)
	}
	if p.MaxPushesPerDay != 3 || p.CooldownPerTopic != 2*time.Hour || p.DedupeWindow != 12*time.Hour {
		t.Fatalf("custom budgets not parsed: %+v", p)
	}
	if p.MorningBriefing != "07:30" || p.EveningReflection != "21:45" {
		t.Fatalf("custom cadence not parsed: %+v", p)
	}
}

func TestInQuietHoursWrapsMidnight(t *testing.T) {
	p := defaults()
	at := func(h, m int) time.Time {
		return time.Date(2026, 9, 13, h, m, 0, 0, time.UTC)
	}
	for _, hm := range [][2]int{{23, 0}, {2, 30}, {7, 59}} {
		if !InQuietHours(at(hm[0], hm[1]), time.UTC, p) {
			t.Fatalf("%02d:%02d must be quiet", hm[0], hm[1])
		}
	}
	for _, hm := range [][2]int{{8, 0}, {12, 0}, {22, 59}} {
		if InQuietHours(at(hm[0], hm[1]), time.UTC, p) {
			t.Fatalf("%02d:%02d must not be quiet", hm[0], hm[1])
		}
	}
}

func TestReflectionDueOncePerDay(t *testing.T) {
	dir := t.TempDir()
	p := defaults()
	loc := time.UTC
	evening := time.Date(2026, 9, 13, 22, 5, 0, 0, time.UTC)
	if !ReflectionDue(dir, evening, loc, p) {
		t.Fatal("reflection must be due inside the evening window")
	}
	morning := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	if ReflectionDue(dir, morning, loc, p) {
		t.Fatal("reflection must not be due in the morning")
	}
	MarkReflected(dir, evening, loc)
	if ReflectionDue(dir, evening, loc, p) {
		t.Fatal("reflection must not repeat the same day")
	}
	next := time.Date(2026, 9, 14, 22, 5, 0, 0, time.UTC)
	if !ReflectionDue(dir, next, loc, p) {
		t.Fatal("reflection must be due again the next evening")
	}
}
