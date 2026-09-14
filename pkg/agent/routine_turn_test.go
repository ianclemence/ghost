package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	ghodb "github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/skills"
)

func testRoutineLoop(t *testing.T) *AgentLoop {
	t.Helper()
	ws := t.TempDir()
	database, err := ghodb.NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return &AgentLoop{workspace: ws, db: database}
}

func routineMsg(session, content string) bus.InboundMessage {
	return bus.InboundMessage{Channel: "web", SenderID: "u", ChatID: "c",
		Content: content, SessionKey: session, Metadata: map[string]string{}}
}

func TestRoutineProposalAndConfirm(t *testing.T) {
	al := testRoutineLoop(t)
	ans, ok := al.tryRoutineTurn(routineMsg("sess-r", "Every Monday at 9 remind me to review my finances"))
	if !ok {
		t.Fatal("must propose routine")
	}
	if !strings.Contains(ans, "Say yes to confirm") {
		t.Fatalf("must ask confirmation: %q", ans)
	}
	ans2, ok := al.tryRoutineTurn(routineMsg("sess-r", "yes"))
	if !ok {
		t.Fatal("must confirm")
	}
	if !strings.Contains(ans2, "Done.") {
		t.Fatalf("must create: %q", ans2)
	}
	// Proposal consumed: further yes is ordinary chat.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-r", "yes")); ok {
		t.Fatal("consumed proposal must not re-trigger")
	}
}

func TestRoutineClarifyTask(t *testing.T) {
	al := testRoutineLoop(t)
	ans, ok := al.tryRoutineTurn(routineMsg("sess-c", "Every Monday at 9"))
	if !ok || !strings.Contains(ans, "What should happen") {
		t.Fatalf("must clarify task: %q", ans)
	}
	// User supplies the task → proposal.
	ans2, ok := al.tryRoutineTurn(routineMsg("sess-c", "review my finances"))
	if !ok || !strings.Contains(ans2, "Say yes to confirm") {
		t.Fatalf("must propose after task: %q", ans2)
	}
	ans3, ok := al.tryRoutineTurn(routineMsg("sess-c", "yes"))
	if !ok || !strings.Contains(ans3, "Done.") {
		t.Fatalf("must create: %q", ans3)
	}
}

func TestRoutineDecline(t *testing.T) {	al := testRoutineLoop(t)
	al.tryRoutineTurn(routineMsg("sess-d", "Every Friday at 5 remind me to stop work"))
	ans, ok := al.tryRoutineTurn(routineMsg("sess-d", "no"))
	if !ok || !strings.Contains(ans, "didn't schedule") {
		t.Fatalf("must decline cleanly: %q", ans)
	}
}

func TestOneTimeNotRoutineTurn(t *testing.T) {
	al := testRoutineLoop(t)
	if _, ok := al.tryRoutineTurn(routineMsg("sess-o", "Remind me tomorrow at 9 to buy milk")); ok {
		t.Fatal("one-time reminder must not become a routine")
	}
	if _, ok := al.tryRoutineTurn(routineMsg("sess-o", "what's the weather")); ok {
		t.Fatal("ordinary chat must not trigger")
	}
}

func TestRoutinePendingDurable(t *testing.T) {
	al := testRoutineLoop(t)
	al.tryRoutineTurn(routineMsg("sess-p", "Every Monday at 9 remind me to stretch"))
	// Durable store (survives process restart, unlike session memory).
	s2 := skills.NewPendingStore(al.workspace)
	pending, ok := s2.OpenForSession("sess-p")
	if !ok || pending.Capability != routinePendingCapability {
		t.Fatal("proposal must be durable")
	}
	if pending.Continuation["kind"] == "" || pending.Continuation["instruction"] == "" {
		t.Fatalf("spec must persist: %+v", pending.Continuation)
	}
}

func TestRoutineDuplicatePrevention(t *testing.T) {
	al := testRoutineLoop(t)
	// First creation.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-dup", "Every Monday at 9 remind me to water the plants")); !ok {
		t.Fatal("first propose")
	}
	if ans, ok := al.tryRoutineTurn(routineMsg("sess-dup", "yes")); !ok || !strings.Contains(ans, "Done.") {
		t.Fatalf("first confirm: %q", ans)
	}
	// Identical request again -> propose; confirm -> NOT a duplicate.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-dup", "Every Monday at 9 remind me to water the plants")); !ok {
		t.Fatal("second propose")
	}
	ans, ok := al.tryRoutineTurn(routineMsg("sess-dup", "yes"))
	if !ok || !strings.Contains(ans, "already have that routine") {
		t.Fatalf("duplicate must be rejected honestly: %q", ans)
	}
	// Different time on the same instruction -> a distinct routine is fine.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-dup", "Every Monday at 18 remind me to water the plants")); !ok {
		t.Fatal("third propose")
	}
	if ans, ok := al.tryRoutineTurn(routineMsg("sess-dup", "yes")); !ok || !strings.Contains(ans, "Done.") {
		t.Fatalf("distinct schedule must create: %q", ans)
	}
}

func TestStandingGoalSkipsRoutineTurn(t *testing.T) {
	al := testRoutineLoop(t)
	// Explicit standing-goal language must fall through to the model
	// (goal tool), even with schedule words that parse as routine.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-g", "Set a standing goal to read for 20 minutes every evening")); ok {
		t.Fatal("standing goal must not become a routine proposal")
	}
	// Ordinary reminders still route to routines.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-r2", "Every evening remind me to read")); !ok {
		t.Fatal("ordinary reminder must still propose a routine")
	}
}

func TestProposalTaskQuestionFallsThrough(t *testing.T) {
	al := testRoutineLoop(t)
	if _, ok := al.tryRoutineTurn(routineMsg("sess-q", "Every Monday at 9")); !ok {
		t.Fatal("must clarify task")
	}
	// A question about existing state is not task content: it must fall
	// through to the model, never become "remind you to What are my goals?".
	if _, ok := al.tryRoutineTurn(routineMsg("sess-q", "What are my goals?")); ok {
		t.Fatal("state question must not become routine task text")
	}
	// Genuine task content still completes the proposal.
	if _, ok := al.tryRoutineTurn(routineMsg("sess-q", "review my finances")); !ok {
		t.Fatal("real task must still complete the proposal")
	}
}
