package cron

import (
	"path/filepath"
	"testing"
)

func TestPerProfileIsolation(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	}, nil)

	every := int64(60_000)

	cs.AddJobWithOptions("bot-a job1", CronSchedule{Kind: "every", EveryMS: &every}, "msg1", true, "mobile", "default", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("bot-b job1", CronSchedule{Kind: "every", EveryMS: &every}, "msg2", true, "mobile", "default", nil, nil, false, "bot-b")
	cs.AddJobWithOptions("bot-a job2", CronSchedule{Kind: "every", EveryMS: &every}, "msg3", true, "mobile", "default", nil, nil, false, "bot-a")

	botAJobs := cs.ListJobsByProfile("bot-a", false)
	botBJobs := cs.ListJobsByProfile("bot-b", false)
	globalJobs := cs.ListJobs(false)

	if len(botAJobs) != 2 {
		t.Fatalf("expected 2 jobs for bot-a, got %d", len(botAJobs))
	}
	if len(botBJobs) != 1 {
		t.Fatalf("expected 1 job for bot-b, got %d", len(botBJobs))
	}
	if len(globalJobs) != 3 {
		t.Fatalf("expected 3 global jobs, got %d", len(globalJobs))
	}
}

func TestPauseJobsByProfile(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	}, nil)

	every := int64(60_000)
	cs.AddJobWithOptions("a1", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("a2", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("b1", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-b")

	paused := cs.PauseJobsByProfile("bot-a")
	if paused != 2 {
		t.Fatalf("expected 2 paused, got %d", paused)
	}

	botAJobs := cs.ListJobsByProfile("bot-a", false)
	if len(botAJobs) != 0 {
		t.Fatalf("expected 0 enabled jobs for bot-a after pause, got %d", len(botAJobs))
	}

	botBJobs := cs.ListJobsByProfile("bot-b", false)
	if len(botBJobs) != 1 {
		t.Fatalf("expected bot-b unaffected, got %d jobs", len(botBJobs))
	}
}

func TestResumeJobsByProfile(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	}, nil)

	every := int64(60_000)
	cs.AddJobWithOptions("a1", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("a2", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")

	cs.PauseJobsByProfile("bot-a")
	resumed := cs.ResumeJobsByProfile("bot-a")
	if resumed != 2 {
		t.Fatalf("expected 2 resumed, got %d", resumed)
	}

	botAJobs := cs.ListJobsByProfile("bot-a", false)
	if len(botAJobs) != 2 {
		t.Fatalf("expected 2 enabled jobs for bot-a after resume, got %d", len(botAJobs))
	}
}

func TestRemoveJobsByProfile(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	}, nil)

	every := int64(60_000)
	cs.AddJobWithOptions("a1", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("a2", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-a")
	cs.AddJobWithOptions("b1", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "bot-b")

	removed := cs.RemoveJobsByProfile("bot-a")
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}

	remaining := cs.ListJobs(true)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining job, got %d", len(remaining))
	}
	if remaining[0].Payload.Profile != "bot-b" {
		t.Fatalf("expected remaining job to be bot-b, got %s", remaining[0].Payload.Profile)
	}
}

func TestProfileFieldPersisted(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	}, nil)

	every := int64(60_000)
	job, _ := cs.AddJobWithOptions("test", CronSchedule{Kind: "every", EveryMS: &every}, "m", true, "m", "d", nil, nil, false, "my-profile")

	// Reload from disk
	cs2 := NewCronService(store, nil, nil)
	loaded, ok := cs2.GetJob(job.ID)
	if !ok {
		t.Fatal("job not found after reload")
	}
	if loaded.Payload.Profile != "my-profile" {
		t.Fatalf("expected profile 'my-profile' after reload, got %q", loaded.Payload.Profile)
	}
}
