package agent

import (
	"database/sql"
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

// PollProactive delivers through the gate and suppresses repeats.
func TestPollProactiveDeliversOnce(t *testing.T) {
	al, rsvc, ssvc, store := signalLoop(t)
	id := mkRoutine(t, rsvc, "briefing")
	item, _ := ssvc.GetItem(id)
	item.State = scheduled.StateFailed
	item.LastError = "boom"
	if err := store.Update(item); err != nil {
		t.Fatal(err)
	}
	ch, unsub := al.bus.SubscribeOutbound("test", true, 8)
	defer unsub()
	if err := al.state.SetLastActiveSession("telegram", "123"); err != nil {
		t.Fatal(err)
	}
	// Warm affinity so the floor doesn't gate the test signal.
	al.affect = al.affect.Turn(0.9, 0.5, 0.9, false, time.Now())
	if n := al.PollProactive(); n != 1 {
		t.Fatalf("must deliver exactly one notice, got %d", n)
	}
	select {
	case m := <-ch:
		if !strings.Contains(m.Content, "briefing") {
			t.Fatalf("delivered wrong content: %q", m.Content)
		}
	default:
		t.Fatal("notice must arrive on the bus")
	}
	if n := al.PollProactive(); n != 0 {
		t.Fatalf("repeat poll must be a gated no-op, got %d", n)
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
