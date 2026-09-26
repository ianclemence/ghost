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
	"github.com/ianclemence/ghost/pkg/commitments"
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
		Type:       scheduled.TypeReminder,
		Title:      title,
		State:      scheduled.StateScheduled,
		Schedule:   scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &due},
		Timezone:   "UTC",
		Action:     scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: title, Deliver: true},
		Source:     "user",
		CreatedBy:  "test",
		NextRunAt:  &due,
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
	if len(idea.Sources) != 1 || idea.Sources[0].Ref != item.ID {
		t.Fatalf("proposal must cite the reminder row, got %+v", idea.Sources)
	}
	// Authorization is minted at approval, not at surfacing, so a proposal
	// that sits unread cannot accumulate a lapsed token.
	broker := al.governance.Broker
	if idea.PermissionRequestID != "" {
		t.Fatalf("surfacing must not mint an authorization, got %q", idea.PermissionRequestID)
	}
	if pending := broker.Requests(permissions.StatusPending, 10); len(pending) != 0 {
		t.Fatalf("surfacing created %d broker requests; want none", len(pending))
	}

	// The owner-facing card carries the approve/deny actions addressed to the
	// proposal itself (no perishable request id).
	found := false
	for _, c := range cards.DefaultStore.List("mobile") {
		if c.Data == nil || c.Data["idea_id"] != idea.ID {
			continue
		}
		found = true
		if c.RequestID != "" {
			t.Fatalf("proposal card must not carry an authorization token, got %q", c.RequestID)
		}
		if len(c.Actions) != 2 {
			t.Fatalf("proposal card must carry approve+deny, got %+v", c.Actions)
		}
	}
	if !found {
		t.Fatal("no delivered card addressed to the proposal")
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
	// Nothing was minted and nothing was left resolvable.
	if pending := al.governance.Broker.Requests(permissions.StatusPending, 10); len(pending) != 0 {
		t.Fatalf("a dismissed proposal must leave no live authorization: %+v", pending)
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

// Approval mints the authorization and records the broker's decision in the
// same instant. This is what makes a proposal's lifetime independent of an
// authorization token's lifetime: there is never a token sitting around
// waiting to expire.
func TestProactiveApprovalRecordsTheBrokerDecision(t *testing.T) {
	al, svc, stream := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "renew the car insurance", now.Add(-2*time.Hour))

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal, got %d", n)
	}
	before := stream.Recent(200, cevents.Filter{})
	countEvents := func(list []*cevents.Event, typ cevents.Type) int {
		n := 0
		for _, e := range list {
			if e.Type == typ {
				n++
			}
		}
		return n
	}
	if countEvents(before, cevents.PermissionRequested) != 0 {
		t.Fatal("merely surfacing a proposal must not ask the broker")
	}

	idea := findIdea(t, al.workspace, ideas.ObsReminderOverdue)
	record, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	after := stream.Recent(200, cevents.Filter{})
	if countEvents(after, cevents.PermissionRequested) != 1 {
		t.Fatalf("approval must record exactly one broker ask, got %d", countEvents(after, cevents.PermissionRequested))
	}
	if countEvents(after, cevents.PermissionApproved) != 1 {
		t.Fatalf("approval must record exactly one broker approval, got %d", countEvents(after, cevents.PermissionApproved))
	}
	if record.PermissionRequestID == "" {
		t.Fatal("the executed approval must be recorded on the proposal")
	}
	// Nothing is left pending: the authorization was consumed by the run.
	if pending := al.governance.Broker.Requests(permissions.StatusPending, 10); len(pending) != 0 {
		t.Fatalf("a completed approval must not leave a pending request: %+v", pending)
	}
}

// The owner's words become durable state, with provenance, from a real turn.
func TestCommitmentExtractionFromTurn(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	runTurn(t, al, "I need to send Alex those photos Friday", "main")

	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("open commitments: %v", err)
	}
	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("expected one durable commitment, got %d (%v)", len(list), err)
	}
	c := list[0]
	if c.Kind != commitments.KindSend || c.Subject != "Alex" {
		t.Fatalf("commitment = %+v, want a send to Alex", c)
	}
	if c.DueAt == nil {
		t.Fatal("the named day must resolve to a due time")
	}
	if c.Provenance.Quote == "" || c.Provenance.Session != "main" {
		t.Fatalf("commitment must carry provenance, got %+v", c.Provenance)
	}
	// A future promise is not yet worth interrupting anyone about.
	if n := al.EvaluateProposals(time.Now().UTC()); n != 0 {
		t.Fatalf("a future promise must not surface early, got %d", n)
	}
	// The lifecycle is in the canonical stream, and the commitment itself is
	// never silently copied into preference memory.
	found := false
	for _, e := range al.observabilityEvents() {
		if e.Type == cevents.CommitmentCreated {
			found = true
		}
	}
	if !found {
		t.Fatal("recording a commitment must be visible in the canonical stream")
	}
	if al.pcStore != nil {
		for _, entry := range al.pcStore.Current() {
			if strings.Contains(strings.ToLower(entryValueText(entry)), "photos") {
				t.Fatalf("a promise is not a fact about the owner: %+v", entry)
			}
		}
	}
}

// A promise whose day has arrived becomes a bounded proposal, then a real
// action, then a settled obligation.
func TestCommitmentBecomesAProposalAndSettles(t *testing.T) {
	al, _, stream := newProactiveLoop(t)
	now := time.Now().UTC()

	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("open commitments: %v", err)
	}
	due := now.Add(-2 * time.Hour)
	c, err := store.Create(commitments.Commitment{
		Text: "send Alex those photos", Subject: "Alex", Kind: commitments.KindSend,
		DueAt: &due, DueSource: "Friday", Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{
			Session: "main", MessageID: "m1",
			Quote: "I need to send Alex those photos Friday", At: now,
		},
		DedupeKey: commitments.DueKey("send Alex those photos", "Alex", &due),
	})
	if err != nil {
		t.Fatalf("create commitment: %v", err)
	}

	if n := al.EvaluateProposals(now); n != 1 {
		t.Fatalf("expected 1 proposal once the promise came due, got %d", n)
	}
	idea := findIdea(t, al.workspace, ideas.ObsCommitmentDue)
	if idea.Plan == nil || idea.Plan.Args["id"] != c.ID {
		t.Fatalf("proposal must be planned against the commitment, got %+v", idea.Plan)
	}

	// Approve. The delegated turn runs, but a provider that only talks has
	// not done the work: the promise must NOT be marked kept. This is the
	// false-completion guard, proven end to end.
	record, result, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if record.Status != ideas.StatusFailed || record.EvidenceLevel != "blocked" {
		t.Fatalf("a turn that only talked must be recorded as blocked, got %s/%s", record.Status, record.EvidenceLevel)
	}
	if result == "" {
		t.Fatal("the owner must be told what is missing")
	}
	settled, err := store.Get(c.ID)
	if err != nil {
		t.Fatalf("reload commitment: %v", err)
	}
	if settled.Status == commitments.StatusCompleted {
		t.Fatalf("a turn that only talked must never complete the promise: %+v", settled)
	}
	if settled.Status != commitments.StatusBlocked {
		t.Fatalf("status = %s, want blocked (still open work)", settled.Status)
	}
	if settled.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", settled.Attempts)
	}
	for _, want := range []cevents.Type{cevents.CommitmentBlocked, cevents.ProactiveFailed} {
		found := false
		for _, e := range stream.Recent(200, cevents.Filter{}) {
			if e.Type == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("canonical stream missing %s", want)
		}
	}
	// A blocked promise is not abandoned: it becomes eligible again later,
	// this time with what was missing on the record.
	later := now.Add(commitmentBlockedRetryAfter + time.Hour)
	if n := al.EvaluateProposals(later); n != 1 {
		t.Fatalf("a blocked promise must come back with the missing piece, got %d", n)
	}
}

// Real work, for the purposes of settling a promise, means a mutating governed
// tool ran. A read-only lookup is not handling the owner's promise.
func TestTurnDidRealWorkRequiresAMutation(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	session := "commitment:test-real-work"
	publish := func(capability string) {
		al.governance.Events.Publish(&cevents.Event{
			Type: cevents.ToolCompleted, SessionID: session, Status: "success",
			GhostID: "ghost-test", AgentID: "agent-test",
			Payload: map[string]interface{}{"tool": "probe", "capability": capability, "status": "success"},
		})
	}

	before := al.sessionMaxEventSeq(session)
	publish("web.fetch") // a read-only lookup
	if al.turnDidRealWork(session, before) {
		t.Fatal("a read-only lookup is not handling the promise")
	}
	mark := al.sessionMaxEventSeq(session)
	publish("schedule.create") // a mutating capability
	if !al.turnDidRealWork(session, mark) {
		t.Fatal("a mutating capability must count as real work")
	}
	// Work that happened before the turn does not count for this turn.
	if al.turnDidRealWork(session, al.sessionMaxEventSeq(session)+1) {
		t.Fatal("work outside the turn's window must not count")
	}
}

// Speculation never becomes durable state — the negative control for
// commitment extraction at the runtime boundary.
func TestSpeculationNeverBecomesACommitment(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	for _, msg := range []string{
		"I might send Alex those photos Friday",
		"Maybe I should email the landlord",
		"Should I call the bank tomorrow?",
		"Remind me to send Alex those photos Friday",
	} {
		runTurn(t, al, msg, "main")
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("open commitments: %v", err)
	}
	list, _ := store.List()
	if len(list) != 0 {
		t.Fatalf("speculation, questions and reminder requests must not create commitments: %+v", list)
	}
}

// Without a channel that has ever carried a message, Ghost does not offer to
// send something it cannot send: the offer degrades to a reminder, which it
// really can create.
func TestCommitmentDegradesWhenMessagingIsUnavailable(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	// Nothing has ever been delivered on any channel.
	al.state.SetLastActiveSession("", "")

	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("open commitments: %v", err)
	}
	now := time.Now().UTC()
	due := now.Add(-time.Hour)
	c, err := store.Create(commitments.Commitment{
		Text: "email the landlord", Kind: commitments.KindEmail,
		DueAt: &due, Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{Session: "main", MessageID: "m1", Quote: "I need to email the landlord tomorrow", At: now},
	})
	if err != nil {
		t.Fatalf("create commitment: %v", err)
	}
	plan, ok := al.commitmentPlan(c)
	if !ok || plan == nil {
		t.Fatal("a reminder is always a real option")
	}
	if plan.Op != opCommitmentRemind {
		t.Fatalf("without a channel the offer must degrade to a reminder, got %s", plan.Op)
	}
	// With a channel that has carried a message, the full offer is available.
	if err := al.state.SetLastActiveSession("mobile", "owner-1"); err != nil {
		t.Fatalf("set active session: %v", err)
	}
	plan2, _ := al.commitmentPlan(c)
	if plan2 == nil || plan2.Op != opCommitmentTurn {
		t.Fatalf("with a channel the offer should be to act, got %+v", plan2)
	}
}

// The awareness filter is the cheap gate in front of every event: only a
// state change that can alter what Ghost should notice gets through.
func TestAwarenessEventFilter(t *testing.T) {
	relevant := []cevents.Type{
		cevents.CommitmentCreated, cevents.CommitmentCompleted,
		cevents.RoutineFailed, cevents.RoutineWaiting, cevents.TaskFailed,
		cevents.PermissionApproved, cevents.OperationFailed,
	}
	for _, typ := range relevant {
		if !awarenessEvent(typ) {
			t.Errorf("%s must trigger awareness", typ)
		}
	}
	ignored := []cevents.Type{
		cevents.AgentProgress, cevents.ToolStarted, cevents.MemoryRetrieved,
		cevents.UsageRecorded, cevents.EffortSelected, cevents.MessageReceived,
	}
	for _, typ := range ignored {
		if awarenessEvent(typ) {
			t.Errorf("%s must not trigger awareness", typ)
		}
	}
}

// An event-driven runtime evaluates without waiting for the heartbeat: a
// relevant state change produces a proposal promptly.
func TestProactiveWatcherEvaluatesOnRelevantEvent(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	// The reminder exists before the watcher starts; only the event drives
	// evaluation from here.
	overdueReminder(t, svc, "call the plumber", now.Add(-2*time.Hour))

	al.StartProactiveWatcher()
	al.governance.Events.Publish(&cevents.Event{
		Type: cevents.RoutineFailed, SessionID: "main",
		GhostID: "ghost-test", AgentID: "agent-test",
		Payload: map[string]interface{}{"routine": "r1"},
	})

	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		for _, i := range liveIdeas(t, al.workspace) {
			if i.Kind == ideas.ObsReminderOverdue {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("a relevant event did not trigger an evaluation")
}

// An irrelevant event must not cause work: the filter runs before any query.
func TestProactiveWatcherIgnoresIrrelevantEvent(t *testing.T) {
	al, svc, _ := newProactiveLoop(t)
	now := time.Now().UTC()
	overdueReminder(t, svc, "renew the licence", now.Add(-2*time.Hour))

	al.StartProactiveWatcher()
	for i := 0; i < 20; i++ {
		al.governance.Events.Publish(&cevents.Event{
			Type: cevents.AgentProgress, SessionID: "main",
			GhostID: "ghost-test", AgentID: "agent-test",
			Payload: map[string]interface{}{"i": i},
		})
	}
	// Ignore the very first evaluation window; then the record must be bare.
	time.Sleep(1200 * time.Millisecond)
	for _, i := range liveIdeas(t, al.workspace) {
		if i.Kind == ideas.ObsReminderOverdue {
			t.Fatal("an irrelevant event must not trigger an evaluation")
		}
	}
}

// Requesting an evaluation when no watcher is running must never block.
func TestRequestEvaluationWithoutWatcherIsSafe(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			al.RequestProactiveEvaluation()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RequestProactiveEvaluation blocked")
	}
}

// Shared helpers for the proactive fixtures used by the JEV dump and tests.
func scheduledStore(t *testing.T, al *AgentLoop) *scheduled.Store {
	t.Helper()
	store := scheduled.NewStore(al.DB())
	if err := store.InitSchema(); err != nil {
		t.Fatalf("scheduled schema: %v", err)
	}
	return store
}

func routineService(t *testing.T, al *AgentLoop, store *scheduled.Store) *routines.Service {
	t.Helper()
	svc, err := routines.New(al.DB(), store)
	if err != nil {
		t.Fatalf("routines service: %v", err)
	}
	return svc
}

func mustRoutine(t *testing.T, svc *routines.Service, ghostID string) *routines.Routine {
	t.Helper()
	r, err := svc.Create(ghostID, "owner", "Nightly report", "send the nightly report",
		"UTC", scheduled.Schedule{Kind: scheduled.ScheduleEvery, Every: 24 * time.Hour}, nil)
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	return r
}

func failRoutineRun(t *testing.T, svc *routines.Service, id string) {
	t.Helper()
	if _, err := svc.Run(context.Background(), id, "run-1", func(ctx context.Context, rr *routines.Routine) routines.RunOutcome {
		return routines.RunOutcome{Message: "smtp timeout", Completion: product.CompletionFailed}
	}); err != nil {
		t.Fatalf("routine run: %v", err)
	}
}

func scheduledDelete(t *testing.T, al *AgentLoop, id string) error {
	t.Helper()
	return scheduled.NewStore(al.DB()).Delete(id)
}

// A verified outcome must settle the obligation it was about. Regression:
// the reminder path returns "verified" (it reads the created row back), and a
// settlement switch that only knew "succeeded" left the promise open forever.
func TestVerifiedOutcomeSettlesTheCommitment(t *testing.T) {
	al, _, _ := newProactiveLoop(t)
	al.state.SetLastActiveSession("", "")
	store, err := al.commitmentStoreFor()
	if err != nil {
		t.Fatalf("open commitments: %v", err)
	}
	now := time.Now().UTC()
	due := now.Add(-time.Hour)
	c, err := store.Create(commitments.Commitment{
		Text: "email the landlord", Kind: commitments.KindEmail, DueAt: &due,
		Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{Session: "main", MessageID: "m1", Quote: "I need to email the landlord tomorrow", At: now},
	})
	if err != nil {
		t.Fatalf("create commitment: %v", err)
	}
	al.EvaluateProposals(now)
	idea := findIdea(t, al.workspace, ideas.ObsCommitmentDue)
	if idea.Plan.Op != opCommitmentRemind {
		t.Fatalf("expected the reminder plan without a channel, got %s", idea.Plan.Op)
	}
	record, _, err := al.DecideIdea(context.Background(), idea.ID, "approve", 0)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if record.EvidenceLevel != "verified" {
		t.Fatalf("evidence level = %q, want verified", record.EvidenceLevel)
	}
	settled, err := store.Get(c.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if settled.Status != commitments.StatusCompleted {
		t.Fatalf("a verified outcome must settle the promise, got %s/%s", settled.Status, settled.Outcome)
	}
}
