package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/turnlog"
)

// recordTurn puts a turn into the state a reply is in while it streams.
func recordTurn(t *testing.T, st *turnlog.Store, session, request, channel, userText, partial string) {
	t.Helper()
	if res, err := st.Claim(session, request); err != nil || !res.Created {
		t.Fatalf("claim %s/%s: %v %v", session, request, res, err)
	}
	if _, err := st.Set(session, request, turnlog.StatusRunning, ""); err != nil {
		t.Fatalf("set running: %v", err)
	}
	if err := st.SetOrigin(session, request, userText, channel, "default"); err != nil {
		t.Fatalf("set origin: %v", err)
	}
	if partial != "" {
		if err := st.Checkpoint(session, request, partial, true); err != nil {
			t.Fatalf("checkpoint: %v", err)
		}
	}
}

// restart opens the same turn directory as a fresh process would, marking
// anything still running as interrupted.
func restart(t *testing.T, dir string) *turnlog.Store {
	t.Helper()
	st, err := turnlog.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Recovery's process start always postdates the turns it finds.
	if _, err := st.Recover(time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("recover: %v", err)
	}
	return st
}

func newTurnDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "turns")
}

// A turn that died a moment ago is the one worth finishing: the owner asked
// and the answer never arrived.
func TestPlanFinishesAFreshInterruptedTurn(t *testing.T) {
	dir := newTurnDir(t)
	live, err := turnlog.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	recordTurn(t, live, "main", "req-1", "mobile", "summarise my week", "You shipped three ")

	st := restart(t, dir)
	plan, err := planRecovery(st, time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 1 {
		t.Fatalf("expected the fresh turn to be finished, got %+v", plan.finish)
	}
	if plan.finish[0].turn.RequestID != "req-1" || plan.finish[0].resumeID == "" {
		t.Fatalf("finish job lost its identity: %+v", plan.finish[0])
	}
	if len(plan.materialize) != 0 {
		t.Fatalf("a turn being finished must not also be written out: %+v", plan.materialize)
	}
}

// Nothing written yet is still an unanswered question, so it is finished
// too — there is just nothing to put in the transcript.
func TestPlanFinishesATurnThatProducedNoWords(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "what's the weather", "")

	plan, err := planRecovery(restart(t, dir), time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 1 || len(plan.materialize) != 0 {
		t.Fatalf("expected finish-only, got finish=%d materialize=%d", len(plan.finish), len(plan.materialize))
	}
}

// Past the window the owner has moved on. An answer arriving now answers a
// question nobody is waiting on, so the words are written out instead.
func TestPlanWritesOutAStaleTurn(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "old question", "half an answer")

	st := restart(t, dir)
	plan, err := planRecovery(st, time.Now().Add(2*time.Hour), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 0 {
		t.Fatalf("a two-hour-old turn must not be finished: %+v", plan.finish)
	}
	if len(plan.materialize) != 1 || plan.materialize[0].Partial != "half an answer" {
		t.Fatalf("the words must be preserved: %+v", plan.materialize)
	}
}

// Turning the finisher off leaves every reply to be written out — the safe
// fallback, never a silent drop.
func TestPlanWithFinishingDisabledWritesEverythingOut(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "q", "half")

	plan, err := planRecovery(restart(t, dir), time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 0 || len(plan.materialize) != 1 {
		t.Fatalf("expected materialize-only, got finish=%d materialize=%d", len(plan.finish), len(plan.materialize))
	}
}

// The owner already asked something after this turn. Finishing the old
// reply now would drop a stale answer into the middle of a newer
// conversation.
func TestPlanLeavesATurnTheOwnerMovedOnFrom(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-old", "mobile", "first question", "half of it")
	recordTurn(t, live, "main", "req-new", "mobile", "second question", "")

	st := restart(t, dir)
	plan, err := planRecovery(st, time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 1 || plan.finish[0].turn.RequestID != "req-new" {
		t.Fatalf("only the newest turn may be finished: %+v", plan.finish)
	}
	if len(plan.materialize) != 1 || plan.materialize[0].RequestID != "req-old" {
		t.Fatalf("the superseded turn's words must be preserved: %+v", plan.materialize)
	}
}

// A reply that failed live is not waiting to be resumed — its words go into
// the transcript instead, while they are still recent enough to mean
// something to the owner.
func TestPlanWritesAFailedReplyOutRatherThanResuming(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "q", "it was going fine until ")
	if _, err := live.Set("main", "req-1", turnlog.StatusFailed, "provider down"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	plan, err := planRecovery(restart(t, dir), time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 0 {
		t.Fatalf("a failed turn is terminal, not resumable: %+v", plan.finish)
	}
	if len(plan.materialize) != 1 {
		t.Fatalf("the failed reply must reach the transcript: %+v", plan.materialize)
	}
}

// A fragment of an old failure dropped into the conversation weeks later
// would read as a ghost of something already past, so it stays in the turn
// record — durable, inspectable, and not spoken.
func TestPlanLeavesAnOldFailureInTheRecord(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "q", "half a thought")
	if _, err := live.Set("main", "req-1", turnlog.StatusFailed, "provider down"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	ageTurn(t, dir, "req-1", time.Now().Add(-48*time.Hour))

	plan, err := planRecovery(restart(t, dir), time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 0 || len(plan.materialize) != 0 {
		t.Fatalf("a two-day-old failure is neither finished nor spoken: finish=%d materialize=%d",
			len(plan.finish), len(plan.materialize))
	}
}

// ageTurn rewrites one turn record's own clock, so a test can decide how
// long ago something happened without waiting for it.
func ageTurn(t *testing.T, dir, requestID string, when time.Time) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	aged := false
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]interface{}
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record["request_id"] != requestID {
			continue
		}
		record["updated_at"] = when.Format(time.RFC3339Nano)
		record["created_at"] = when.Format(time.RFC3339Nano)
		out, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, out, 0o600); err != nil {
			t.Fatal(err)
		}
		aged = true
	}
	if !aged {
		t.Fatalf("no record found for %s", requestID)
	}
}

// The turn was waiting on the owner when the process died. The owner owes
// Ghost a decision — re-driving it would re-raise an approval nobody
// answered, possibly twice.
func TestPlanDoesNotAnswerTheOwnersTurnForThem(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "delete my drafts", "I need your ")
	if _, err := live.Set("main", "req-1", turnlog.StatusWaiting, "waiting_for_permission"); err != nil {
		t.Fatalf("wait: %v", err)
	}

	plan, err := planRecovery(restart(t, dir), time.Now(), defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.finish) != 0 {
		t.Fatalf("a turn waiting on the owner must not be driven on: %+v", plan.finish)
	}
	if len(plan.materialize) != 1 || plan.materialize[0].Partial != "I need your " {
		t.Fatalf("what Ghost had said must still be preserved: %+v", plan.materialize)
	}
}

// Recovery must be repeatable: planning twice without running anything must
// give the same answer, or a second crash mid-recovery would double up.
func TestPlanIsStableAcrossRepeatedCalls(t *testing.T) {
	dir := newTurnDir(t)
	live, _ := turnlog.New(dir)
	recordTurn(t, live, "main", "req-1", "mobile", "q", "half")

	st := restart(t, dir)
	now := time.Now()
	first, err := planRecovery(st, now, defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planRecovery(st, now, defaultResumeWindow)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.finish) != len(second.finish) || len(first.materialize) != len(second.materialize) {
		t.Fatalf("plan is not stable: %+v then %+v", first, second)
	}
	if len(first.finish) == 1 && first.finish[0].resumeID != second.finish[0].resumeID {
		t.Fatal("the resume identity must be derived, not random, so replans agree")
	}
}
