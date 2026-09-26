package heartbeat

import (
	"strings"
	"testing"
	"time"
)

const cadenceDoc = `# Periodic Tasks (Heartbeat)

Guardrails that always apply.

## Morning Routine (08:00, device timezone)

- [ ] one briefing

## Maintenance (Every 4 Hours)

- [ ] check temp

## Continuous Learning (Weekly)

- [ ] list skills

## Skill Health Check (Daily)

- [ ] doctor
`

// The whole point: one tick must not put the entire checklist in front of the
// model. Only the sections whose cadence is due may appear.
func TestDueContentRunsOnlyDueSections(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, loc)

	// Last model turn late yesterday: the morning window and the daily
	// section are due; the weekly section is not.
	got := DueContent(cadenceDoc, now, loc, time.Date(2026, 9, 25, 22, 0, 0, 0, loc))
	if !strings.Contains(got, "Guardrails that always apply") {
		t.Fatalf("the preamble must always ship: %q", got)
	}
	if !strings.Contains(got, "Morning Routine") {
		t.Fatalf("a due daily window must ship: %q", got)
	}
	if !strings.Contains(got, "Skill Health Check") {
		t.Fatalf("a due daily section must ship: %q", got)
	}
	if strings.Contains(got, "Continuous Learning") {
		t.Fatalf("the weekly section is not due: %q", got)
	}

	// Last turn two hours ago: the 4-hourly section is not due yet.
	recent := DueContent(cadenceDoc, now, loc, time.Date(2026, 9, 26, 7, 0, 0, 0, loc))
	if strings.Contains(recent, "Maintenance (Every 4 Hours)") {
		t.Fatalf("the 4-hourly section is not due two hours after a run: %q", recent)
	}
	if !strings.Contains(recent, "Morning Routine") {
		t.Fatalf("the morning window is still due: %q", recent)
	}

	// Everything already handled: no model call at all.
	quiet := DueContent(cadenceDoc, now, loc, time.Date(2026, 9, 26, 8, 30, 0, 0, loc))
	if strings.TrimSpace(quiet) != "" {
		t.Fatalf("a tick with nothing due must return empty, got %q", quiet)
	}

	// The 4-hourly section comes due on its own cadence.
	if out := DueContent(cadenceDoc, now, loc, time.Date(2026, 9, 26, 4, 30, 0, 0, loc)); !strings.Contains(out, "Maintenance (Every 4 Hours)") {
		t.Fatalf("the 4-hourly section must ship when due: %q", out)
	}
}

// A file with no cadence headings must never be silently disabled.
func TestDueContentWithoutHeadingsAlwaysShips(t *testing.T) {
	doc := "Just some prose with no headings at all."
	got := DueContent(doc, time.Now(), time.UTC, time.Now())
	if got != doc {
		t.Fatalf("no-heading file must ship whole, got %q", got)
	}
}

// An unrecognised cadence is treated as due, so new prose is never skipped.
func TestDueContentUnknownCadenceIsDue(t *testing.T) {
	doc := "## Something New (whenever you feel like it)\n\n- [ ] do the thing\n"
	got := DueContent(doc, time.Now(), time.UTC, time.Now().Add(-time.Minute))
	if !strings.Contains(got, "do the thing") {
		t.Fatalf("unknown cadence must ship, got %q", got)
	}
}

func TestLastRunRoundTrip(t *testing.T) {
	ws := t.TempDir()
	if !LoadLastRun(ws).IsZero() {
		t.Fatal("no recorded run reads as zero time")
	}
	now := time.Now().UTC().Truncate(time.Second)
	MarkRan(ws, now)
	if got := LoadLastRun(ws); !got.Equal(now) {
		t.Fatalf("last run = %v, want %v", got, now)
	}
}

// The cadence gate is only correct if a run is recorded even when it reports
// nothing — a silent tick is the normal case, and an unrecorded run would make
// every section due again on the next tick.
func TestCadenceGateStopsRepeatingAfterASilentRun(t *testing.T) {
	ws := t.TempDir()
	loc := time.UTC
	now := time.Date(2026, 9, 26, 9, 0, 0, 0, loc)

	// Nothing recorded yet: the morning window and the daily section are due.
	first := DueContent(cadenceDoc, now, loc, LoadLastRun(ws))
	if !strings.Contains(first, "Morning Routine") {
		t.Fatalf("the first tick of the day must run the morning window: %q", first)
	}
	// The run is recorded even though it reported nothing.
	MarkRan(ws, now)
	second := DueContent(cadenceDoc, now.Add(30*time.Minute), loc, LoadLastRun(ws))
	if strings.TrimSpace(second) != "" {
		t.Fatalf("the next tick has nothing due but produced %q", second)
	}
}
