package main

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/things"
	_ "modernc.org/sqlite"
)

// TestBuildThingsFeedMergesRoutinesAndScheduled exercises the real write
// path (routineService + scheduledService) and the real read path
// (buildThingsFeed). It is the end-to-end guarantee behind /v1/things:
// one owner-visible list, routines represented once, no second authority.
func TestBuildThingsFeedMergesRoutinesAndScheduled(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	prevDB, prevGhost, prevWS := apiDB, substrateGhost, apiWorkspaceDir
	t.Cleanup(func() { apiDB, substrateGhost, apiWorkspaceDir = prevDB, prevGhost, prevWS })
	apiDB = db
	substrateGhost = "ghost-test"
	apiWorkspaceDir = t.TempDir()

	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	svc := scheduled.NewService(store, nil, nil)

	// A routine: recurring product instruction with a sidecar.
	routineSvc, err := routineService()
	if err != nil {
		t.Fatalf("routineService: %v", err)
	}
	if _, err := routineSvc.Create("ghost-test", "owner", "Weekly brief", "Prepare my weekly brief", "UTC",
		scheduled.Schedule{Kind: scheduled.ScheduleCron, Expr: "0 9 * * 1-5"}, nil); err != nil {
		t.Fatalf("create routine: %v", err)
	}

	// A non-routine scheduled reminder.
	at := time.Now().UTC().Add(2 * time.Hour)
	if err := svc.CreateItem(&scheduled.ScheduledItem{
		ID:       "rem-1",
		Type:     scheduled.TypeReminder,
		Title:    "Stand up",
		State:    scheduled.StateScheduled,
		Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &at},
		Action:   scheduled.Action{Kind: scheduled.ActionMessage, Content: "Stand up"},
		Source:   "user",
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}

	feed := buildThingsFeed(svc)
	if len(feed) != 2 {
		t.Fatalf("want 2 things (routine + reminder), got %d: %+v", len(feed), feed)
	}

	var sawRoutine, sawReminder bool
	for _, th := range feed {
		switch th.Kind {
		case things.KindRoutine:
			sawRoutine = true
			if th.Title != "Weekly brief" || th.What != "Prepare my weekly brief" {
				t.Errorf("routine thing wrong: %+v", th)
			}
			if th.Schedule == "" || th.Schedule == "Manual" {
				t.Errorf("routine schedule should be humanized: %q", th.Schedule)
			}
		case things.KindReminder:
			sawReminder = true
			if th.Title != "Stand up" {
				t.Errorf("reminder thing wrong: %+v", th)
			}
		}
	}
	if !sawRoutine || !sawReminder {
		t.Fatalf("feed missing routine or reminder: routine=%v reminder=%v", sawRoutine, sawReminder)
	}
}

// A routine must appear exactly once even though it is also a scheduled row.
// This is the regression guard for the duplicate-row bug the split UI invites.
func TestBuildThingsFeedNoDuplicateRoutine(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	prevDB, prevGhost, prevWS := apiDB, substrateGhost, apiWorkspaceDir
	t.Cleanup(func() { apiDB, substrateGhost, apiWorkspaceDir = prevDB, prevGhost, prevWS })
	apiDB = db
	substrateGhost = "ghost-test"
	apiWorkspaceDir = t.TempDir()

	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	svc := scheduled.NewService(store, nil, nil)

	routineSvc, err := routineService()
	if err != nil {
		t.Fatalf("routineService: %v", err)
	}
	r, err := routineSvc.Create("ghost-test", "owner", "Daily sync", "Sync notes", "UTC",
		scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: 24 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}

	feed := buildThingsFeed(svc)
	count := 0
	for _, th := range feed {
		if th.ID == r.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("routine %s appeared %d times, want exactly 1", r.ID, count)
	}
}

// A missing scheduled backend must not blank the feed; routines are read
// independently so a partially-available Ghost still shows what it can.
func TestBuildThingsFeedDegradesWithoutScheduler(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "ghost.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	prevDB, prevGhost, prevWS := apiDB, substrateGhost, apiWorkspaceDir
	t.Cleanup(func() { apiDB, substrateGhost, apiWorkspaceDir = prevDB, prevGhost, prevWS })
	apiDB = db
	substrateGhost = "ghost-test"
	apiWorkspaceDir = t.TempDir()

	store := scheduled.NewStore(db)
	if err := store.InitSchema(); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	routineSvc, err := routines.New(db, store)
	if err != nil {
		t.Fatalf("routines.New: %v", err)
	}
	if _, err := routineSvc.Create("ghost-test", "owner", "Keep", "Keep doing it", "UTC",
		scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: time.Hour}, nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	// nil scheduler service: must still return the routine.
	feed := buildThingsFeed(nil)
	if len(feed) != 1 || feed[0].Kind != things.KindRoutine {
		t.Fatalf("want only the routine with nil scheduler, got %+v", feed)
	}
}
