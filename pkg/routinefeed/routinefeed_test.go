package routinefeed

import (
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

func at(t time.Time) *time.Time { return &t }

func TestFromRoutine(t *testing.T) {
	next := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	r := &routines.Routine{
		ID:           "routine-abc",
		Name:         "Weekly brief",
		Instruction:  "Prepare my weekly brief",
		Status:       routines.StatusActive,
		ScheduleKind: "cron",
		ScheduleExpr: "0 9 * * 1-5",
		NextRun:      at(next),
	}

	got := FromRoutine(r)
	if got.ID != "routine-abc" || got.Title != "Weekly brief" || got.What != "Prepare my weekly brief" {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if got.Kind != KindRoutine {
		t.Errorf("kind = %q, want routine", got.Kind)
	}
	if got.State != StateActive {
		t.Errorf("state = %q, want active", got.State)
	}
	if got.Source != "routine" {
		t.Errorf("source = %q, want routine", got.Source)
	}
	if got.Schedule == "" || got.Schedule == "Manual" {
		t.Errorf("schedule should be humanized, got %q", got.Schedule)
	}
}

func TestFromRoutineNil(t *testing.T) {
	if got := FromRoutine(nil); got.ID != "" {
		t.Errorf("nil routine should yield zero Item, got %+v", got)
	}
}

func TestInferKind(t *testing.T) {
	every := time.Hour
	cases := []struct {
		name string
		item *scheduled.ScheduledItem
		want Kind
	}{
		{
			name: "reminder",
			item: &scheduled.ScheduledItem{Type: scheduled.TypeReminder, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt}},
			want: KindReminder,
		},
		{
			name: "recurring automation",
			item: &scheduled.ScheduledItem{Type: scheduled.TypeAutomation, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: every}},
			want: KindAutomation,
		},
		{
			name: "one-time automation",
			item: &scheduled.ScheduledItem{Type: scheduled.TypeAutomation, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt}},
			want: KindAutomation,
		},
		{
			name: "explicit task",
			item: &scheduled.ScheduledItem{Type: scheduled.TypeTask, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleNone}},
			want: KindTask,
		},
		{
			name: "event is a reminder",
			item: &scheduled.ScheduledItem{Type: scheduled.TypeEvent, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt}},
			want: KindReminder,
		},
		{
			name: "untyped recurring falls back to routine",
			item: &scheduled.ScheduledItem{Type: "unknown", Schedule: scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 9 * * *"}},
			want: KindRoutine,
		},
		{
			name: "untyped one-off falls back to task",
			item: &scheduled.ScheduledItem{Type: "unknown", Schedule: scheduled.Schedule{Kind: scheduled.ScheduleNone}},
			want: KindTask,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := inferKind(tc.item)
			if got != tc.want {
				t.Errorf("kind = %q, want %q", got, tc.want)
			}
			if reason == "" {
				t.Errorf("reason must be non-empty for auditability")
			}
		})
	}
}

func TestNormalizeStates(t *testing.T) {
	routineCases := map[routines.Status]State{
		routines.StatusActive:    StateActive,
		routines.StatusPaused:    StatePaused,
		routines.StatusWaiting:   StateWaiting,
		routines.StatusCompleted: StateDone,
		routines.StatusFailed:    StateFailed,
		routines.StatusCancelled: StateCancelled,
		routines.Status("weird"): StateActive,
	}
	for in, want := range routineCases {
		if got := normalizeRoutineState(in); got != want {
			t.Errorf("routine %q -> %q, want %q", in, got, want)
		}
	}

	itemCases := map[scheduled.ItemState]State{
		scheduled.StateScheduled: StateActive,
		scheduled.StateDue:       StateActive,
		scheduled.StateRunning:   StateActive,
		scheduled.StatePaused:    StatePaused,
		scheduled.StateCompleted: StateDone,
		scheduled.StateFailed:    StateFailed,
		scheduled.StateMissed:    StateFailed,
		scheduled.StateCancelled: StateCancelled,
		scheduled.ItemState(""):  StateActive,
	}
	for in, want := range itemCases {
		if got := normalizeItemState(in); got != want {
			t.Errorf("item %q -> %q, want %q", in, got, want)
		}
	}
}

func TestListDeduplicatesRoutineSourcedItems(t *testing.T) {
	now := time.Now()
	routinesList := []*routines.Routine{
		{ID: "routine-1", Name: "Brief", Instruction: "do it", Status: routines.StatusActive, ScheduleKind: "every", ScheduleEverySecs: 3600, CreatedAt: now},
	}
	scheduledList := []*scheduled.ScheduledItem{
		// Same ID as the routine, plus a raw routine-sourced row with a
		// different ID that must still be suppressed.
		{ID: "routine-1", Title: "dup", Source: "routine", CreatedAt: now},
		{ID: "routine-2", Title: "other routine row", Source: "routine", CreatedAt: now},
		{ID: "rem-1", Title: "Standup", Type: scheduled.TypeReminder, State: scheduled.StateScheduled, Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt}, CreatedAt: now},
	}
	got := List(routinesList, scheduledList)
	if len(got) != 2 {
		t.Fatalf("want 2 things (1 routine + 1 reminder), got %d: %+v", len(got), got)
	}
	ids := map[string]bool{}
	for _, th := range got {
		if ids[th.ID] {
			t.Errorf("duplicate id %q in output", th.ID)
		}
		ids[th.ID] = true
	}
	if !ids["routine-1"] || !ids["rem-1"] {
		t.Errorf("expected routine-1 and rem-1, got %v", ids)
	}
}

func TestListOrdersActiveFirstThenByNextRun(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	later := base.Add(48 * time.Hour)
	sooner := base.Add(time.Hour)

	got := List(
		[]*routines.Routine{
			{ID: "paused", Name: "p", Status: routines.StatusPaused, ScheduleKind: "every", ScheduleEverySecs: 60, CreatedAt: base},
			{ID: "active-late", Name: "l", Status: routines.StatusActive, ScheduleKind: "at", ScheduleExpr: later.Format(time.RFC3339), NextRun: at(later), CreatedAt: base},
			{ID: "active-soon", Name: "s", Status: routines.StatusActive, ScheduleKind: "at", ScheduleExpr: sooner.Format(time.RFC3339), NextRun: at(sooner), CreatedAt: base},
		},
		nil,
	)
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].ID != "active-soon" || got[1].ID != "active-late" || got[2].ID != "paused" {
		t.Errorf("order = %s, %s, %s; want active-soon, active-late, paused", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestListDeterministicOnTies(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	in := []*routines.Routine{
		{ID: "b", Name: "b", Status: routines.StatusActive, ScheduleKind: "every", ScheduleEverySecs: 60, CreatedAt: base},
		{ID: "a", Name: "a", Status: routines.StatusActive, ScheduleKind: "every", ScheduleEverySecs: 60, CreatedAt: base},
	}
	first := List(in, nil)
	second := List(in, nil)
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("non-deterministic order at %d: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
	if first[0].ID != "a" {
		t.Errorf("tie-break should be by ID: got %q first", first[0].ID)
	}
}

func TestSummary(t *testing.T) {
	cases := []struct {
		name string
		th   Item
		want string
	}{
		{"waiting", Item{Schedule: "Every day", State: StateWaiting}, "Every day · waiting for you"},
		{"paused", Item{Schedule: "Every day", State: StatePaused}, "Every day · paused"},
		{"ran", Item{Schedule: "Every day", State: StateActive, RunCount: 3}, "Every day · ran 3×"},
		{"plain", Item{Schedule: "Every day", State: StateActive}, "Every day"},
		{"manual", Item{State: StateActive}, "Manual"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summary(tc.th); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHumanScheduleReusesSchedulerHumanizer(t *testing.T) {
	// The console, phone, and this package must agree. The scheduler's
	// humanizer is the single source of truth for cron phrasing.
	r := &routines.Routine{ScheduleKind: "cron", ScheduleExpr: "0 9 * * 1-5"}
	got := humanRoutineSchedule(r)
	wantItem := scheduled.ScheduledItem{Schedule: scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 9 * * 1-5"}}
	want := wantItem.HumanSchedule()
	if got != want {
		t.Errorf("cron phrasing drifted: got %q, want %q", got, want)
	}
}

func TestHumanScheduleEvery(t *testing.T) {
	r := &routines.Routine{ScheduleKind: "every", ScheduleEverySecs: 3600}
	if got := humanRoutineSchedule(r); got == "" || got == "Manual" {
		t.Errorf("every schedule should humanize, got %q", got)
	}
}

func TestHumanScheduleUnknownFallsBackToManual(t *testing.T) {
	r := &routines.Routine{ScheduleKind: "nonsense"}
	if got := humanRoutineSchedule(r); got != "Manual" {
		t.Errorf("unknown kind should be Manual, got %q", got)
	}
}

func TestFromRoutineStripsExecutionPhrasing(t *testing.T) {
	r := &routines.Routine{
		ID: "routine-x", Name: "Prepare my brief",
		Instruction: "remind me to prepare my brief",
		Status:      routines.StatusActive, ScheduleKind: "every", ScheduleEverySecs: 60,
	}
	got := FromRoutine(r)
	if got.What != "Prepare my brief" {
		t.Errorf("what = %q, want clean task without 'remind me to'", got.What)
	}
	if got.Title != "Prepare my brief" {
		t.Errorf("title = %q", got.Title)
	}
}

func TestDisplayInstructionFallbacks(t *testing.T) {
	if got := displayInstruction("Please send the report"); got != "Send the report" {
		t.Errorf("got %q", got)
	}
	if got := displayInstruction(""); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
	if got := displayInstruction("water the plants"); got != "Water the plants" {
		t.Errorf("got %q", got)
	}
}
