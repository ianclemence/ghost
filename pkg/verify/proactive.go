package verify

import (
	"errors"
	"time"

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
