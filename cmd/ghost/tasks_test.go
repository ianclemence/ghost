package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	_ "modernc.org/sqlite"
)

func testRoutineService(t *testing.T) *routines.Service {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "tasks.db")+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatal(err)
	}
	svc, err := routines.New(db, store)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestResolveRoutineID(t *testing.T) {
	svc := testRoutineService(t)
	r, err := svc.Create("g", "o", "Water", "drink water", "UTC",
		scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveRoutineID(svc, "g", r.ID); got != r.ID {
		t.Fatalf("full id must resolve, got %q", got)
	}
	if got := resolveRoutineID(svc, "g", r.ID[:8]); got != r.ID {
		t.Fatalf("unique prefix must resolve, got %q", got)
	}
	if got := resolveRoutineID(svc, "g", "nope"); got != "" {
		t.Fatalf("unknown ref must not resolve, got %q", got)
	}
}

func TestRoundDur(t *testing.T) {
	if roundDur(30*time.Minute) != "30m" {
		t.Fatalf("minutes wrong: %q", roundDur(30*time.Minute))
	}
	if roundDur(5*time.Hour) != "5h" {
		t.Fatalf("hours wrong: %q", roundDur(5*time.Hour))
	}
	if roundDur(72*time.Hour) != "3d" {
		t.Fatalf("days wrong: %q", roundDur(72*time.Hour))
	}
}
