package verify

import (
	"errors"
	"time"

	"github.com/ianclemence/ghost/pkg/commitments"
	"github.com/ianclemence/ghost/pkg/ideas"
)

// Proactive opportunity lifecycle: the deterministic guarantees that keep
// Ghost from nagging and keep a stale approval from acting. This check runs on
// a scratch workspace with no model and no network — it is the anti-spam and
// stale-approval contract, not the execution path (execution through the
// broker and the tool registry is proven by pkg/agent and the golden suite).
func checkProactiveLifecycle(e *Env) Check {
	store, err := ideas.New(e.Workspace)
	if err != nil {
		return fail("Proactive", "opportunity store opens", err.Error(), true)
	}
	now := time.Now().UTC()
	exp := now.Add(24 * time.Hour)
	plan := &ideas.Plan{
		Kind: ideas.PlanLocal, Op: "remind_now", Capability: "schedule.create",
		Risk: ideas.RiskLow, Describe: "run it now",
	}
	cand := ideas.Idea{
		ID: "verify-proposal-1", Title: "a reminder", Body: "Your reminder never reached you",
		Status: ideas.StatusPending, DedupeKey: "reminder_overdue:r1", StateVer: "v1",
		Kind: ideas.ObsReminderOverdue, Priority: 9, Confidence: 0.95,
		Plan: plan, ExpiresAt: &exp, CreatedAt: now,
		Sources: []ideas.Source{{Kind: ideas.SourceSchedule, Ref: "r1", Excerpt: "due long ago"}},
	}
	if err := store.Add([]ideas.Idea{cand}); err != nil {
		return fail("Proactive", "candidate persists", err.Error(), true)
	}

	// One live opportunity per dedupe key: re-observation must not duplicate.
	existing, err := store.Live(50)
	if err != nil {
		return fail("Proactive", "candidate reload", err.Error(), true)
	}
	dup := ideas.Candidates([]ideas.Observation{{
		Kind: ideas.ObsReminderOverdue, Subject: "r1", Summary: "same thing again",
		StateVer: "v1", Priority: 9, Confidence: 0.95, Actionable: true,
	}}, existing, now, ideas.DefaultDedupeWindow())
	if len(dup) != 0 {
		return fail("Proactive", "duplicate suppression", "the same opportunity was re-created", true)
	}

	// The gate only surfaces worthy, fresh, actionable things.
	gate := ideas.DefaultGate()
	if ok, reason := gate.ShouldSurface(existing[0], now); !ok {
		return fail("Proactive", "actionable proposal passes the gate", reason, true)
	}
	lowConf := existing[0]
	lowConf.Confidence = 0.2
	if ok, _ := gate.ShouldSurface(lowConf, now); ok {
		return fail("Proactive", "low-confidence is silenced", "a 0.2-confidence proposal surfaced", true)
	}
	stale := existing[0]
	past := now.Add(-time.Minute)
	stale.ExpiresAt = &past
	if ok, reason := gate.ShouldSurface(stale, now); ok {
		return fail("Proactive", "expired is never surfaced", reason, true)
	}
	// Advice with no action must clear a higher bar than an actionable offer.
	advice := existing[0]
	advice.Plan = nil
	advice.Priority = 7
	if ok, _ := gate.ShouldSurface(advice, now); ok {
		return fail("Proactive", "advice without an action stays quiet", "useless interruption allowed", true)
	}

	// Dismissing must suppress the same opportunity inside the cooldown.
	if _, err := store.Transition(cand.ID, []ideas.Status{ideas.StatusPending}, func(i *ideas.Idea) error {
		i.Status = ideas.StatusDismissed
		t := now
		i.DecidedAt = &t
		return nil
	}); err != nil {
		return fail("Proactive", "dismissal persists", err.Error(), true)
	}
	after, err := store.Live(50)
	if err != nil {
		return fail("Proactive", "reload after dismissal", err.Error(), true)
	}
	if quiet, _ := ideas.Quiet(after, cand.DedupeKey, now.Add(time.Hour), ideas.DefaultDedupeWindow()); !quiet {
		return fail("Proactive", "dismissal cooldown suppresses repeats", "a dismissed opportunity came back immediately", true)
	}
	if cand2 := ideas.Candidates([]ideas.Observation{{
		Kind: ideas.ObsReminderOverdue, Subject: "r1", Summary: "same thing again",
		StateVer: "v1", Priority: 9, Confidence: 0.95, Actionable: true,
	}}, after, now.Add(time.Hour), ideas.DefaultDedupeWindow()); len(cand2) != 0 {
		return fail("Proactive", "dismissed opportunity stays quiet", "re-created inside cooldown", true)
	}

	// Stale-approval protection: a transition from a status that no longer
	// holds must be refused, so a stale card cannot execute.
	if _, err := store.BeginExecution(cand.ID, now); !errors.Is(err, ideas.ErrStale) {
		return fail("Proactive", "answered proposal cannot start executing", "stale approval was accepted", true)
	}

	return pass("Proactive", "opportunity lifecycle (dedupe, gate, cooldown, stale-approval)")
}

// The durable commitment ledger: the bridge from conversation to proactive
// action. This check proves the grounding rules that keep Ghost from turning
// speculation or paraphrase into an obligation, and the settlement rules that
// keep a failed action from closing a promise.
func checkCommitmentLedger(e *Env) Check {
	store, err := commitments.New(e.Workspace)
	if err != nil {
		return fail("Commitments", "ledger opens", err.Error(), true)
	}
	now := time.Now().UTC()
	due := now.Add(-time.Hour)

	// A real promise is recorded with provenance.
	c, err := store.Create(commitments.Commitment{
		Text: "send Alex those photos", Subject: "Alex", Kind: commitments.KindSend,
		DueAt: &due, Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{Session: "main", MessageID: "m1", Quote: "I need to send Alex those photos Friday", At: now},
		DedupeKey:  commitments.DueKey("send Alex those photos", "Alex", &due),
	})
	if err != nil {
		return fail("Commitments", "promise recorded", err.Error(), true)
	}

	// No owner words, no obligation.
	if _, err := store.Create(commitments.Commitment{Text: "do the thing", Origin: "model"}); err == nil {
		return fail("Commitments", "provenance required", "an obligation without the owner's words was accepted", true)
	}

	// A restatement is the same promise, not a second one.
	again, err := store.Create(commitments.Commitment{
		Text: "send Alex those photos", Subject: "Alex", Kind: commitments.KindSend, DueAt: &due,
		Confidence: 0.9, Origin: "deterministic",
		Provenance: commitments.Provenance{Session: "main", MessageID: "m2", Quote: "I need to send Alex those photos Friday", At: now},
		DedupeKey:  commitments.DueKey("send Alex those photos", "Alex", &due),
	})
	if err != nil || again.ID != c.ID {
		return fail("Commitments", "restatement dedupes", "the same promise was recorded twice", true)
	}

	// Extraction grounding: speculation, questions, requests to Ghost and
	// explicit reminder asks never become obligations.
	for _, msg := range []string{
		"I might send Alex those photos Friday",
		"Maybe I should email the landlord",
		"Can you send the report tomorrow",
		"Remind me to send Alex those photos Friday",
	} {
		if got := commitments.ExtractDeterministic(msg, now, "UTC"); len(got) != 0 {
			return fail("Commitments", "speculation is not a promise", msg, true)
		}
	}
	// A real one is, with the day resolved by the runtime.
	good := commitments.ExtractDeterministic("I need to call the bank tomorrow", now, "UTC")
	if len(good) != 1 || good[0].DueAt == nil || good[0].Kind != commitments.KindCall {
		return fail("Commitments", "a real promise is extracted with its date", "extraction did not produce a dated call", true)
	}

	// A failed action leaves the promise open, not complete.
	if _, err := store.Settle(c.ID, commitments.StatusOpen, "failed", "the channel was unavailable"); err != nil {
		return fail("Commitments", "failure keeps the promise open", err.Error(), true)
	}
	got, _ := store.Get(c.ID)
	if got.Status != commitments.StatusOpen {
		return fail("Commitments", "failure keeps the promise open", "a failed action completed the promise", true)
	}
	// Completion is terminal.
	if _, err := store.Settle(c.ID, commitments.StatusCompleted, "completed", "sent"); err != nil {
		return fail("Commitments", "completion settles", err.Error(), true)
	}
	if _, err := store.Settle(c.ID, commitments.StatusOpen, "failed", "late failure"); err == nil {
		return fail("Commitments", "completion is terminal", "a settled promise reopened", true)
	}

	return pass("Commitments", "durable obligations (provenance, dedupe, grounding, settlement)")
}
