package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cards"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/routines"
	"github.com/ianclemence/ghost/pkg/scheduled"
	"github.com/ianclemence/ghost/pkg/tools"
)

// The proactive proposal runtime: NOTICE → REASON → PROPOSE → APPROVE → ACT →
// VERIFY → REMEMBER, built from primitives Ghost already has.
//
//   - NOTICE   pkg/ideas observations (proactive_observe.go), collected from
//              the scheduler, routine sidecar, goal store and job store.
//   - REASON   deterministic scoring + gate (pkg/ideas), then the existing
//              Permission Broker decision — never a model.
//   - PROPOSE  rendered from structured fields and delivered through the
//              existing noticer → outbound → suggestion-card path.
//   - APPROVE  the owner answers a bound broker request (card button, console,
//              or a chat yes) — the same request/approval machinery as any
//              other ask.
//   - ACT      the existing tool registry (or a thin adapter over the same
//              service the API calls), gated by the broker.
//   - VERIFY   the registry's own evidence contract plus explicit read-back
//              for local operations.
//   - REMEMBER the outcome is written to the idea record, the canonical event
//              stream, activity, and the session history.
//
// Nothing here invents authority. A proposal the model can influence never
// decides its own risk, and an approval only ever executes the exact action it
// was bound to, after the state it was built from is re-checked.

// Local operation ids. These are runtime vocabulary, not model output.
const (
	opRemindNow     = "remind_now"
	opRetryReminder = "retry_reminder"
	opRetryRoutine  = "retry_routine"
	opPauseRoutine  = "pause_routine"
	opGoalTurn      = "goal_turn"
	opTaskTurn      = "task_turn"

	// Commitment operations. Both are always executable as stated: a
	// delegated governed turn, or a reminder Ghost really creates.
	opCommitmentTurn   = "commitment_turn"
	opCommitmentRemind = "commitment_remind"
)

// Capability ids for local operations. They exist so the broker can reason
// about, deny, and audit them exactly like capability-backed tools; they are
// declared here by the runtime and never by a model.
const (
	capScheduleCreate = "schedule.create"
	capRoutineRun     = "routine.run"
	capRoutinePause   = "routine.pause"
	capAgentTurn      = "agent.turn"
)

// proposalSession is the session proactive proposals are bound to. It is the
// owner's main conversation, so a plain "yes" in chat resolves the same
// request a card button would.
const proposalSession = "main"

// planForObservation states the single concrete action Ghost offers for an
// observed situation. Every plan names its capability and risk up front: the
// broker sees exactly what the owner is being asked to allow, and nothing
// here can be widened later.
func (al *AgentLoop) planForObservation(o ideas.Observation) *ideas.Plan {
	switch o.Kind {
	case ideas.ObsReminderOverdue:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opRemindNow, Capability: capScheduleCreate,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "run it now",
		}
	case ideas.ObsReminderFailed:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opRetryReminder, Capability: capScheduleCreate,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "retry it now",
		}
	case ideas.ObsRoutineWaiting:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opRetryRoutine, Capability: capRoutineRun,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "run it again now",
		}
	case ideas.ObsRoutineFailed:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opRetryRoutine, Capability: capRoutineRun,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "retry it now",
		}
	case ideas.ObsRoutineStale:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opPauseRoutine, Capability: capRoutinePause,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "pause it",
		}
	case ideas.ObsGoalStalled:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opGoalTurn, Capability: capAgentTurn,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "pick a next step for it",
		}
	case ideas.ObsTaskOverdue:
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opTaskTurn, Capability: capAgentTurn,
			Args: map[string]string{"id": o.Subject}, Risk: ideas.RiskLow,
			Describe: "take care of it",
		}
	case ideas.ObsCommitmentDue, ideas.ObsCommitmentStalled:
		// The capability-aware planner owns obligation actions: it decides
		// whether Ghost can honestly offer anything at all.
		if al == nil || al.workspace == "" {
			return nil
		}
		store, err := al.commitmentStoreFor()
		if err != nil {
			return nil
		}
		c, err := store.Get(o.Subject)
		if err != nil {
			return nil
		}
		plan, ok := al.commitmentPlan(c)
		if !ok {
			return nil
		}
		return plan
	default:
		return nil
	}
}

func riskToBroker(r ideas.Risk) permissions.Risk {
	switch r {
	case ideas.RiskReadOnly:
		return permissions.RiskReadOnly
	case ideas.RiskLow:
		return permissions.RiskLow
	case ideas.RiskHighImpact:
		return permissions.RiskHighImpact
	default:
		// Unknown risk fails closed: the broker will ask.
		return permissions.RiskConsequential
	}
}

func argsToMap(in map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// observationKey is the stable identity of an observed opportunity. It never
// includes the state digest: the same situation is the same opportunity until
// it is answered or resolved.
func observationKey(o ideas.Observation) string {
	return ideas.DedupeKey(o.Kind, o.Subject, "")
}

// supersedeProposal voids a live proposal whose underlying situation changed
// or resolved. It cancels any bound broker request so a stale card cannot
// execute, and records the change in the canonical stream.
func (al *AgentLoop) supersedeProposal(store *ideas.Store, idea ideas.Idea, reason string, now time.Time) {
	superseded, err := store.Supersede(idea.ID, reason, now)
	if err != nil {
		return
	}
	al.cancelBound(idea)
	al.publishProactive(cevents.ProactiveSuperseded, superseded, reason)
}

// reconcileLiveProposals keeps the store honest: a proposal whose observation
// no longer holds, or whose state digest moved, is void before anything can be
// approved against it. This is what makes "the situation changed" a runtime
// fact rather than something the owner has to notice.
func (al *AgentLoop) reconcileLiveProposals(store *ideas.Store, obs []ideas.Observation, now time.Time) {
	current := make(map[string]string, len(obs))
	for _, o := range obs {
		current[observationKey(o)] = o.StateVer
	}
	live, err := store.Live(500)
	if err != nil {
		return
	}
	for i := range live {
		idea := live[i]
		if idea.DedupeKey == "" {
			continue
		}
		switch idea.Status {
		case ideas.StatusPending, ideas.StatusPresented, ideas.StatusSnoozed:
		default:
			continue
		}
		ver, ok := current[idea.DedupeKey]
		if !ok {
			al.supersedeProposal(store, idea, "The situation resolved itself, so this no longer needs you.", now)
			continue
		}
		if idea.StateVer != "" && ver != idea.StateVer {
			al.supersedeProposal(store, idea, "The situation changed, so this no longer applies.", now)
		}
	}
}

// EvaluateProposals runs the deterministic proactive pipeline once. It is
// cheap: bounded queries, no model call, and the existing gate absorbs
// repeats. Safe to call on every heartbeat tick.
//
// Returns how many proposals were surfaced (or acted on automatically).
func (al *AgentLoop) EvaluateProposals(now time.Time) int {
	if al == nil || al.workspace == "" {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	pol := proactive.Load(al.workspace)
	if !pol.Enabled {
		return 0
	}
	store, err := ideas.New(al.workspace)
	if err != nil {
		return 0
	}
	existing, err := store.Live(500)
	if err != nil {
		// A corrupt/unreadable store must not block the rest of the tick;
		// the next tick retries. Never execute from a failed read.
		return 0
	}

	// 1. Observed facts → durable candidates, deduplicated by opportunity.
	var allowed []ideas.Observation
	for _, o := range al.CollectObservations(now) {
		if !pol.CategoryAllowed(o.Kind.Category()) {
			continue
		}
		allowed = append(allowed, o)
	}
	ttl := time.Duration(pol.ProposalTTLHours) * time.Hour
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	// Resolve stale proposals first, so a changed situation cannot sit in the
	// store waiting to be approved.
	al.reconcileLiveProposals(store, allowed, now)

	byKey := make(map[string]ideas.Observation, len(allowed))
	for _, o := range allowed {
		byKey[observationKey(o)] = o
	}
	fresh := ideas.Candidates(allowed, existing, now, ideas.DefaultDedupeWindow())
	for i := range fresh {
		if o, ok := byKey[fresh[i].DedupeKey]; ok {
			fresh[i].Plan = al.planForObservation(o)
			// The risk class is decided by the runtime's plan, so the record
			// and every surface report what the broker will actually be asked.
			fresh[i].Risk = string(planRisk(fresh[i]))
		}
		exp := now.Add(ttl).UTC()
		fresh[i].ExpiresAt = &exp
	}
	if len(fresh) > 0 {
		if err := store.Add(fresh); err != nil {
			logger.InfoCF("agent", "proactive candidate store failed",
				map[string]interface{}{"error": err.Error()})
			return 0
		}
		for _, f := range fresh {
			al.publishProactive(cevents.ProactiveCandidate, f, "noticed")
		}
	}

	// 2. Decide what may reach the owner now.
	live, err := store.Live(500)
	if err != nil {
		return 0
	}
	gate := ideas.DefaultGate()
	surfaced := 0
	for i := range live {
		idea := live[i]
		if idea.Status == ideas.StatusSnoozed {
			if idea.SnoozedUntil == nil || now.Before(*idea.SnoozedUntil) {
				continue
			}
			promoted, err := store.Transition(idea.ID, []ideas.Status{ideas.StatusSnoozed}, func(x *ideas.Idea) error {
				x.Status = ideas.StatusPending
				x.SnoozedUntil = nil
				return nil
			})
			if err != nil {
				continue
			}
			idea = promoted
		}
		if idea.Status != ideas.StatusPending {
			continue
		}
		if idea.Expired(now) {
			al.expireProposal(store, idea, now)
			continue
		}
		if ok, _ := gate.ShouldSurface(idea, now); !ok {
			continue
		}
		if al.surfaceProposal(store, idea, pol, now) {
			surfaced++
		}
	}
	return surfaced
}

// expireProposal closes an unanswered proposal and cancels any broker request
// bound to it, so an expired card can never resolve into an execution.
func (al *AgentLoop) expireProposal(store *ideas.Store, idea ideas.Idea, now time.Time) {
	updated, err := store.Expire(idea.ID, now)
	if err != nil {
		return
	}
	if idea.PermissionRequestID != "" && al.governance != nil && al.governance.Broker != nil {
		_ = al.governance.Broker.Cancel(idea.PermissionRequestID)
	}
	al.publishProactive(cevents.ProactiveExpired, updated, "window closed")
}

// surfaceProposal decides whether a candidate may reach the owner, then
// delivers it or acts on it. It returns true when the owner was actually
// reached (or an autonomous action completed and was reported).
//
// Two things happen here, and neither creates an authorization:
//
//   - A dry-run policy check. A proposal Ghost's own policy forbids is never
//     shown as an offer it cannot keep. Nothing is written and no request is
//     created, so a suggestion that is never answered leaves no residue and
//     cannot go stale.
//   - The gate (priority, confidence, budget, cooldown, quiet hours).
//
// Authorization is deliberately NOT created here. An approval request minted
// now would expire long before an owner gets to it, which is how a product
// ends up saying "want me to do this?" and then "sorry, that silently
// expired". Authorization belongs to the moment of approval.
func (al *AgentLoop) surfaceProposal(store *ideas.Store, idea ideas.Idea, pol proactive.Policy, now time.Time) bool {
	verdict := al.proposalVerdict(idea)
	switch verdict {
	case permissions.DecisionDeny:
		failed, err := store.Fail(idea.ID, "denied", "I'm not allowed to do that, so I didn't suggest it.", now)
		if err == nil {
			al.publishProactive(cevents.ProactiveFailed, failed, "denied by policy")
		}
		logger.InfoCF("agent", "proactive proposal denied by policy",
			map[string]interface{}{"idea": idea.ID, "capability": planCapability(idea)})
		return false
	case permissions.DecisionAllow:
		// The permission policy explicitly permits this category without
		// asking (auto/full mode, or an existing standing grant). Act, verify,
		// report — and still record what happened.
		channel, chatID := al.proposalTarget(pol)
		_, _, err := al.runProposal(store, idea, channel, chatID, now)
		return err == nil
	}

	label := "Approve"
	if idea.Plan != nil && idea.Plan.Describe != "" {
		label = strings.ToUpper(idea.Plan.Describe[:1]) + idea.Plan.Describe[1:]
	}
	al.notifyProposal(idea, label, pol)
	return true
}

// proposalVerdict is a dry-run policy read: allow, ask, or deny. It writes
// nothing and creates no request. Unknown inputs fail closed (ask).
func (al *AgentLoop) proposalVerdict(idea ideas.Idea) permissions.Decision {
	if idea.Plan == nil {
		return permissions.DecisionAllow // informational, nothing executes
	}
	if al.governance == nil || al.governance.Broker == nil {
		return permissions.DecisionDeny // unwired governance fails closed
	}
	scope := scopeFor(proposalSession, argsToMap(idea.Plan.Args))
	return permissions.VerdictDecision(al.governance.Broker.Evaluate(
		idea.Plan.Capability, planAction(idea), scope, riskToBroker(idea.Plan.Risk)))
}

// planAction is the capability action a plan resolves to. It matches exactly
// what the broker will be asked at approval time, so a dry run and the real
// decision can never disagree about identity.
func planAction(idea ideas.Idea) string {
	if idea.Plan == nil {
		return ""
	}
	if strings.TrimSpace(idea.Plan.Tool) != "" {
		return toolAction(idea.Plan.Tool, argsToMap(idea.Plan.Args))
	}
	return idea.Plan.Op
}

// authorizeAndConsent converts the owner's approval into an authorized
// execution.
//
// The owner consented on an authenticated surface. The broker still decides —
// it may deny, or allow outright under a standing grant — and when it asks,
// this records that ask and the owner's answer exactly once, under the same
// identity a future identical action is evaluated under. That is the same
// transaction a chat "yes" performs; the consent simply arrived through the
// proposal instead of a phrase.
func (al *AgentLoop) authorizeAndConsent(idea ideas.Idea, store *ideas.Store) (bool, string, error) {
	if idea.Plan == nil {
		return true, "", nil
	}
	if al.governance == nil || al.governance.Broker == nil {
		return false, "", fmt.Errorf("permission governance is unavailable")
	}
	tool := idea.Plan.Tool
	if tool == "" {
		tool = idea.Plan.Op
	}
	args := argsToMap(idea.Plan.Args)
	res := al.governance.AuthorizeStandalone(
		idea.ID, proposalSession, idea.Plan.Capability, tool, args, riskToBroker(idea.Plan.Risk))
	if res.Allowed {
		return true, "", nil
	}
	if res.PendingID == "" {
		msg := strings.TrimSpace(res.AskMessage)
		if msg == "" {
			msg = "I couldn't prepare that action."
		}
		return false, "", fmt.Errorf("%s", msg)
	}
	// The ask exists so the decision is durable, scoped and auditable; the
	// owner has already consented, so it is resolved now — exactly once, via
	// the same code path as every other approval.
	if _, err := al.governance.Broker.ResolveAuto(res.PendingID, permissions.GrantOnce); err != nil {
		return false, "", err
	}
	if _, ok := al.governance.Broker.ConsumeApproved(idea.ID); !ok {
		return false, "", fmt.Errorf("that approval could not be recorded")
	}
	if _, err := store.Transition(idea.ID, []ideas.Status{idea.Status}, func(x *ideas.Idea) error {
		x.PermissionRequestID = res.PendingID
		return nil
	}); err == nil {
		idea.PermissionRequestID = res.PendingID
	}
	return true, res.PendingID, nil
}

func planCapability(idea ideas.Idea) string {
	if idea.Plan == nil {
		return ""
	}
	return idea.Plan.Capability
}

func planRisk(idea ideas.Idea) ideas.Risk {
	if idea.Plan == nil {
		return ideas.RiskReadOnly
	}
	return idea.Plan.Risk
}

// notifyProposal delivers a proposal through the single proactive throat
// (noticer gate → outbound → suggestion card), with the approve action bound
// to the exact broker request.
func (al *AgentLoop) notifyProposal(idea ideas.Idea, label string, pol proactive.Policy) {
	// A configured channel is honoured strictly. When the owner asked for one
	// channel and is not reachable there, hold the proposal instead of pushing
	// it somewhere they did not choose — and do not spend the gate's budget on
	// a notice that cannot go out.
	if pol.PreferredChannel != "" {
		if ch, chatID := al.proposalTarget(pol); ch == "" || chatID == "" {
			return
		}
	}
	// The card's buttons address the proposal by identity. No authorization
	// token rides on the card: it would expire while the proposal stayed
	// valid, and the owner would be asked to approve something that had
	// already lapsed. Approving mints and records the authorization then.
	actions := []cards.Action{}
	if idea.Plan != nil {
		actions = append(actions,
			cards.Action{ID: "approve", Label: label, Style: "primary"},
			cards.Action{ID: "deny", Label: "No thanks", Style: "destructive"},
		)
	}
	nt := Notice{
		Topic:      idea.DedupeKey,
		Priority:   idea.Priority,
		Urgency:    idea.Urgency,
		Confidence: idea.Confidence,
		DedupeKey:  idea.DedupeKey,
		Message:    ideas.Render(idea),
		ProposalID: idea.ID,
		Actions:    actions,
	}
	if al.MaybeNotify(nt) != DecisionNotify {
		return
	}
	// The gate let it through, so it is on its way: record the decision point.
	// The note is a fixed label — approval ask text can carry argument values
	// and must never reach the canonical stream.
	if store, err := ideas.New(al.workspace); err == nil {
		if updated, err := store.MarkPresented(idea.ID, time.Now().UTC()); err == nil {
			al.publishProactive(cevents.ProactivePresented, updated, "surfaced")
		}
	}
}

// proposalTarget resolves where a proposal goes. A configured preferred
// channel is honoured strictly: if the owner is not reachable there, delivery
// is held rather than pushed to a channel they did not choose.
func (al *AgentLoop) proposalTarget(pol proactive.Policy) (string, string) {
	if al.state == nil {
		return "", ""
	}
	channel, chatID := al.state.GetLastActiveSession()
	if pol.PreferredChannel != "" && channel != pol.PreferredChannel {
		return "", ""
	}
	return channel, chatID
}

// publishProactive records one lifecycle step in the canonical stream. The
// payload is structured and secret-free: it is what the console, activity,
// replay and diagnostics read.
func (al *AgentLoop) publishProactive(typ cevents.Type, idea ideas.Idea, note string) {
	if al.governance == nil || al.governance.Events == nil {
		return
	}
	payload := map[string]interface{}{
		"idea_id":    idea.ID,
		"kind":       string(idea.Kind),
		"category":   idea.Kind.Category(),
		"priority":   idea.Priority,
		"confidence": idea.Confidence,
		"actionable": idea.Plan != nil,
		"dedupe_key": idea.DedupeKey,
	}
	if note != "" {
		payload["note"] = note
	}
	// The activity chip reads "summary"; the observation itself is the most
	// honest one-line description of what was noticed or done.
	if summary := strings.TrimSpace(idea.Body); summary != "" {
		payload["summary"] = summary
	}
	if idea.Plan != nil {
		payload["capability"] = idea.Plan.Capability
		payload["risk"] = string(idea.Plan.Risk)
		payload["action"] = idea.Plan.Describe
	}
	ev := &cevents.Event{
		Type:      typ,
		SessionID: proposalSession,
		GhostID:   al.governance.GhostID,
		AgentID:   al.governance.AgentID,
		Status:    string(idea.Status),
		Payload:   payload,
	}
	if idea.PermissionRequestID != "" {
		ev.RequestID = idea.PermissionRequestID
	}
	al.governance.Events.Publish(ev)
}

// DecideIdea applies an owner decision from any surface. Approve resolves the
// bound broker request and executes; dismiss and snooze close the proposal
// without touching the world.
//
// It returns the updated idea plus the runtime's honest statement of what
// happened (which is empty for dismiss/snooze — nothing happened).
func (al *AgentLoop) DecideIdea(ctx context.Context, ref string, decision string, snoozeFor time.Duration) (ideas.Idea, string, error) {
	store, err := ideas.New(al.workspace)
	if err != nil {
		return ideas.Idea{}, "", err
	}
	idea, err := store.Get(ref)
	if err != nil {
		return ideas.Idea{}, "", err
	}
	now := time.Now().UTC()
	switch decision {
	case "dismiss":
		updated, err := store.Transition(idea.ID, []ideas.Status{ideas.StatusPending, ideas.StatusPresented, ideas.StatusSnoozed}, func(x *ideas.Idea) error {
			// A dismissed consequential proposal must not be executable from
			// a stale card: cancel its broker request.
			x.Status = ideas.StatusDismissed
			x.Outcome = "dismissed"
			t := now
			x.DecidedAt = &t
			return nil
		})
		if err != nil {
			return idea, "", err
		}
		al.cancelBound(idea)
		al.settleCommitmentFromProposal(updated, "dismissed", "owner said no for now")
		al.publishProactive(cevents.ProactiveDismissed, updated, "owner said no")
		return updated, "", nil
	case "snooze":
		if snoozeFor <= 0 {
			snoozeFor = 4 * time.Hour
		}
		updated, err := store.Snooze(idea.ID, now.Add(snoozeFor), now)
		if err != nil {
			return idea, "", err
		}
		al.cancelBound(idea)
		al.publishProactive(cevents.ProactiveSnoozed, updated, "owner deferred")
		return updated, "", nil
	case "approve":
		return al.approveProposal(ctx, store, idea, now)
	default:
		return idea, "", fmt.Errorf("unknown decision %q", decision)
	}
}

func (al *AgentLoop) cancelBound(idea ideas.Idea) {
	if idea.PermissionRequestID != "" && al.governance != nil && al.governance.Broker != nil {
		_ = al.governance.Broker.Cancel(idea.PermissionRequestID)
	}
}

// approveProposal is the APPROVE step: resolve the bound broker request and,
// only if the broker authorized it, execute. Everything else about the
// proposal — its arguments, its risk, its capability — comes from the stored
// record, never from the caller.
func (al *AgentLoop) approveProposal(ctx context.Context, store *ideas.Store, idea ideas.Idea, now time.Time) (ideas.Idea, string, error) {
	if !idea.Status.Undecided() {
		return idea, "", fmt.Errorf("that suggestion is already %s", idea.Status)
	}
	if idea.Expired(now) {
		expired, _ := store.Expire(idea.ID, now)
		al.cancelBound(idea)
		al.publishProactive(cevents.ProactiveExpired, expired, "approval arrived after the window")
		return expired, "", fmt.Errorf("that suggestion expired before you answered")
	}
	// The world may have moved on. Re-derive the observation and compare the
	// state digest: a changed situation voids the approval instead of acting
	// on stale information.
	if !al.proposalIsCurrent(idea, now) {
		superseded, _ := store.Supersede(idea.ID, "The situation changed, so I didn't act on it.", now)
		al.cancelBound(idea)
		al.publishProactive(cevents.ProactiveSuperseded, superseded, "state changed before approval")
		return superseded, "", fmt.Errorf("the situation changed, so I didn't act on it")
	}

	// Authorize now, not when the proposal was surfaced: the owner consented
	// just now, so the broker's decision is recorded just now and can never be
	// a lapsed token. A policy denial still stops execution here.
	allowed, _, authErr := al.authorizeAndConsent(idea, store)
	if !allowed {
		failed, _ := store.Fail(idea.ID, "denied", authErr.Error(), now)
		al.publishProactive(cevents.ProactiveFailed, failed, "not authorized")
		return failed, authErr.Error(), authErr
	}

	channel, chatID := al.proposalTarget(proactive.Load(al.workspace))
	record, result, err := al.runProposal(store, idea, channel, chatID, now)
	return record, result, err
}

// proposalIsCurrent re-derives the observation the proposal was built from and
// checks that it still exists with the same state digest.
func (al *AgentLoop) proposalIsCurrent(idea ideas.Idea, now time.Time) bool {
	if idea.DedupeKey == "" {
		return true
	}
	for _, o := range al.CollectObservations(now) {
		if observationKey(o) != idea.DedupeKey {
			continue
		}
		// Found the same opportunity. It is current unless the underlying
		// state digest moved.
		return idea.StateVer == "" || o.StateVer == idea.StateVer
	}
	// The opportunity no longer exists: whatever was true is now resolved.
	return false
}

// runProposal executes the stored plan under the broker's authority, verifies
// what happened, records the outcome and reports it. It is the single
// execution path for both owner-approved and policy-allowed (automatic)
// actions.
func (al *AgentLoop) runProposal(store *ideas.Store, idea ideas.Idea, channel, chatID string, now time.Time) (ideas.Idea, string, error) {
	// Claim exactly one execution. A second approval (or a duplicate card
	// tap) sees ErrStale and does nothing.
	claimed, err := store.BeginExecution(idea.ID, now)
	if err != nil {
		return idea, "", err
	}
	idea = claimed
	al.publishProactive(cevents.ProactiveApproved, idea, "authorized")

	outcome, result, ev, err := al.executePlan(context.Background(), idea, channel, chatID)
	done := outcome == outcomeVerified || outcome == outcomeSucceeded || outcome == outcomeDispatched || outcome == outcomeNeedsApproval
	if err != nil || !done {
		msg := result
		if msg == "" {
			msg = "It didn't work. Nothing was changed."
		}
		failed, ferr := store.Fail(idea.ID, string(outcome), msg, time.Now().UTC())
		if ferr == nil {
			if updated, uerr := store.Transition(failed.ID, []ideas.Status{ideas.StatusFailed}, func(x *ideas.Idea) error {
				x.EvidenceLevel = evidenceLevel(outcome)
				return nil
			}); uerr == nil {
				failed = updated
			}
			al.publishProactive(cevents.ProactiveFailed, failed, msg)
			al.settleCommitmentFromProposal(failed, string(outcome), msg)
			al.reportProposalOutcome(failed, msg, channel, chatID)
			return failed, msg, err
		}
		return idea, msg, err
	}

	completed, cerr := store.Complete(idea.ID, string(outcome), result, time.Now().UTC())
	if cerr != nil {
		return idea, result, cerr
	}
	if updated, uerr := store.Transition(completed.ID, []ideas.Status{ideas.StatusCompleted}, func(x *ideas.Idea) error {
		x.EvidenceLevel = evidenceLevel(outcome)
		return nil
	}); uerr == nil {
		completed = updated
	}
	al.publishProactiveWithEvidence(cevents.ProactiveCompleted, completed, result, ev)
	// Memory half of the loop: the obligation this proposal was about is
	// settled from the real outcome, never from the plan.
	al.settleCommitmentFromProposal(completed, string(outcome), result)
	al.reportProposalOutcome(completed, result, channel, chatID)
	return completed, result, nil
}

type planOutcome string

const (
	// outcomeVerified: the runtime read the result back from real state.
	outcomeVerified    planOutcome = "verified"
	outcomeSucceeded   planOutcome = "succeeded"
	outcomeDispatched  planOutcome = "dispatched"
	outcomeFailed      planOutcome = "failed"
	outcomeUnavailable planOutcome = "unavailable"
	// outcomeNeedsApproval: the work stopped on a permission request of its
	// own, which is progress, not failure.
	outcomeNeedsApproval planOutcome = "needs_approval"
	// outcomeBlocked: the work did not happen, and nothing failed either —
	// Ghost needs something from the owner (a recipient, a file, a channel).
	// It is not success and must never be recorded as one.
	outcomeBlocked planOutcome = "blocked"
)

// evidenceLevel maps an execution outcome to the honest strength of proof.
func evidenceLevel(o planOutcome) string {
	switch o {
	case outcomeVerified:
		return "verified"
	case outcomeSucceeded:
		return "acknowledged"
	case outcomeDispatched:
		return "dispatched"
	case outcomeUnavailable:
		return "unavailable"
	case outcomeBlocked:
		return "blocked"
	case outcomeNeedsApproval:
		return "waiting"
	default:
		return "failed"
	}
}

// executePlan runs the stored plan. Tool plans go through the registry
// (schema validation, grant check, evidence contract, verification); local
// plans go through a thin adapter over the same services the console API uses
// and are verified by read-back.
func (al *AgentLoop) executePlan(ctx context.Context, idea ideas.Idea, channel, chatID string) (planOutcome, string, map[string]interface{}, error) {
	if idea.Plan == nil {
		return outcomeSucceeded, "Noted.", nil, nil
	}
	switch idea.Plan.Kind {
	case ideas.PlanTool:
		return al.executeToolPlan(ctx, idea, channel, chatID)
	default:
		return al.executeLocalPlan(ctx, idea, channel, chatID)
	}
}

func (al *AgentLoop) executeToolPlan(ctx context.Context, idea ideas.Idea, channel, chatID string) (planOutcome, string, map[string]interface{}, error) {
	if al.tools == nil {
		return outcomeUnavailable, "That ability isn't available right now, so I didn't do anything.", nil, fmt.Errorf("tool registry unavailable")
	}
	tool := idea.Plan.Tool
	if _, ok := al.tools.Get(tool); !ok {
		return outcomeUnavailable, "That ability isn't available right now, so I didn't do anything.", nil, fmt.Errorf("tool %q not registered", tool)
	}
	// Stamp the grant the broker already issued for this exact call. The
	// registry's default-deny grant check is the last line of defence and
	// must see it.
	ctx = tools.GrantExec(ctx, tool)
	res := al.tools.ExecuteWithContext(ctx, tool, argsToMap(idea.Plan.Args), channel, chatID, proposalSession, nil)
	if res == nil {
		return outcomeFailed, "That didn't work. Nothing was changed.", nil, fmt.Errorf("no result")
	}
	if al.governance != nil {
		al.governance.ToolRan(idea.ID, proposalSession, tool, "", res.IsError, res.Obs)
	}
	if res.IsError || res.Err != nil {
		msg := strings.TrimSpace(res.ForUser)
		if msg == "" {
			msg = "That didn't work. Nothing was changed."
		}
		return outcomeFailed, msg, res.Evidence, res.Err
	}
	text := strings.TrimSpace(res.ForUser)
	if text == "" {
		text = strings.TrimSpace(res.ForLLM)
	}
	if text == "" {
		text = "Done."
	}
	return outcomeSucceeded, text, res.Evidence, nil
}

// executeLocalPlan runs a runtime operation through the same service the
// console API calls, then verifies the state actually changed.
func (al *AgentLoop) executeLocalPlan(ctx context.Context, idea ideas.Idea, channel, chatID string) (planOutcome, string, map[string]interface{}, error) {
	now := time.Now().UTC()
	switch idea.Plan.Op {
	case opRemindNow, opRetryReminder:
		if al.schedSvc == nil {
			return outcomeUnavailable, "The scheduler isn't available right now, so I didn't touch your reminder.", nil, fmt.Errorf("scheduler unavailable")
		}
		id := idea.Plan.Args["id"]
		item, err := al.schedSvc.GetItem(id)
		if err != nil || item == nil {
			return outcomeFailed, "I couldn't find that reminder any more, so I left it alone.", nil, err
		}
		item.State = scheduled.StateScheduled
		item.RetryCount = 0
		item.LastError = ""
		item.NextRunAt = &now
		if err := al.schedSvc.UpdateItem(item); err != nil {
			return outcomeFailed, "I couldn't reschedule that reminder. Nothing was changed.", nil, err
		}
		// Verify by read-back, not by the write returning.
		check, err := al.schedSvc.GetItem(id)
		if err != nil || check == nil || check.NextRunAt == nil || check.State != scheduled.StateScheduled {
			return outcomeFailed, "I scheduled it but couldn't confirm it, so treat it as unconfirmed.", nil, fmt.Errorf("verification failed")
		}
		return outcomeDispatched, fmt.Sprintf("Done — %q will go off within the minute.", itemTitle(check)),
			map[string]interface{}{"type": "state_transition", "entity": id, "requested": "scheduled", "observed": string(check.State)}, nil

	case opRetryRoutine:
		if al.schedSvc == nil {
			return outcomeUnavailable, "The scheduler isn't available right now, so I didn't touch your routine.", nil, fmt.Errorf("scheduler unavailable")
		}
		id := idea.Plan.Args["id"]
		item, err := al.schedSvc.GetItem(id)
		if err != nil || item == nil {
			return outcomeFailed, "I couldn't find that routine any more, so I left it alone.", nil, err
		}
		if err := al.schedSvc.RunNow(id); err != nil {
			return outcomeFailed, "I couldn't start that routine. Nothing was changed.", nil, err
		}
		return outcomeDispatched, fmt.Sprintf("Running %q now — you'll see the result in activity.", itemTitle(item)),
			map[string]interface{}{"type": "state_transition", "entity": id, "requested": "run_now", "observed": "dispatched"}, nil

	case opPauseRoutine:
		if al.routineSvc == nil {
			return outcomeUnavailable, "Routines aren't available right now, so I didn't change anything.", nil, fmt.Errorf("routines unavailable")
		}
		id := idea.Plan.Args["id"]
		if err := al.routineSvc.Pause(id); err != nil {
			return outcomeFailed, "I couldn't pause that routine. Nothing was changed.", nil, err
		}
		r, err := al.routineSvc.Get(id)
		if err != nil || r == nil || r.Status != routines.StatusPaused {
			return outcomeFailed, "I asked for the pause but couldn't confirm it.", nil, fmt.Errorf("verification failed")
		}
		return outcomeSucceeded, fmt.Sprintf("Paused %q — you can resume it any time.", displayName(r.Name, r.ID)),
			map[string]interface{}{"type": "state_transition", "entity": id, "requested": "paused", "observed": string(r.Status)}, nil

	case opGoalTurn:
		return al.executeDelegatedTurn(ctx, idea, channel, chatID,
			"One of my standing goals has gone quiet. Look at the goal in your goals list and either take the next concrete step or tell me it should be paused. Be specific and brief.")
	case opTaskTurn:
		return al.executeDelegatedTurn(ctx, idea, channel, chatID,
			"You have a background task waiting on a human. Look at it, do the smallest useful thing you can, and if you need something from me say exactly what.")
	case opCommitmentTurn:
		return al.executeCommitmentTurn(ctx, idea, channel, chatID)
	case opCommitmentRemind:
		return al.executeCommitmentRemind(ctx, idea, channel, chatID)
	default:
		return outcomeFailed, "I don't know how to do that, so I didn't.", nil, fmt.Errorf("unknown op %q", idea.Plan.Op)
	}
}

// executeDelegatedTurn runs a normal governed agent turn. Any consequential
// tool the model reaches for still goes through the broker on its own; this
// step only decides that Ghost should look.
func (al *AgentLoop) executeDelegatedTurn(ctx context.Context, idea ideas.Idea, channel, chatID, prompt string) (planOutcome, string, map[string]interface{}, error) {
	session := "proactive:" + idea.ID
	before := al.sessionMaxEventSeq(session)
	reply, err := al.ProcessDirectWithChannel(ctx, prompt, session, channel, chatID, nil, nil, nil)
	if err != nil {
		return outcomeFailed, "I couldn't finish looking into that.", nil, err
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return outcomeFailed, "I looked but couldn't find anything useful to do.", nil, nil
	}
	if al.governance != nil && al.governance.Broker != nil {
		if _, pending := al.governance.Broker.PendingForSession(session); pending {
			return outcomeNeedsApproval, reply, map[string]interface{}{
				"type": "action", "session": session, "waiting": "permission",
			}, nil
		}
	}
	// Talking is not doing: without a mutating tool the work did not happen.
	if !al.turnDidRealWork(session, before) {
		return outcomeBlocked, reply, map[string]interface{}{
			"type": "action", "session": session, "blocked": "needed input from the owner",
		}, nil
	}
	return outcomeVerified, reply, map[string]interface{}{"type": "action", "session": session}, nil
}

// reportProposalOutcome tells the owner what actually happened, in the
// runtime's own words — and records it in session history so future turns
// remember the follow-through.
func (al *AgentLoop) reportProposalOutcome(idea ideas.Idea, result, channel, chatID string) {
	result = strings.TrimSpace(result)
	if result == "" {
		return
	}
	// The sentence the owner reads is derived from how strong the proof is,
	// never from a model's claim. A dispatched action is not called done.
	prefix := "Done — "
	switch idea.EvidenceLevel {
	case "verified":
		prefix = "Done — "
	case "acknowledged":
		prefix = "Done — "
	case "dispatched":
		prefix = "I've set that in motion. "
	case "unavailable":
		prefix = "I couldn't do that. "
	case "blocked":
		prefix = "I couldn't finish that yet. "
	case "waiting":
		prefix = ""
	case "failed":
		prefix = "That didn't go through. "
	default:
		if idea.Outcome == "failed" {
			prefix = "That didn't go through. "
		}
	}
	msg := prefix + result
	if channel != "" && chatID != "" && al.bus != nil {
		al.bus.PublishOutbound(bus.OutboundMessage{Channel: channel, ChatID: chatID, Content: msg})
	}
	// Keep the outcome in the owner's conversation so the next turn knows.
	if al.sessions != nil {
		al.sessions.AddMessage(proposalSession, "assistant", msg)
		al.sessions.Save(proposalSession)
	}
}

func (al *AgentLoop) publishProactiveWithEvidence(typ cevents.Type, idea ideas.Idea, result string, ev map[string]interface{}) {
	al.publishProactive(typ, idea, result)
	if al.governance == nil || al.governance.Events == nil || idea.PermissionRequestID == "" {
		return
	}
	payload := map[string]interface{}{"idea_id": idea.ID}
	for k, v := range ev {
		payload[k] = v
	}
	al.governance.Events.Publish(&cevents.Event{
		Type: cevents.VerificationCompleted, RequestID: idea.PermissionRequestID,
		SessionID: proposalSession, Status: "verified",
		GhostID: al.governance.GhostID, AgentID: al.governance.AgentID,
		Payload: payload,
	})
}

// pendingProposalFor returns the newest undecided proposal waiting on the
// owner. Proposals are delivered to the owner's main conversation, so that is
// the session a chat reply is answered in.
func (al *AgentLoop) pendingProposalFor(sessionKey string) (ideas.Idea, bool) {
	if al == nil || al.workspace == "" || sessionKey != proposalSession {
		return ideas.Idea{}, false
	}
	store, err := ideas.New(al.workspace)
	if err != nil {
		return ideas.Idea{}, false
	}
	open, err := store.Open(50)
	if err != nil {
		return ideas.Idea{}, false
	}
	for i := range open {
		if open[i].Status == ideas.StatusPresented || open[i].Status == ideas.StatusPending {
			return open[i], true
		}
	}
	return ideas.Idea{}, false
}

// CheckProposalReply interprets a chat message as an answer to a surfaced
// proposal. It is the counterpart to CheckApprovalReply: proposals mint no
// authorization until they are approved, so there is no pending broker request
// for the phrase table to find — this looks up the proposal itself.
//
// Only a short, standalone phrase counts, so ordinary conversation containing
// the word "no" is never hijacked into a decision.
func (al *AgentLoop) CheckProposalReply(sessionKey, text string) (ideas.Idea, string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(text))
	if trimmed == "" || len(trimmed) > 24 || len(strings.Fields(trimmed)) > 3 {
		return ideas.Idea{}, "", false
	}
	grant, ok := approvalPhrases[trimmed]
	if !ok {
		return ideas.Idea{}, "", false
	}
	idea, ok := al.pendingProposalFor(sessionKey)
	if !ok {
		return ideas.Idea{}, "", false
	}
	if grant == permissions.GrantDeny {
		return idea, "dismiss", true
	}
	return idea, "approve", true
}
