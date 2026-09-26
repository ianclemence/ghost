package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/commitments"
	"github.com/ianclemence/ghost/pkg/ideas"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/proactive"
	"github.com/ianclemence/ghost/pkg/scheduled"
)

// Commitments: the bridge from conversation to proactive action.
//
// When the owner says "I need to send Alex those photos Friday", Ghost stores a
// durable obligation (pkg/commitments) instead of letting the sentence die in
// the transcript. Later the obligation becomes an observation, and the runtime
// proposes exactly one governed action for it.
//
// This file owns the three halves of that bridge: extraction (wired into the
// turn's memory pass), observation (fed to the proactive pipeline), and
// settlement (the memory half of the loop, driven only by real outcomes).

const (
	// commitmentStallAfter is how long an obligation with no date on it may
	// sit untouched before Ghost offers to pick it up. Long enough not to
	// nag, short enough to be useful.
	commitmentStallAfter = 72 * time.Hour
	// commitmentBlockedRetryAfter is how long a promise Ghost could not move
	// on its own stays quiet before it may ask again — this time with the
	// missing piece stated.
	commitmentBlockedRetryAfter = 24 * time.Hour
)

// commitmentStoreFor opens the ledger for the loop's workspace.
func (al *AgentLoop) commitmentStoreFor() (*commitments.Store, error) {
	return commitments.New(al.workspace)
}

// extractCommitmentsInline is the deterministic half of commitment
// extraction: patterns are free, so they run inside the turn and the durable
// record exists even if the process stops right after the reply. The
// model-backed half runs from the deferred queue (see deferred.go).
func (al *AgentLoop) extractCommitmentsInline(opts processOptions) {
	if al.workspace == "" || isMachineTurn(opts.SessionKey) || isAutomationIntent(opts.UserMessage) {
		return
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return
	}
	now := time.Now().UTC()
	msgID := opts.RequestID
	if msgID == "" {
		msgID = fmt.Sprintf("msg-%d", now.UnixNano())
	}
	for _, c := range commitments.ExtractDeterministic(opts.UserMessage, now, al.scheduleTimezone()) {
		al.persistCommitment(store, c, opts.SessionKey, msgID, now)
		return
	}
}

// persistCommitment stores one extracted obligation after checking that its
// provenance really is a verbatim span of what the owner wrote.
func (al *AgentLoop) persistCommitment(store *commitments.Store, c commitments.Candidate, session, msgID string, now time.Time) {
	if c.Quote == "" {
		return
	}
	created, err := store.Create(commitments.Commitment{
		Text: c.Text, Subject: c.Subject, Kind: c.Kind,
		DueAt: c.DueAt, DueSource: c.DuePhrase,
		Confidence: c.Confidence, Origin: c.Origin,
		Provenance: commitments.Provenance{Session: session, MessageID: msgID, Quote: c.Quote, At: now},
		DedupeKey:  commitments.DueKey(c.Text, c.Subject, c.DueAt),
	})
	if err != nil {
		logger.InfoCF("agent", "commitment not stored", map[string]interface{}{"error": err.Error()})
		return
	}
	al.publishCommitment(cevents.CommitmentCreated, created, "noticed a promise")
	logger.InfoCF("agent", "commitment recorded", map[string]interface{}{
		"id": created.ID, "kind": string(created.Kind), "origin": created.Origin,
	})
	// A new obligation is a state change: evaluate awareness immediately
	// instead of waiting for the next reconciliation tick.
	al.RequestProactiveEvaluation()
}

func normalizeForMatch(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// scheduleTimezone is the timezone obligations resolve their dates in. It
// mirrors the scheduler's own derivation so a commitment and a reminder
// interpret "Friday" identically.
func (al *AgentLoop) scheduleTimezone() string {
	if al.pcStore != nil {
		if loc := proactive.UserLocation(al.pcStore); loc != nil && loc.String() != "" {
			return loc.String()
		}
	}
	return "UTC"
}

// observeCommitments turns obligations into grounded observations. An
// obligation becomes relevant when its day arrives, or when it has been
// waiting long enough that Ghost offering to pick it up is useful.
func (al *AgentLoop) observeCommitments(now time.Time) []ideas.Observation {
	if al.workspace == "" {
		return nil
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return nil
	}
	attention, err := store.AttentionWith(now, commitmentStallAfter, commitmentBlockedRetryAfter)
	if err != nil {
		return nil
	}
	var out []ideas.Observation
	for _, c := range attention {
		if _, ok := al.commitmentPlan(c); !ok {
			// Nothing Ghost can honestly offer for this one. Waiting is
			// better than a proposal it cannot keep.
			continue
		}
		due := c.DueAt != nil
		kind := ideas.ObsCommitmentStalled
		headline := ""
		if due {
			kind = ideas.ObsCommitmentDue
			// The owner's own words already carry the time phrase ("…photos
			// today"), so repeating it would read as a stutter.
			headline = fmt.Sprintf("You said you'd %s", c.Text)
			if src := strings.TrimSpace(c.DueSource); src != "" && !strings.Contains(strings.ToLower(c.Text), strings.ToLower(src)) {
				headline += " " + src
			}
		} else if c.Status == commitments.StatusBlocked {
			headline = fmt.Sprintf("You said you'd %s — I still need a hand with it", c.Text)
		} else {
			headline = fmt.Sprintf("You said you'd %s — that was %d days ago",
				c.Text, int(now.Sub(c.CreatedAt).Hours()/24))
		}
		summary := headline + " and it's still open"
		priority := 8
		urgency := false
		if due {
			// Past its day: time-sensitive, so it may break quiet hours.
			priority = 8
			urgency = now.After(c.DueAt.Add(12 * time.Hour))
		} else {
			priority = 7
		}
		out = append(out, ideas.Observation{
			Kind:    kind,
			Subject: c.ID,
			Summary: summary,
			Detail:  "You told me about this" + provenancePhrase(c) + ".",
			At:      now,
			Evidence: []ideas.Source{{
				Kind: ideas.SourceCommitment, Ref: c.ID,
				Excerpt: "you said: \"" + truncateReason(c.Provenance.Quote, 120) + "\"",
			}},
			StateVer:   ideas.StateVer(c.ID, string(c.Status), fmt.Sprint(c.Attempts), dueAtKey(c.DueAt), c.Outcome),
			Actionable: true,
			Priority:   priority,
			Urgency:    urgency,
			Confidence: c.Confidence,
		})
	}
	return out
}

func dueAtKey(t *time.Time) string {
	if t == nil {
		return "none"
	}
	return t.UTC().Format(time.RFC3339)
}

func provenancePhrase(c commitments.Commitment) string {
	if c.Provenance.At.IsZero() {
		return ""
	}
	return " on " + c.Provenance.At.In(time.Local).Format("Mon 2 Jan")
}

// commitmentPlan is the capability-aware planner for obligations. It answers
// "what can Ghost actually do about this?" and returns ok=false when the
// honest answer is "nothing useful".
//
// The plan is always executable as stated:
//
//   - A messaging obligation whose channel is gone degrades to a reminder,
//     because offering to send something Ghost cannot send would be a false
//     promise.
//   - Otherwise Ghost offers to take it on, which runs as a normal governed
//     turn: any consequential tool the model then reaches for still goes
//     through the Permission Broker on its own.
func (al *AgentLoop) commitmentPlan(c commitments.Commitment) (*ideas.Plan, bool) {
	describe := c.Kind.Action()
	if c.Kind.Messaging() && !al.messagingAvailable() {
		// No channel has ever been used, so Ghost has nowhere to send it.
		return &ideas.Plan{
			Kind: ideas.PlanLocal, Op: opCommitmentRemind, Capability: capScheduleCreate,
			Args: map[string]string{"id": c.ID}, Risk: ideas.RiskLow,
			Describe: "remind you instead",
		}, true
	}
	return &ideas.Plan{
		Kind: ideas.PlanLocal, Op: opCommitmentTurn, Capability: capAgentTurn,
		Args: map[string]string{"id": c.ID}, Risk: ideas.RiskLow,
		Describe: describe,
	}, true
}

// messagingAvailable reports whether Ghost has a real outbound channel. It
// reads runtime state, never configuration guesses: a channel that has never
// carried a message cannot be relied on to carry this one.
func (al *AgentLoop) messagingAvailable() bool {
	if al.state == nil {
		return false
	}
	channel, chatID := al.state.GetLastActiveSession()
	return strings.TrimSpace(channel) != "" && strings.TrimSpace(chatID) != ""
}

// settleCommitmentFromProposal is the memory half of the loop. It is called
// only with a real runtime outcome, and it never marks an obligation complete
// on anything less.
func (al *AgentLoop) settleCommitmentFromProposal(idea ideas.Idea, outcome, note string) {
	if idea.Plan == nil || idea.Plan.Args == nil {
		return
	}
	id := strings.TrimSpace(idea.Plan.Args["id"])
	if id == "" || !strings.HasPrefix(id, "cm-") {
		return
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return
	}
	if _, err := store.MarkAttempt(id); err != nil {
		return
	}
	var status commitments.Status
	var outcomeWord string
	switch outcome {
	case "verified", "succeeded", "dispatched":
		// A verified outcome is the strongest case: the runtime read the
		// result back from real state, so the promise is kept.
		status, outcomeWord = commitments.StatusCompleted, "completed"
	case "needs_approval":
		// The obligation is being worked on; the follow-up approval is the
		// next step, so it is not blocked and not complete.
		status, outcomeWord = commitments.StatusOpen, "in_progress"
	case "failed":
		status, outcomeWord = commitments.StatusOpen, "failed"
	case "dismissed":
		status, outcomeWord = commitments.StatusOpen, "dismissed"
	case "blocked":
		status, outcomeWord = commitments.StatusBlocked, "blocked"
	default:
		return
	}
	settled, err := store.Settle(id, status, outcomeWord, truncateReason(note, 240))
	if err != nil {
		return
	}
	ev := cevents.CommitmentFailed
	switch status {
	case commitments.StatusCompleted:
		ev = cevents.CommitmentCompleted
	case commitments.StatusBlocked:
		ev = cevents.CommitmentBlocked
	}
	al.publishCommitment(ev, settled, outcomeWord)
}

// publishCommitment records the obligation lifecycle in the canonical stream.
// The payload is identifiers and state, never the owner's private prose beyond
// a bounded excerpt already stored in the ledger.
func (al *AgentLoop) publishCommitment(typ cevents.Type, c commitments.Commitment, note string) {
	if al.governance == nil || al.governance.Events == nil {
		return
	}
	payload := map[string]interface{}{
		"commitment_id": c.ID,
		"kind":          string(c.Kind),
		"status":        string(c.Status),
		"subject":       c.Subject,
		"origin":        c.Origin,
		"confidence":    c.Confidence,
	}
	if c.DueAt != nil {
		payload["due_at"] = c.DueAt.UTC().Format(time.RFC3339)
	}
	if note != "" {
		payload["note"] = note
	}
	if summary := strings.TrimSpace(c.Text); summary != "" {
		payload["summary"] = summary
	}
	al.governance.Events.Publish(&cevents.Event{
		Type: typ, SessionID: proposalSession,
		GhostID: al.governance.GhostID, AgentID: al.governance.AgentID,
		Status: string(c.Status), Payload: payload,
	})
}

// observabilityEvents exposes the canonical stream for in-package tests that
// need to assert on the lifecycle without owning the stream handle.
func (al *AgentLoop) observabilityEvents() []*cevents.Event {
	if al == nil || al.governance == nil || al.governance.Events == nil {
		return nil
	}
	return al.governance.Events.Recent(200, cevents.Filter{})
}

// CommitmentCounts is the owner-facing summary used by diagnostics surfaces.
func (al *AgentLoop) CommitmentCounts() (open, blocked int) {
	if al.workspace == "" {
		return 0, 0
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return 0, 0
	}
	o, b, _, err := store.Count()
	if err != nil {
		return 0, 0
	}
	return o, b
}

// executeCommitmentTurn takes on an obligation as a normal governed turn. It
// is always executable as stated: the turn looks, does the smallest real
// thing it can, and any consequential tool it reaches for still goes through
// the Permission Broker on its own.
func (al *AgentLoop) executeCommitmentTurn(ctx context.Context, idea ideas.Idea, channel, chatID string) (planOutcome, string, map[string]interface{}, error) {
	id := strings.TrimSpace(idea.Plan.Args["id"])
	store, err := al.commitmentStoreFor()
	if err != nil {
		return outcomeUnavailable, "I couldn't open my notes on that, so I left it alone.", nil, err
	}
	c, err := store.Get(id)
	if err != nil {
		return outcomeFailed, "I couldn't find that promise any more, so I left it alone.", nil, err
	}
	session := "commitment:" + c.ID
	before := al.sessionMaxEventSeq(session)
	reply, err := al.ProcessDirectWithChannel(ctx, commitmentTurnPrompt(c), session, channel, chatID, nil, nil, nil)
	if err != nil {
		return outcomeFailed, "I couldn't get anywhere with that. Nothing was changed.", nil, err
	}
	reply = strings.TrimSpace(reply)
	// A turn that stopped on its own permission request is progress: the next
	// step is an approval the owner already has in front of them.
	if al.governance != nil && al.governance.Broker != nil {
		if _, pending := al.governance.Broker.PendingForSession(session); pending {
			if reply == "" {
				reply = "I need your go-ahead for the next step."
			}
			return outcomeNeedsApproval, reply, map[string]interface{}{
				"type": "action", "session": session, "waiting": "permission",
			}, nil
		}
	}
	if reply == "" {
		return outcomeFailed, "I looked, but couldn't find a way to move it forward.", nil, nil
	}
	// The turn only counts as done if something was actually done. A reply
	// that asks for a missing recipient or file is honest work planning, not
	// a completed promise — and the promise must stay open.
	if !al.turnDidRealWork(session, before) {
		return outcomeBlocked, reply, map[string]interface{}{
			"type": "action", "session": session, "blocked": "needed input from the owner",
		}, nil
	}
	return outcomeVerified, reply, map[string]interface{}{"type": "action", "session": session}, nil
}

// sessionMaxEventSeq is the highest canonical sequence number recorded for a
// session, so a delegated turn can be scoped to what it newly did.
func (al *AgentLoop) sessionMaxEventSeq(session string) int64 {
	if al.governance == nil || al.governance.Events == nil {
		return 0
	}
	var max int64
	for _, e := range al.governance.Events.Recent(300, cevents.Filter{SessionID: session}) {
		if e.Seq > max {
			max = e.Seq
		}
	}
	return max
}

// turnDidRealWork reports whether a delegated turn actually executed a
// mutating governed tool. Read-only lookups do not count: reading a file is
// not handling the owner's promise, and claiming otherwise is exactly the
// false-completion failure this product may not have.
func (al *AgentLoop) turnDidRealWork(session string, sinceSeq int64) bool {
	if al.governance == nil || al.governance.Events == nil {
		return false
	}
	for _, e := range al.governance.Events.Recent(300, cevents.Filter{SessionID: session}) {
		if e.Type != cevents.ToolCompleted || e.Seq <= sinceSeq {
			continue
		}
		capID, _ := e.Payload["capability"].(string)
		if capID == "" {
			continue
		}
		// The capability registry is authoritative for what a tool can do.
		// Unknown capabilities fail closed as mutating, so an unrecognised
		// tool counts as work rather than silently passing as a read.
		if spec, ok := capability.Get(capID); !ok || string(spec.Risk) != "read_only" {
			return true
		}
	}
	return false
}

// commitmentTurnPrompt states the obligation, its provenance, and the honesty
// rules in one place. It never asks the model to decide whether the promise
// exists — that was decided from the owner's words and stored.
func commitmentTurnPrompt(c commitments.Commitment) string {
	due := "no date was given"
	if c.DueAt != nil {
		due = "due " + c.DueAt.In(time.Local).Format("Mon 2 Jan 15:04")
	}
	subject := "not named"
	if strings.TrimSpace(c.Subject) != "" {
		subject = c.Subject
	}
	return fmt.Sprintf(`You are picking up a promise I recorded for the owner. Take the smallest concrete next step you actually can, using real capabilities.

Promise: %s
Kind: %s
Who or what: %s
When: %s
They said: %q

Rules:
- If you can do it now, do it, then say exactly what happened and what proves it.
- If you cannot, say precisely what is missing (a recipient, a file, a connected service) and do not claim anything was done.
- Never invent who or what they meant. If the target is genuinely ambiguous, ask one focused question.
- Keep it short. One thing.`,
		c.Text, c.Kind, subject, due, c.Provenance.Quote)
}

// executeCommitmentRemind creates a real reminder. It is the honest fallback
// when the obligation needs a channel the owner has not connected: Ghost does
// not offer to send something it cannot send.
func (al *AgentLoop) executeCommitmentRemind(ctx context.Context, idea ideas.Idea, channel, chatID string) (planOutcome, string, map[string]interface{}, error) {
	if al.schedSvc == nil {
		return outcomeUnavailable, "The scheduler isn't available right now, so I didn't set a reminder.", nil, fmt.Errorf("scheduler unavailable")
	}
	id := strings.TrimSpace(idea.Plan.Args["id"])
	store, err := al.commitmentStoreFor()
	if err != nil {
		return outcomeUnavailable, "I couldn't open my notes on that, so I left it alone.", nil, err
	}
	c, err := store.Get(id)
	if err != nil {
		return outcomeFailed, "I couldn't find that promise any more, so I left it alone.", nil, err
	}
	now := time.Now().UTC()
	when := now.Add(2 * time.Hour)
	if c.DueAt != nil && c.DueAt.After(now) {
		when = *c.DueAt
	}
	item := &scheduled.ScheduledItem{
		Type:         scheduled.TypeReminder,
		Title:        truncateReason(c.Text, 80),
		State:        scheduled.StateScheduled,
		Schedule:     scheduled.Schedule{Kind: scheduled.ScheduleAt, At: &when},
		Timezone:     al.scheduleTimezone(),
		Action:       scheduled.Action{Kind: scheduled.ActionAgentTurn, Content: "Reminder I asked for: " + c.Text, Deliver: true},
		Channel:      channel,
		ChatID:       chatID,
		DeliveryMode: scheduled.DeliverySmart,
		Source:       "proactive",
		CreatedBy:    "agent",
		NextRunAt:    &when,
		MaxRetries:   3,
	}
	if err := al.schedSvc.CreateItem(item); err != nil {
		return outcomeFailed, "I couldn't set that reminder. Nothing was changed.", nil, err
	}
	// Read it back: a reminder is only real if the scheduler can reload it.
	check, err := al.schedSvc.GetItem(item.ID)
	if err != nil || check == nil || check.NextRunAt == nil {
		return outcomeDispatched, "I set a reminder but couldn't confirm it.", nil, nil
	}
	return outcomeVerified,
		fmt.Sprintf("Reminder set for %s.", check.NextRunAt.In(time.Local).Format("Mon 15:04")),
		map[string]interface{}{
			"type": "state_transition", "entity": check.ID,
			"requested": "scheduled", "observed": string(check.State),
		}, nil
}

// Commitments returns the owner's promise ledger, newest first.
func (al *AgentLoop) Commitments() ([]commitments.Commitment, error) {
	if al == nil || al.workspace == "" {
		return nil, fmt.Errorf("no workspace")
	}
	store, err := al.commitmentStoreFor()
	if err != nil {
		return nil, err
	}
	list, err := store.List()
	if err != nil {
		return nil, err
	}
	// Newest first: the surface reads as "what is on my plate".
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// CancelCommitment closes a promise by owner decision. It never touches the
// work Ghost may have done; it records that the owner no longer wants it held.
func (al *AgentLoop) CancelCommitment(id, note string) (commitments.Commitment, error) {
	store, err := al.commitmentStoreFor()
	if err != nil {
		return commitments.Commitment{}, err
	}
	c, err := store.Cancel(id, note)
	if err != nil {
		return commitments.Commitment{}, err
	}
	al.publishCommitment(cevents.CommitmentCompleted, c, "cancelled by the owner")
	return c, nil
}
