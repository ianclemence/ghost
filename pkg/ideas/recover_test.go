package ideas

import (
	"testing"
	"time"
)

// An approved action that was running when Ghost stopped used to stay
// "executing" for ever: nothing finished it, and the dedupe check treats
// executing as live, so the same opportunity could never be proposed again.
func TestInterruptedExecutionIsFailedHonestlyAndFreesTheOpportunity(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Add([]Idea{{Title: "Retry the water reminder", Body: "b", DedupeKey: "reminder-failed:r1", Status: StatusPending}}); err != nil {
		t.Fatal(err)
	}
	list, _ := st.List(StatusPending, 10)
	id := list[0].ID
	began := time.Now().UTC().Add(-2 * time.Hour)
	if _, err := st.BeginExecution(id, began); err != nil {
		t.Fatal(err)
	}

	live, _ := st.Live(50)
	if quiet, _ := Quiet(live, "reminder-failed:r1", time.Now().UTC(), DedupeWindow{}); !quiet {
		t.Fatal("precondition: an executing idea blocks its opportunity")
	}

	// A run that is still inside its window is left alone.
	if got, err := st.RecoverInterrupted(time.Now().UTC(), began.Add(-time.Minute)); err != nil || len(got) != 0 {
		t.Fatalf("a run inside its window must not be touched: %v %v", got, err)
	}

	got, err := st.RecoverInterrupted(time.Now().UTC(), time.Now().UTC().Add(-30*time.Minute))
	if err != nil || len(got) != 1 {
		t.Fatalf("the orphaned run must be recovered: %v %v", got, err)
	}
	if got[0].Status != StatusFailed || got[0].Outcome != "interrupted" || got[0].DecidedAt == nil {
		t.Fatalf("it must end in an honest terminal state: %+v", got[0])
	}
	if again, _ := st.RecoverInterrupted(time.Now().UTC(), time.Now().UTC()); len(again) != 0 {
		t.Fatal("recovery must be idempotent")
	}
	if running, _ := st.List(StatusExecuting, 0); len(running) != 0 {
		t.Fatalf("nothing may stay executing: %v", running)
	}
}

// Records written before ExecutionStartedAt existed have no start time; an
// executing record like that can only be an orphan.
func TestExecutingWithoutAStartTimeIsRecovered(t *testing.T) {
	st, _ := New(t.TempDir())
	if err := st.Add([]Idea{{Title: "old", Body: "b", Status: StatusExecuting}}); err != nil {
		t.Fatal(err)
	}
	got, err := st.RecoverInterrupted(time.Now().UTC(), time.Now().UTC().Add(-time.Hour))
	if err != nil || len(got) != 1 {
		t.Fatalf("an executing record with no start time is an orphan: %v %v", got, err)
	}
}
