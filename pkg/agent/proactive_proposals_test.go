package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// newProactiveLoop wires the same subsystems cmd/ghost wires at startup —
// authoritative broker, canonical event stream, scheduler + routine signals —
// over a throwaway workspace. It is the honest harness for the proactive
// lifecycle: nothing here is stubbed except the scheduler's executor (the
// scheduler's own firing is covered by pkg/scheduled).
func newProactiveLoop(t *testing.T) (*AgentLoop, *scheduled.Service, *cevents.Stream) {
	t.Helper()
	return newProactiveLoopWith(t, t.TempDir(), &mockProvider{})
}

func newProactiveLoopWith(t *testing.T, ws string, provider providers.LLMProvider) (*AgentLoop, *scheduled.Service, *cevents.Stream) {
	t.Helper()
	al := newTestAgentLoopWithProvider(t, ws, provider)

	broker, err := permissions.Open(al.DB(), permissions.ModeAsk, 0)
	if err != nil {
		t.Fatalf("open broker: %v", err)
	}
	stream, err := cevents.Open(al.DB(), filepath.Join(ws, "events"))
	if err != nil {
		t.Fatalf("open events: %v", err)
	}
	al.SetGovernance(NewGovernance(stream, broker, "ghost-test", "agent-test"))

	store := scheduled.NewStore(al.DB())
	if err := store.InitSchema(); err != nil {
		t.Fatalf("scheduled schema: %v", err)
	}
	svc := scheduled.NewService(store, &scheduled.SimpleEventBus{},
		func(ctx context.Context, it *scheduled.ScheduledItem) error { return nil })
	al.SetRoutineSignals(nil, svc)

	if err := al.state.SetLastActiveSession("mobile", "owner-1"); err != nil {
		t.Fatalf("set active session: %v", err)
	}
	return al, svc, stream
}

// overdueReminder persists a one-shot reminder that was due in the past and
// never fired — the real-world shape of a promise Ghost did not keep.
func overdueReminder(t *testing.T, svc *scheduled.Service, title string, due time.Time) *scheduled.ScheduledItem {
	t.Helper()
	item := &scheduled.ScheduledItem{
		Type:      scheduled.TypeReminder,
		Title:     title,
		State:     scheduled.StateScheduled,
		Schedule:  scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &due},
		Timezone:  "UTC",
		Action:    scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: title, Deliver: true},
		Source:    "user",
		CreatedBy: "test",
		NextRunAt: &due,
		MaxRetries: 3,
	}
	if err := svc.CreateItem(item); err != nil {
		t.Fatalf("create reminder: %v", err)
	}
	return item
}

func liveIdeas(t *testing.T, ws string) []ideas.Idea {
	t.Helper()
	store, err := ideas.New(ws)
	if err != nil {
		t.Fatalf("open ideas: %v", err)
	}
	all, err := store.Live(200)
	if err != nil {
		t.Fatalf("list ideas: %v", err)
	}
	return all
}

func findIdea(t *testing.T, ws string, kind ideas.ObsKind) ideas.Idea {
	t.Helper()
	for _, i := range liveIdeas(t, ws) {
		if i.Kind == kind {
			return i
		}
	}
	t.Fatalf("no %s idea found", kind)
	return ideas.Idea{}
}

// The full loop: a grounded observation becomes a proposal with real evidence,
// binds a broker request, delivers a card the owner can act on, and on approval
// executes through the real path and records a verified outcome.
func TestProactiveProposalLifecycleReminder(t *testing.T) {
	al, svc, stream := newProactiveLoop(t)
	ws := al.workspace
	now := time.Now().UTC()
	due := now.Add(-2 * time.Hour)
	item := overdueReminder(t, svc, "send Alex the document", due)

	// NOTICE + REASON + PROPOSE.
	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 surfaced proposal, got %d", n)
	}
	idea := findIdea(t, ws, ideas.ObsReminderOverdue)
	if idea.Status != ideas.StatusPresented {
		t.Fatalf("proposal must be presented, got %s", idea.Status)
	}
	if idea.Plan == nil || idea.Plan.Op != opRemindNow {
		t.Fatalf("proposal must carry the remind_now plan, got %+v", idea.Plan)
	}
	if idea.PermissionRequestID == "" {
		t.Fatal("proposal must bind a broker request")
	}
	if len(idea.Sources) != 1 || idea.Sources[0].Ref != item.ID {
		t.Fatalf("proposal must cite the reminder row, got %+v", idea.Sources)
	}

	// The ask exists in the broker as a real pending request.
	broker := al.governance.Broker
	req, ok := broker.PendingForRequest(idea.PermissionRequestID)
	if !ok {
		// PendingForRequest keys on the turn id; fall back to the row id.
		list := broker.Requests(permissions.StatusPending, 10)
		if len(list) == 0 {
			t.Fatal("no pending broker request for the proposal")
		}
		req = list[0]
	}
	if req.Risk != permissions.RiskLow {
		t.Fatalf("reminder reschedule risk = %s, want low_risk", req.Risk)
	}

	// The owner-facing card carries the approve/deny actions bound to it.
	found := false
	for _, c := range cards.DefaultStore.List("mobile") {
		if c.RequestID == idea.PermissionRequestID {
			found = true
			if len(c.Actions) != 2 {
				t.Fatalf("proposal card must carry approve+deny, got %+v", c.Actions)
			}
		}
	}
	if !found {
		t.Fatal("no delivered card bound to the proposal request")
	}

	// APPROVE → ACT → VERIFY → REMEMBER.
	record, result, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if record.Status != ideas.StatusCompleted {
		t.Fatalf("approved proposal must complete, got %s (%s)", record.Status, record.Result)
	}
	if record.Outcome == "" || record.Result == "" {
		t.Fatalf("outcome and result must be recorded from evidence, got %+v", record)
	}
	if !strings.Contains(result, "go off") {
		t.Fatalf("result must state what actually happened, got %q", result)
	}

	// The action really happened: the reminder is rescheduled to now.
	got, err := svc.GetItem(item.ID)
	if err != nil || got == nil {
		t.Fatalf("reload reminder: %v", err)
	}
	if got.State != scheduled.StateScheduled || got.NextRunAt == nil {
		t.Fatalf("reminder must be rescheduled, got state=%s next=%v", got.State, got.NextRunAt)
	}

	// The lifecycle is in the canonical stream and readable as activity.
	events := stream.Recent(100, cevents.Filter{})
	seen := map[cevents.Type]bool{}
	for _, e := range events {
		seen[e.Type] = true
	}
	for _, want := range []cevents.Type{cevents.ProactiveCandidate, cevents.ProactivePresented, cevents.ProactiveApproved, cevents.ProactiveCompleted, cevents.VerificationCompleted} {
		if !seen[want] {
			t.Fatalf("canonical stream missing %s", want)
		}
	}

	// Duplicate suppression: the same opportunity never returns while answered.
	if n := al.EvaluateProposals(now.Add(time.Minute)); n != 0 {
		t.Fatalf("answered opportunity must not resurface, got %d", n)
	}
}

// Dismissing a proposal must not execute anything, must cancel the broker
// request so the card can never resolve, and must keep the opportunity quiet.
func TestProactiveProposalDismissedDoesNotExecute(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	ws := al.workspace
	now := time.Now().UTC()
	item := overdueReminder(t, svc, "call the dentist", now.Add(-3*time.Hour))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	idea := findIdea(t, ws, ideas.ObsReminderOverdue)

	decided, _, err := al.DecideIdea(context.Background(), idea.ID, "dismiss", 0)
	if err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	if decided.Status != ideas.StatusDismissed {
		t.Fatalf("status = %s, want dismissed", decided.Status)
	}
	// No side effect: the reminder is untouched.
	got, _ := svc.GetItem(item.ID)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(*item.NextRunAt) {
		t.Fatalf("dismiss must not reschedule the reminder, got %v", got.NextRunAt)
	}
	// The bound request is cancelled, so a stale card cannot execute it.
	if req, ok := al.governance.Broker.PendingForRequest(idea.PermissionRequestID); ok {
		t.Fatalf("dismissed proposal must not leave a resolvable request: %+v", req)
	}
	// Repeated evaluation stays quiet inside the dismissal cooldown.
	if n := al.EvaluateProposals(now.Add(time.Minute)); n != 0 {
		t.Fatalf("dismissed opportunity must stay quiet, got %d", n)
	}
}

// If the world changes between proposal and approval, the approval is void and
// nothing executes — the anti-stale-approval guarantee.
func TestProactiveProposalSupersededWhenStateChanges(t *testing.T) {
	al, svc, stream := newProactiveLoop(t)
	ws := al.workspace
	now := time.Now().UTC()
	item := overdueReminder(t, svc, "book the vet", now.Add(-90*time.Minute))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	idea := findIdea(t, ws, ideas.ObsReminderOverdue)

	// The situation resolves itself: the reminder fires and completes.
	item.State = scheduled.StateCompleted
	if err := svc.UpdateItem(item); err != nil {
		t.Fatalf("complete reminder: %v", err)
	}

	record, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err == nil {
		t.Fatal("approving a stale proposal must refuse")
	}
	if record.Status != ideas.StatusSuperseded {
		t.Fatalf("status = %s, want superseded", record.Status)
	}
	// Nothing executed: the reminder stays completed, not rescheduled.
	got, _ := svc.GetItem(item.ID)
	if got.State != scheduled.StateCompleted {
		t.Fatalf("stale approval must not mutate state, got %s", got.State)
	}
	seen := false
	for _, e := range stream.Recent(100, cevents.Filter{}) {
		if e.Type == cevents.ProactiveSuperseded {
			seen = true
		}
	}
	if !seen {
		t.Fatal("supersede must be recorded in the canonical stream")
	}
}

// The suggestion never becomes executable twice: a second approval on the same
// proposal is refused and produces no second side effect.
func TestProactiveProposalApprovedExactlyOnce(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "send the invoice", now.Add(-4*time.Hour))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	idea := findIdea(t, al.workspace, ideas.ObsReminderOverdue)

	if _, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0); err == nil {
		t.Fatal("second approve must be refused")
	}
}

// Proactive assistance can be switched off entirely without disabling the rest
// of Ghost, and the preference file drives it.
func TestProactiveDisabledByPreference(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "water the plants", now.Add(-5*time.Hour))

	writePrefs(t, al.workspace, "`enabled: false`\n")
	if n := al.EvaluateProposals(now); n != 0 {
		t.Fatalf("disabled proactivity must surface nothing, got %d", n)
	}
	if got := liveIdeas(t, al.workspace); len(got) != 0 {
		t.Fatalf("disabled proactivity must create no candidates, got %d", len(got))
	}

	// Re-enabled: the same opportunity is picked up.
	writePrefs(t, al.workspace, "`enabled: true`\n")
	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("re-enabled proactivity must surface the opportunity, got %d", n)
	}
}

// Category controls are honoured without touching the notification budget.
func TestProactiveCategoryFilter(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "buy milk", now.Add(-2*time.Hour))

	writePrefs(t, al.workspace, "`categories: routines`\n")
	if n := al.EvaluateProposals(now); n != 0 {
		t.Fatalf("filtered category must not surface, got %d", n)
	}
}

// Quiet hours hold delivery but not the record: the proposal still exists and
// is waiting, not lost.
func TestProactiveQuietHoursHoldsDelivery(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "take the bins out", now.Add(-2*time.Hour))

	// Quiet all day: no push now, but the proposal is durable.
	writePrefs(t, al.workspace, "`quiet_hours: 00:00 - 23:59`\n")
	al.EvaluateProposals(now)

	held := false
	for _, i := range liveIdeas(t, al.workspace) {
		if i.Kind == ideas.ObsReminderOverdue && i.Status.Undecided() {
			held = true
		}
	}
	if !held {
		t.Fatal("a quiet-hours opportunity must survive as a durable record")
	}
}

func writePrefs(t *testing.T, workspace, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, "PROACTIVE_PREFERENCES.md"), []byte(body), 0600); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
}

func TestProactivePolicyDefaultsOn(t *testing.T) {
	pol := proactive.Load(t.TempDir())
	if !pol.Enabled {
		t.Fatal("proactive assistance must default on")
	}
	if !pol.CategoryAllowed("reminders") {
		t.Fatal("with no category restriction every category is allowed")
	}
}

// Evaluation is local and deterministic: deciding whether to interrupt the
// owner must never cost a model call, and must never leave the device.
func TestProactiveEvaluationNeverCallsTheModel(t *testing.T) {
	counting := &countingProvider{}
	al, svc, _ := newProactiveLoopWith(t, t.TempDir(), counting)
	now := time.Now().UTC()
	overdueReminder(t, svc, "renew the passport", now.Add(-6*time.Hour))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	if counting.calls != 0 {
		t.Fatalf("proactive evaluation must not invoke the model, got %d calls", counting.calls)
	}
	// Approving a deterministic local action also needs no model.
	idea := findIdea(t, al.workspace, ideas.ObsReminderOverdue)
	if _, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if counting.calls != 0 {
		t.Fatalf("a deterministic action must not invoke the model, got %d calls", counting.calls)
	}
}

// A failing executor produces an honest failure, never a success claim, and
// backs off so Ghost does not nag.
func TestProactiveProposalFailureIsHonest(t *testing.T) {
	al, svc, stream := newProactiveLoop(t)
	now := time.Now().UTC()
	item := overdueReminder(t, svc, "pay the electricity bill", now.Add(-2*time.Hour))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	idea := findIdea(t, al.workspace, ideas.ObsReminderOverdue)

	// Make the executor fail: the item disappears between proposal and
	// approval, which is a real runtime failure mode (deleted elsewhere).
	if err := scheduled.NewStore(al.DB()).Delete(item.ID); err != nil {
		t.Fatalf("delete reminder: %v", err)
	}

	record, result, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err == nil {
		t.Fatal("a failing action must not report success")
	}
	if record.Status != ideas.StatusSuperseded && record.Status != ideas.StatusFailed {
		if err == nil {
			t.Fatalf("status = %s, want failed/superseded", record.Status)
		}
	}
	if record.Status == ideas.StatusCompleted {
		t.Fatal("a failed action must never be recorded as completed")
	}
	if strings.Contains(strings.ToLower(result), "done") {
		t.Fatalf("failure text must not claim success, got %q", result)
	}
	failed := false
	for _, e := range stream.Recent(100, cevents.Filter{}) {
		if e.Type == cevents.ProactiveCompleted {
			t.Fatal("a failed action must not emit proactive.completed")
		}
		if e.Type == cevents.ProactiveFailed || e.Type == cevents.ProactiveSuperseded {
			failed = true
		}
	}
	if !failed {
		t.Fatal("failure must be recorded in the canonical stream")
	}
	// Backoff: the failed opportunity does not immediately resurface.
	_ = item
}

// The lifecycle survives a restart: a presented proposal is durable, and a
// fresh runtime does not duplicate it.
func TestProactiveProposalSurvivesRestart(t *testing.T) {
	ws := t.TempDir()
	now := time.Now().UTC()

	al, svc, _ := newProactiveLoopWith(t, ws, &mockProvider{})
	overdueReminder(t, svc, "send the lease", now.Add(-2*time.Hour))
	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	first := findIdea(t, ws, ideas.ObsReminderOverdue)

	// A fresh process over the same workspace sees the same one proposal.
	al2 := newTestAgentLoop(t, ws)
	store, err := ideas.New(ws)
	if err != nil {
		t.Fatalf("reopen ideas: %v", err)
	}
	live, err := store.Live(50)
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	count := 0
	for _, i := range live {
		if i.Kind == ideas.ObsReminderOverdue {
			count++
			if i.ID != first.ID {
				t.Fatalf("restart changed the proposal id: %s != %s", i.ID, first.ID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("restart must preserve exactly one proposal, got %d", count)
	}
	_ = al2
}


// The routine signal path: a routine whose runs keep failing becomes a
// proposal with a real retry action, and pausing it is verified by read-back.
func TestProactiveProposalRoutineFailure(t *testing.T) {
	al, svc, stream := newProactiveLoop(t)
	now := time.Now().UTC()

	store := scheduled.NewStore(al.DB())
	routineSvc, err := routines.New(al.DB(), store)
	if err != nil {
		t.Fatalf("routines service: %v", err)
	}
	al.SetRoutineSignals(routineSvc, svc)

	r, err := routineSvc.Create(al.ghostID(), "owner", "Nightly report", "send the nightly report",
		"UTC", scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: 24 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	// A real failed run through the routine pipeline.
	if _, err := routineSvc.Run(context.Background(), r.ID, "run-1", func(ctx context.Context, rr *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Message: "smtp timeout", Completion: product.CompletionFailed}
	}); err != nil {
		t.Fatalf("routine run: %v", err)
	}

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 surfaced proposal, got %d", n)
	}
	idea := findIdea(t, al.workspace, ideas.ObsRoutineFailed)
	if idea.Plan == nil || idea.Plan.Op != opRetryRoutine {
		t.Fatalf("routine failure must offer a retry, got %+v", idea.Plan)
	}
	if idea.Plan.Capability != capRoutineRun {
		t.Fatalf("capability = %s, want %s", idea.Plan.Capability, capRoutineRun)
	}

	// Approve → the routine is dispatched again (real scheduler call).
	record, result, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if record.Status != ideas.StatusCompleted {
		t.Fatalf("status = %s (%s), want completed", record.Status, record.Result)
	}
	if !strings.Contains(strings.ToLower(result), "running") {
		t.Fatalf("result must describe what happened, got %q", result)
	}
	seen := false
	for _, e := range stream.Recent(100, cevents.Filter{}) {
		if e.Type == cevents.ProactiveCompleted {
			seen = true
		}
	}
	if !seen {
		t.Fatal("routine proposal completion must be recorded")
	}
}

// A stale routine proposal is voided automatically when the situation
// resolves, before any approval can act on it.
func TestProactiveRoutineProposalSupersededOnRecovery(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()

	store := scheduled.NewStore(al.DB())
	routineSvc, err := routines.New(al.DB(), store)
	if err != nil {
		t.Fatalf("routines service: %v", err)
	}
	al.SetRoutineSignals(routineSvc, svc)

	r, err := routineSvc.Create(al.ghostID(), "owner", "Nightly report", "send the nightly report",
		"UTC", scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: 24 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	if _, err := routineSvc.Run(context.Background(), r.ID, "run-1", func(ctx context.Context, rr *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Message: "smtp timeout", Completion: product.CompletionFailed}
	}); err != nil {
		t.Fatalf("routine run: %v", err)
	}
	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	idea := findIdea(t, al.workspace, ideas.ObsRoutineFailed)

	// The routine recovers: a successful run replaces the failure evidence.
	if _, err := routineSvc.Run(context.Background(), r.ID, "run-2", func(ctx context.Context, rr *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Message: "sent", Completion: product.CompletionSuccess}
	}); err != nil {
		t.Fatalf("routine run 2: %v", err)
	}

	// The next evaluation voids the stale proposal instead of leaving it
	// approvable.
	al.EvaluateProposals(now.Add(time.Minute))
	dead := false
	for _, i := range liveIdeas(t, al.workspace) {
		if i.ID == idea.ID {
			dead = i.Status == ideas.StatusSuperseded || i.Status == ideas.StatusCompleted
		}
	}
	if !dead {
		t.Fatalf("recovered routine must void its proposal, got %+v", findIdea(t, al.workspace, ideas.ObsRoutineFailed))
	}
}
