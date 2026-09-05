package routines

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

func openTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// :memory: databases are per-connection; a single connection keeps
	// nested queries (List → Get) on the same database.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatal(err)
	}
	svc, err := New(db, store)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestCreateGetList(t *testing.T) {
	svc := openTestService(t)
	r, err := svc.Create("ghost-1", "owner-1", "Weekly Brief", "prepare my weekly brief", "Asia/Bangkok",
		scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 9 * * MON"}, []string{"calendar.read"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusActive || r.Timezone != "Asia/Bangkok" || len(r.AllowedCapabilities) != 1 {
		t.Fatalf("bad routine: %+v", r)
	}
	got, err := svc.Get(r.ID)
	if err != nil || got.Instruction != "prepare my weekly brief" || got.GhostID != "ghost-1" {
		t.Fatalf("get wrong: %+v %v", got, err)
	}
	list := svc.List("ghost-1", 10)
	if len(list) != 1 {
		t.Fatal("list must return the routine")
	}
	if len(svc.List("ghost-2", 10)) != 0 {
		t.Fatal("routines are ghost-scoped")
	}
}

func TestValidation(t *testing.T) {
	svc := openTestService(t)
	if _, err := svc.Create("g", "o", "", "do x", "UTC", scheduled.Schedule{}, nil); err == nil {
		t.Fatal("name required")
	}
	if _, err := svc.Create("g", "o", "R", "do x", "Not/Zone", scheduled.Schedule{}, nil); err == nil {
		t.Fatal("timezone validated")
	}
}

func TestPauseResumeCancel(t *testing.T) {
	svc := openTestService(t)
	r, _ := svc.Create("g", "o", "R", "do x", "UTC", scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, nil)
	if err := svc.Pause(r.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(r.ID)
	if got.Status != StatusPaused {
		t.Fatal("must pause")
	}
	if err := svc.Resume(r.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel(r.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.Get(r.ID)
	if got.Status != StatusCancelled {
		t.Fatal("must cancel")
	}
	if err := svc.Resume(r.ID); err == nil {
		t.Fatal("finished routine cannot resume")
	}
}

func TestRunSuccess(t *testing.T) {
	svc := openTestService(t)
	r, _ := svc.Create("g", "o", "R", "do x", "UTC", scheduled.Schedule{}, nil)
	out, err := svc.Run(context.Background(), r.ID, "exec-1", func(ctx context.Context, r *Routine) RunOutcome {
		return RunOutcome{Completion: product.CompletionSuccess, Message: "done"}
	})
	if err != nil || out.Completion != product.CompletionSuccess {
		t.Fatalf("run failed: %+v %v", out, err)
	}
	// Duplicate execution key rejected (exactly-once per occurrence).
	if _, err := svc.Run(context.Background(), r.ID, "exec-1", func(ctx context.Context, r *Routine) RunOutcome {
		return RunOutcome{Completion: product.CompletionSuccess}
	}); err == nil {
		t.Fatal("duplicate execution must be rejected")
	}
}

func TestRunWaitingPreservesRunnability(t *testing.T) {
	svc := openTestService(t)
	r, _ := svc.Create("g", "o", "R", "do x", "UTC", scheduled.Schedule{}, []string{"calendar.read"})
	out, err := svc.Run(context.Background(), r.ID, "exec-2", func(ctx context.Context, r *Routine) RunOutcome {
		return RunOutcome{Completion: product.CompletionWaitingForPermission, WaitingOn: "perm-1"}
	})
	if err != nil || out.WaitingOn != "perm-1" {
		t.Fatalf("waiting run wrong: %+v %v", out, err)
	}
	got, _ := svc.Get(r.ID)
	if got.Status == StatusCompleted || got.Status == StatusFailed {
		t.Fatal("waiting run must stay runnable, not terminal")
	}
}

func TestRunFailure(t *testing.T) {
	svc := openTestService(t)
	r, _ := svc.Create("g", "o", "R", "do x", "UTC", scheduled.Schedule{}, nil)
	out, err := svc.Run(context.Background(), r.ID, "exec-3", func(ctx context.Context, r *Routine) RunOutcome {
		return RunOutcome{Completion: product.CompletionFailed, Message: "boom"}
	})
	if err != nil || out.Completion != product.CompletionFailed {
		t.Fatalf("failure run wrong: %+v %v", out, err)
	}
}
