package agent

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ianclemence/ghost/pkg/tasks"
	"github.com/ianclemence/ghost/pkg/tools"
)

func newJobTestStore(t *testing.T) *tasks.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/jobs.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := tasks.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	return tasks.NewStore(db, nil)
}

// waitForJobStatus polls for a job to land on want, reporting what it
// settled as instead of failing on a bare timeout.
func waitForJobStatus(t *testing.T, store *tasks.Store, id string, want tasks.Status, timeout time.Duration) tasks.Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		j, err := store.Get(id)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if j.Status == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := store.Get(id)
	t.Fatalf("timed out waiting for %s; job is %s (%s)", want, j.Status, j.Error)
	return tasks.Job{}
}

// A job parked for approval is waiting on the reply, not on a restart:
// releasing it is what makes the checkpoint a checkpoint rather than a
// place work goes to die.
func TestResumeWaitingJobsReleasesOnlyItsSession(t *testing.T) {
	store := newJobTestStore(t)

	mine, err := store.CreateWithScope("subagent", "sess-mine", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(mine.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetWaiting(mine.ID, tasks.StatusWaitingPermission, "approve the send"); err != nil {
		t.Fatal(err)
	}

	other, err := store.CreateWithScope("subagent", "sess-other", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetWaiting(other.ID, tasks.StatusWaitingPermission, "approve the delete"); err != nil {
		t.Fatal(err)
	}

	al := &AgentLoop{jobs: store}
	al.resumeWaitingJobs("sess-mine")

	got, err := store.Get(mine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tasks.StatusPending {
		t.Fatalf("job for the approving session = %s, want pending", got.Status)
	}
	otherGot, err := store.Get(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherGot.Status != tasks.StatusWaitingPermission {
		t.Fatalf("job for another session = %s, want it left parked", otherGot.Status)
	}
}

// The whole point of the wiring: a spawn is written down and then run by
// the runner, and its result lands as evidence on the record rather than
// only in a goroutine's memory.
func TestWireJobRunnerRunsRecordedSpawn(t *testing.T) {
	store := newJobTestStore(t)
	sm := tools.NewSubagentManager(&simpleMockProvider{response: "the summary"}, "test-model", t.TempDir(), nil)
	al := &AgentLoop{jobs: store, subagents: sm, shutdownCtx: context.Background()}
	al.wireJobRunner(sm)
	t.Cleanup(func() { al.jobRunner.Stop() })

	out, err := sm.Spawn(context.Background(), "summarise the inbox", "inbox", "cli", "direct", nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if out == "" {
		t.Fatal("spawn returned nothing for the caller to relay")
	}

	var done []tasks.Job
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		done, err = store.List(tasks.StatusSucceeded)
		if err != nil {
			t.Fatal(err)
		}
		if len(done) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(done) != 1 {
		t.Fatalf("succeeded jobs = %d, want the recorded spawn to have run", len(done))
	}
	if done[0].Evidence != "the summary" {
		t.Fatalf("evidence = %q, want the task's result recorded durably", done[0].Evidence)
	}
}

// Work a crash left behind is re-queued at boot and run, which is the
// difference between a durable store and a table nobody reads.
func TestWireJobRunnerRecoversInterruptedSpawn(t *testing.T) {
	store := newJobTestStore(t)

	j, err := store.Create("subagent", "sess-recover", map[string]interface{}{
		"task": "finish the report", "label": "report", "channel": "cli", "chat_id": "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkInterrupted(); err != nil {
		t.Fatal(err)
	}

	sm := tools.NewSubagentManager(&simpleMockProvider{response: "recovered result"}, "test-model", t.TempDir(), nil)
	al := &AgentLoop{jobs: store, subagents: sm, shutdownCtx: context.Background()}
	al.wireJobRunner(sm)
	t.Cleanup(func() { al.jobRunner.Stop() })

	done := waitForJobStatus(t, store, j.ID, tasks.StatusSucceeded, 10*time.Second)
	if done.Evidence != "recovered result" {
		t.Fatalf("evidence = %q, want the recovered run's result", done.Evidence)
	}
	// Recovery charged the interruption against the budget, which is how a
	// crash loop is stopped; one restart must still leave room.
	if done.Failures != 1 {
		t.Fatalf("failures = %d, want 1 for one interruption", done.Failures)
	}
}

// A job whose kind has no handler is left readable instead of run by
// whatever happens to be registered next door.
func TestWireJobRunnerLeavesUnknownKindPending(t *testing.T) {
	store := newJobTestStore(t)
	sm := tools.NewSubagentManager(&simpleMockProvider{response: "x"}, "test-model", t.TempDir(), nil)
	al := &AgentLoop{jobs: store, subagents: sm, shutdownCtx: context.Background()}
	al.wireJobRunner(sm)
	t.Cleanup(func() { al.jobRunner.Stop() })

	j, err := store.Create("mystery", "sess", map[string]interface{}{"task": "?"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	got, err := store.Get(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != tasks.StatusPending || got.Attempts != 0 {
		t.Fatalf("unknown kind = %s attempts=%d, want pending and unattempted", got.Status, got.Attempts)
	}
}
