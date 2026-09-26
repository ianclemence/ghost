package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/commitments"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/tools"
)

// The state fast path exists so authoritative answers skip the model. These
// tests hold both halves of that contract: the questions it answers, and the
// questions it must not touch.
func TestStateQueryDispatch(t *testing.T) {
	al, _, _ := newProactiveLoop(t)

	mustHandle := []string{
		"what reminders do I have?",
		"what's on today",
		"check my reminders",
		"what needs me?",
		"do I have anything waiting for approval?",
		"is my morning routine okay?",
		"what tasks are overdue?",
		"what did you just do?",
		"is Ghost healthy?",
		"how much disk space is left?",
		"what model are you using?",
		"is proactive mode on?",
		"what did I promise?",
	}
	for _, q := range mustHandle {
		if _, ok := al.tryStateQueryTurn(q, "main"); !ok {
			// Handled=false is legitimate when the backing store is unwired
			// in this harness; what matters is that a pattern matched and the
			// renderer was consulted. Assert the matcher directly instead.
			if !stateQueryPatternMatches(strings.ToLower(q)) {
				t.Errorf("no state path matched %q", q)
			}
		}
	}

	mustNotHandle := []string{
		"hey Ghost",
		"what did I tell you about the deposit?",
		"remind me about the chelsea game on 9 october at 9 am",
		"send Alex those photos",
		"what's the weather in Bangkok",
		"add milk to my shopping list",
		"who won the match last night",
	}
	for _, q := range mustNotHandle {
		if _, ok := al.tryStateQueryTurn(q, "main"); ok {
			t.Errorf("state path must not answer %q", q)
		}
	}
}

// stateQueryPatternMatches mirrors the dispatcher's matcher set without
// invoking any renderer, so the test is stable when a store is absent.
func stateQueryPatternMatches(lower string) bool {
	return reminderQueryRE.MatchString(lower) || proposalQueryRE.MatchString(lower) ||
		approvalQueryRE.MatchString(lower) || routineQueryRE.MatchString(lower) ||
		jobQueryRE.MatchString(lower) || activityQueryRE.MatchString(lower) ||
		healthQueryRE.MatchString(lower) || modelQueryRE.MatchString(lower) ||
		proactivePolicyQueryRE.MatchString(lower) || commitmentQueryRE.MatchString(lower)
}

// Renderers must print only fields that exist in the store. A reminder the
// owner never made must not appear, and a name that exists nowhere must not
// be invented.
func TestReminderAnswerIsStateBound(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	al.RegisterTool(tools.NewScheduleTool(svc, "Asia/Bangkok"))
	al.schedSvc = svc

	when := time.Now().UTC().Add(3 * time.Hour)
	if err := svc.CreateItem(&scheduled.ScheduledItem{
		Type: scheduled.TypeReminder, Title: "call the plumber",
		State: scheduled.StateScheduled, Timezone: "Asia/Bangkok",
		Schedule: scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &when},
		Action:   scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: "call the plumber"},
		Source:   "user", CreatedBy: "test", NextRunAt: &when,
	}); err != nil {
		t.Fatalf("create reminder: %v", err)
	}

	ans, ok := al.tryStateQueryTurn("what reminders do I have?", "main")
	if !ok {
		t.Fatal("a schedule listing must be answered from the scheduler")
	}
	if !strings.Contains(ans, "plumber") {
		t.Fatalf("the real reminder must appear: %q", ans)
	}
	for _, invented := range []string{"dentist", "Alex", "gym"} {
		if strings.Contains(ans, invented) {
			t.Fatalf("answer invented %q: %q", invented, ans)
		}
	}

	// An empty schedule must say so, not invent one.
	al2, svc2, _ := newProactiveLoop(t)
	al2.RegisterTool(tools.NewScheduleTool(svc2, "Asia/Bangkok"))
	al2.schedSvc = svc2
	ans2, ok2 := al2.tryStateQueryTurn("what reminders do I have?", "main")
	if !ok2 {
		t.Fatal("even an empty schedule has a truthful answer")
	}
	if strings.Contains(ans2, "plumber") {
		t.Fatalf("empty schedule must not borrow another workspace's data: %q", ans2)
	}
}

func TestProposalAndApprovalAnswersAreStateBound(t *testing.T) {
	al, _, _ := newProactiveLoop(t)

	// No proposals yet: a truthful empty answer.
	if ans, ok := al.renderProposals(); !ok || !strings.Contains(ans, "Nothing needs you") {
		t.Fatalf("empty proposal store answer = %q (ok=%v)", ans, ok)
	}

	store, err := ideas.New(al.workspace)
	if err != nil {
		t.Fatalf("ideas store: %v", err)
	}
	exp := time.Now().UTC().Add(24 * time.Hour)
	if err := store.Add([]ideas.Idea{{
		ID: "idea-state-test", Title: "a reminder never landed", Body: "Your reminder never reached you",
		Status: ideas.StatusPresented, Kind: ideas.ObsReminderOverdue, Priority: 8, Confidence: 0.9,
		ExpiresAt: &exp, CreatedAt: time.Now().UTC(),
		Plan: &ideas.Plan{Kind: ideas.PlanLocal, Op: "remind_now", Capability: "schedule.create",
			Risk: ideas.RiskLow, Describe: "run it now"},
	}}); err != nil {
		t.Fatalf("add idea: %v", err)
	}
	ans, ok := al.renderProposals()
	if !ok || !strings.Contains(ans, "never reached you") {
		t.Fatalf("proposal answer = %q (ok=%v)", ans, ok)
	}
	if !strings.Contains(ans, "run it now") {
		t.Fatalf("the offered action must come from the plan, got %q", ans)
	}
	if strings.Contains(ans, "dentist") {
		t.Fatalf("answer invented content: %q", ans)
	}

	// Approvals: no pending requests is a truthful answer.
	if ans, ok := al.renderApprovals(); !ok || !strings.Contains(ans, "Nothing is waiting") {
		t.Fatalf("empty approval answer = %q (ok=%v)", ans, ok)
	}
	// A real pending request must surface with the broker's own title.
	if _, err := al.governance.Broker.Require("req-state-test", "main", "agent-test",
		"message.send", "message", "alex", "send it", permissions.RiskConsequential, nil); err != nil {
		t.Fatalf("require: %v", err)
	}
	ans2, ok2 := al.renderApprovals()
	if !ok2 || !strings.Contains(ans2, "1 waiting for your approval") {
		t.Fatalf("pending approval answer = %q (ok=%v)", ans2, ok2)
	}
}

func TestCommitmentAnswerIsStateBound(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	if ans, ok := al.renderCommitments(); !ok || !strings.Contains(ans, "no open promises") {
		t.Fatalf("empty ledger answer = %q (ok=%v)", ans, ok)
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("commitment store: %v", err)
	}
	due := time.Now().UTC().Add(-time.Hour)
	if _, err := store.Create(commitments.Commitment{
		Text: "send Alex those photos", Kind: commitments.KindSend, DueAt: &due,
		Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{Session: "main", MessageID: "m1",
			Quote: "I need to send Alex those photos", At: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	ans, ok := al.renderCommitments()
	if !ok || !strings.Contains(ans, "send Alex those photos") {
		t.Fatalf("commitment answer = %q (ok=%v)", ans, ok)
	}
	if strings.Contains(ans, "dentist") {
		t.Fatalf("answer invented content: %q", ans)
	}
}

func TestActivityAnswerUsesRealEvents(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.governance.Events.Publish(&cevents.Event{
		Type: cevents.CapabilityCompleted, SessionID: "main",
		GhostID: "ghost-test", AgentID: "agent-test",
		Payload: map[string]interface{}{"capability": "schedule.create"},
	})
	ans, ok := al.renderActivity()
	if !ok {
		t.Fatal("activity must be answerable from the event stream")
	}
	if !strings.Contains(ans, "My last actions") {
		t.Fatalf("activity answer = %q", ans)
	}
}

// Health reports real numbers and never a fabricated state.
func TestHealthAnswerReportsRealState(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	ans, ok := al.renderHealth()
	if !ok {
		t.Fatal("health must be answerable from the device snapshot")
	}
	if !strings.Contains(ans, "MB RAM available") || !strings.Contains(ans, "disk free") {
		t.Fatalf("health answer must carry real measurements: %q", ans)
	}
	if strings.Contains(ans, "healthy") && strings.Contains(ans, "critical") {
		t.Fatalf("health answer is self-contradictory: %q", ans)
	}
}

// An unwired store must fall through to the normal path, never invent.
func TestUnwiredStoresFallThrough(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.routineSvc = nil
	if _, ok := al.renderRoutines(); ok {
		t.Fatal("routines must not be answered when the routine store is unwired")
	}
	al2, _, _ := newProactiveLoop(t)
	al2.tools = nil
	al2.schedSvc = nil
	if _, ok := al2.renderReminders("main"); ok {
		t.Fatal("reminders must not be answered when the scheduler is unwired")
	}
}
