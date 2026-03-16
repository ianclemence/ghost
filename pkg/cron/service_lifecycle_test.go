package cron

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCronLifecyclePauseResumeUpdateRunNow(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	})

	every := int64(60_000)
	job, err := cs.AddJob(
		"test job",
		CronSchedule{Kind: "every", EveryMS: &every},
		"hello",
		true,
		"mobile",
		"default",
	)
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if err := cs.PauseJob(job.ID); err != nil {
		t.Fatalf("PauseJob failed: %v", err)
	}
	st, err := cs.GetJobStatus(job.ID)
	if err != nil {
		t.Fatalf("GetJobStatus after pause failed: %v", err)
	}
	if st.State != JobStatePaused || st.Enabled {
		t.Fatalf("expected paused+disabled, got state=%s enabled=%v", st.State, st.Enabled)
	}

	if err := cs.ResumeJob(job.ID); err != nil {
		t.Fatalf("ResumeJob failed: %v", err)
	}
	st, err = cs.GetJobStatus(job.ID)
	if err != nil {
		t.Fatalf("GetJobStatus after resume failed: %v", err)
	}
	if st.State != JobStateActive || !st.Enabled {
		t.Fatalf("expected active+enabled, got state=%s enabled=%v", st.State, st.Enabled)
	}

	newMessage := "updated message"
	target := "local"
	enabled := true
	if err := cs.UpdateJob(job.ID, JobUpdate{
		Message: &newMessage,
		Target:  &target,
		Enabled: &enabled,
	}); err != nil {
		t.Fatalf("UpdateJob failed: %v", err)
	}

	updated, ok := cs.GetJob(job.ID)
	if !ok {
		t.Fatalf("expected job to exist after update")
	}
	if updated.Payload.Message != newMessage {
		t.Fatalf("expected updated message %q, got %q", newMessage, updated.Payload.Message)
	}
	if updated.Payload.Target != target {
		t.Fatalf("expected updated target %q, got %q", target, updated.Payload.Target)
	}

	if err := cs.RunJobNow(job.ID); err != nil {
		t.Fatalf("RunJobNow failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := cs.GetJobStatus(job.ID)
		if err == nil && status.RunCount > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	status, _ := cs.GetJobStatus(job.ID)
	t.Fatalf("expected run_count > 0 after RunJobNow, got %d", status.RunCount)
}

func TestRunJobNowPreservesRecurringSchedule(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs-preserve.json")
	cs := NewCronService(store, func(job *CronJob) (string, error) {
		return "ok", nil
	})

	every := int64(3_600_000)
	job, err := cs.AddJob(
		"preserve cadence",
		CronSchedule{Kind: "every", EveryMS: &every},
		"hello",
		true,
		"mobile",
		"default",
	)
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	before, ok := cs.GetJob(job.ID)
	if !ok || before.State.NextRunAtMS == nil {
		t.Fatalf("expected next run before manual trigger")
	}
	originalNext := *before.State.NextRunAtMS

	if err := cs.RunJobNow(job.ID); err != nil {
		t.Fatalf("RunJobNow failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status, err := cs.GetJobStatus(job.ID)
		if err == nil && status.RunCount > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	after, ok := cs.GetJob(job.ID)
	if !ok || after.State.NextRunAtMS == nil {
		t.Fatalf("expected next run after manual trigger")
	}
	if *after.State.NextRunAtMS != originalNext {
		t.Fatalf("expected next run to remain %d, got %d", originalNext, *after.State.NextRunAtMS)
	}
}
