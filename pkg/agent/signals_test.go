package agent

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

func signalLoop(t *testing.T) (*AgentLoop, *routines.Service, *scheduled.Service, *scheduled.Store) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatal(err)
	}
	rsvc, err := routines.New(db, store)
	if err != nil {
		t.Fatal(err)
	}
	ssvc := scheduled.NewService(store, &scheduled.SimpleEventBus{}, nil)
	al := pinTestLoop(t, nil)
	al.SetRoutineSignals(rsvc, ssvc)
	return al, rsvc, ssvc, store
}

func mkRoutine(t *testing.T, rsvc *routines.Service, name string) string {
	t.Helper()
	r, err := rsvc.Create("ghost-local", "owner", name, "do "+name,
		"UTC", scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func TestNoRoutinesNoNotices(t *testing.T) {
	al, _, _, _ := signalLoop(t)
	if got := al.ScanNotices(); len(got) != 0 {
		t.Fatalf("no routines must mean no notices: %+v", got)
	}
}

func TestHealthyRoutineSilent(t *testing.T) {
	al, rsvc, _, _ := signalLoop(t)
	mkRoutine(t, rsvc, "briefing")
	if got := al.ScanNotices(); len(got) != 0 {
		t.Fatalf("healthy routine must stay silent: %+v", got)
	}
}

func TestFailedRoutineNoticeCarriesReason(t *testing.T) {
	al, rsvc, ssvc, store := signalLoop(t)
	id := mkRoutine(t, rsvc, "briefing")
	item, err := ssvc.GetItem(id)
	if err != nil {
		t.Fatal(err)
	}
	item.State = scheduled.StateFailed
	item.LastError = "calendar API 500"
	if err := store.Update(item); err != nil {
		t.Fatal(err)
	}
	got := al.ScanNotices()
	if len(got) != 1 {
		t.Fatalf("failed routine must propose exactly one notice: %+v", got)
	}
	n := got[0]
	if n.Priority < 7 || n.Confidence < 0.6 {
		t.Fatalf("failure must clear the gate: %+v", n)
	}
	for _, want := range []string{"briefing", "failed", "calendar API 500", "retry"} {
		if !strings.Contains(n.Message, want) {
			t.Fatalf("notice must carry its reason (%q): %q", want, n.Message)
		}
	}
}

func TestWaitingRoutineNotice(t *testing.T) {
	al, rsvc, _, store := signalLoop(t)
	id := mkRoutine(t, rsvc, "deploy")
	now := time.Now()
	if err := store.RecordExecution(&scheduled.ExecutionRecord{
		ItemID: id, ExecutionID: id + ":w1",
		ScheduledAt: now, StartedAt: now, Status: "waiting",
	}); err != nil {
		t.Fatal(err)
	}
	got := al.ScanNotices()
	if len(got) != 1 {
		t.Fatalf("waiting routine must propose a notice: %+v", got)
	}
	if !strings.Contains(got[0].Message, "approval") {
		t.Fatalf("wait notice must name approval: %q", got[0].Message)
	}
}

// PollProactive holds what can wait for the morning digest and suppresses
// repeats. A failed routine is not urgent, so it joins the digest instead of
// interrupting; polling twice queues it once.
func TestPollProactiveDeliversOnce(t *testing.T) {
	al, rsvc, ssvc, store := signalLoop(t)
	id := mkRoutine(t, rsvc, "briefing")
	item, _ := ssvc.GetItem(id)
	item.State = scheduled.StateFailed
	item.LastError = "boom"
	if err := store.Update(item); err != nil {
		t.Fatal(err)
	}
	if err := al.state.SetLastActiveSession("telegram", "123"); err != nil {
		t.Fatal(err)
	}
	// Pin clock assumptions: disable quiet hours for this workspace so
	// delivery does not depend on wall-clock time (defaults hold
	// 23:00–08:00, which made this test fail only at night).
	if err := os.WriteFile(filepath.Join(al.workspace, "PROACTIVE_PREFERENCES.md"),
		[]byte("# Prefs\n\n`quiet_hours: 00:00 - 00:00`\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Warm affinity so the floor doesn't gate the test signal.
	al.affect = al.affect.Turn(0.9, 0.5, 0.9, false, time.Now())
	if n := al.PollProactive(); n != 0 {
		t.Fatalf("a non-urgent failure must wait for the morning digest, got %d", n)
	}
	pending := al.attentionQueue().Pending()
	if len(pending) != 1 {
		t.Fatalf("exactly one digest item must wait, got %+v", pending)
	}
	if !strings.Contains(pending[0].Line, "briefing") {
		t.Fatalf("digest item must carry its reason: %q", pending[0].Line)
	}
	if n := al.PollProactive(); n != 0 {
		t.Fatalf("repeat poll must be a no-op, got %d", n)
	}
	if len(al.attentionQueue().Pending()) != 1 {
		t.Fatalf("repeat poll must not duplicate the digest item")
	}
}

// Distant affinity holds non-urgent outreach.
func TestPollProactiveAffinityFloor(t *testing.T) {
	al, rsvc, ssvc, store := signalLoop(t)
	id := mkRoutine(t, rsvc, "briefing")
	item, _ := ssvc.GetItem(id)
	item.State = scheduled.StateFailed
	if err := store.Update(item); err != nil {
		t.Fatal(err)
	}
	if err := al.state.SetLastActiveSession("telegram", "123"); err != nil {
		t.Fatal(err)
	}
	// Affinity starts at 0.5 neutral — force distant.
	al.affect.Affinity = 0.1
	if n := al.PollProactive(); n != 0 {
		t.Fatalf("distant affinity must hold non-urgent notices, got %d", n)
	}
}
