package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/turnlog"
)

// Restart continuity.
//
// When the process died mid-reply the words already spoken lived only in
// memory, so the thread came back with the question still there and no
// answer — durable context intact, working thread frayed. Recovery does two
// different things, deliberately:
//
//   - A turn still worth finishing (recent, still the newest thing in its
//     conversation) is FINISHED. The owner asked; the crash is a pause.
//   - Anything else has its partial reply written into the transcript,
//     marked cut short, so nothing said is ever lost.
//
// Nothing is silently dropped in either branch, and no turn is planned
// twice: deciding is a pure function of durable state, so a second crash
// mid-recovery replans from the record rather than repeating a side effect.

// defaultResumeWindow is how long after the last sign of life an
// interrupted turn is still worth finishing. Past it the owner has moved on
// and an answer arriving now answers a question nobody is waiting on —
// that turn's words go into the transcript instead.
const defaultResumeWindow = 20 * time.Minute

// resumeSettle gives boot a moment to finish wiring (provider, channels,
// scheduler) before a resumed turn starts calling tools.
const resumeSettle = 3 * time.Second

// resumeWindowFor reads the freshness window. GHOST_RESUME_WINDOW takes a
// duration (\"0s\" disables finishing and always falls back to writing the
// partial out); GHOST_NO_AUTO_RESUME=1 is the plain off switch.
func resumeWindowFor() time.Duration {
	if v := strings.TrimSpace(os.Getenv("GHOST_RESUME_WINDOW")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
		logger.WarnCF("internal-api", "unparseable GHOST_RESUME_WINDOW", map[string]interface{}{"value": v})
	}
	if v := strings.TrimSpace(os.Getenv("GHOST_NO_AUTO_RESUME")); v == "1" || strings.EqualFold(v, "true") {
		return 0
	}
	return defaultResumeWindow
}

// failedPartialKeep is how long a reply that FAILED live (rather than being
// cut off) stays worth putting back into the transcript. It was shown to
// whoever was watching at the time, so the words are already in the turn
// record; what recovery must not do is drop a fragment of an old failure
// into the conversation days later, where it reads as a ghost.
const failedPartialKeep = 6 * time.Hour

// decidedByOwner reports whether the turn was waiting on the owner when the
// process died. The owner owes Ghost a decision, not the other way round:
// re-driving it would re-issue an approval the owner never answered (and
// could raise a second card beside the one that died), so those turns have
// their words written out instead of being finished.
func decidedByOwner(t *turnlog.Turn) bool {
	return strings.HasPrefix(strings.TrimSpace(t.Outcome), "waiting")
}

func partialKey(t *turnlog.Turn) string { return t.SessionID + "\x00" + t.RequestID }

// resumeJob is one interrupted turn queued to be finished, under a fresh
// request id: the interrupted claim stays terminal, so idempotency still
// holds and a reconnect can never start a second execution.
type resumeJob struct {
	turn     *turnlog.Turn
	resumeID string
}

// recoveryPlan is the decision, before any of it runs.
type recoveryPlan struct {
	// materialize are replies that will be written into the transcript.
	materialize []*turnlog.Turn
	// finish are replies a restarted process will complete instead.
	finish []resumeJob
}

// planRecovery decides from durable state alone. A turn can land in exactly
// one branch: whatever is finished must not also be written out, or the
// owner would read the half and the whole.
func planRecovery(st *turnlog.Store, now time.Time, window time.Duration) (recoveryPlan, error) {
	var plan recoveryPlan

	finish := map[string]bool{}
	if window > 0 {
		candidates, err := st.Resumable(now, window)
		if err != nil {
			return plan, err
		}
		for _, t := range candidates {
			// The owner already asked something after this: finishing the
			// old reply now would drop a stale answer into the middle of a
			// newer conversation.
			if newer, _ := st.HasNewerTurn(t); newer {
				continue
			}
			// Waiting on the owner is the owner's turn to move, not
			// Ghost's: re-driving it re-asks a question nobody answered.
			if decidedByOwner(t) {
				continue
			}
			finish[partialKey(t)] = true
			plan.finish = append(plan.finish, resumeJob{
				turn:     t,
				resumeID: fmt.Sprintf("resume-%d-%d", now.UnixNano(), len(plan.finish)),
			})
		}
	}

	pending, err := st.PendingPartials()
	if err != nil {
		return plan, err
	}
	for _, t := range pending {
		if finish[partialKey(t)] {
			continue
		}
		// A reply that failed live was already shown to whoever was
		// watching; only a recent one is worth putting back. An old
		// fragment dropped in now would read as a ghost of a conversation
		// the owner has long moved past.
		if t.Status == turnlog.StatusFailed && now.Sub(t.UpdatedAt) > failedPartialKeep {
			continue
		}
		plan.materialize = append(plan.materialize, t)
	}
	return plan, nil
}

// recoverInterruptedTurns repairs the thread after a restart. Safe to call
// once, right after the durable turn log opens: materializing is
// synchronous, so history is whole before the first request is served, and
// finishing waits for boot to settle.
func recoverInterruptedTurns(st *turnlog.Store, loop *agent.AgentLoop) {
	if st == nil || loop == nil {
		return
	}
	plan, err := planRecovery(st, time.Now(), resumeWindowFor())
	if err != nil {
		logger.WarnCF("internal-api", "could not plan turn recovery", map[string]interface{}{"error": err.Error()})
		return
	}

	written := 0
	for _, t := range plan.materialize {
		if loop.MaterializeInterruptedReply(t) {
			if err := st.MarkMaterialized(t.SessionID, t.RequestID); err == nil {
				written++
			}
		}
	}
	if written > 0 {
		logger.InfoCF("internal-api", "recovered replies a restart cut off", map[string]interface{}{"count": written})
	}
	if len(plan.finish) == 0 {
		return
	}

	// Claim each resume immediately, before the delay and before the model
	// runs, so a second crash during the finish leaves the turn marked
	// resumed — and therefore written out next time — instead of finished
	// twice.
	jobs := plan.finish[:0]
	for _, j := range plan.finish {
		if err := st.MarkResumed(j.turn.SessionID, j.turn.RequestID, j.resumeID); err != nil {
			logger.WarnCF("internal-api", "could not claim a turn resume", map[string]interface{}{"error": err.Error()})
			continue
		}
		jobs = append(jobs, j)
	}
	logger.InfoCF("internal-api", "finishing interrupted turns", map[string]interface{}{
		"count":  len(jobs),
		"window": resumeWindowFor().String(),
	})
	for _, j := range jobs {
		j := j
		// Boot first: a resumed turn calls tools, and tools want the
		// provider, channels and scheduler fully wired.
		time.AfterFunc(resumeSettle, func() {
			finishInterruptedTurn(st, loop, j.turn, j.resumeID)
		})
	}
}

// finishInterruptedTurn re-drives one interrupted turn and streams the
// finished reply to every surface watching, exactly as a live turn does.
// If it cannot finish, the words it still holds are written into the
// transcript instead — the reply is either completed or preserved, never
// lost.
func finishInterruptedTurn(st *turnlog.Store, loop *agent.AgentLoop, t *turnlog.Turn, resumeID string) {
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorCF("internal-api", "recovered panic while finishing a turn", map[string]interface{}{
				"session_key": t.SessionID, "panic": fmt.Sprint(r),
			})
			preservePartial(st, loop, t)
		}
	}()
	if resumeID == "" {
		preservePartial(st, loop, t)
		return
	}

	channel := t.Channel
	if channel == "" {
		channel = "mobile"
	}
	chatID := t.ChatID
	if chatID == "" {
		chatID = "default"
	}

	// The owner may well be looking: this reply streams like any other.
	// The question itself is deliberately not re-announced — it has been
	// in the thread since before the restart, and echoing it would give
	// the owner a second copy of what they asked.
	turns.Begin(t.SessionID, resumeID, channel, "")
	outcome := "success"
	defer func() { turns.End(t.SessionID, resumeID, outcome) }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	response, runErr := loop.ResumeInterruptedTurn(ctx, agent.ResumeRequest{
		SessionKey: t.SessionID,
		Channel:    channel,
		ChatID:     chatID,
		RequestID:  resumeID,
		Resumes:    t,
		OnChunk: func(chunk string) {
			turns.Delta(t.SessionID, resumeID, chunk)
		},
		OnToolCall: func(name, args string) {
			turns.Tool(t.SessionID, resumeID, toolStatusLabel(name, args))
		},
	})

	if runErr != nil || strings.TrimSpace(response) == "" {
		outcome = "failed"
		logger.WarnCF("internal-api", "could not finish an interrupted turn", map[string]interface{}{
			"session_key": t.SessionID, "request_id": t.RequestID, "error": fmt.Sprint(runErr),
		})
		preservePartial(st, loop, t)
		return
	}

	// The reply is whole now: retire the checkpoint so recovery can never
	// append the half that preceded it.
	if err := st.MarkMaterialized(t.SessionID, t.RequestID); err != nil {
		logger.WarnCF("internal-api", "could not retire a recovered reply", map[string]interface{}{"error": err.Error()})
	}
	loop.SettleSessionSurfaces(t.SessionID, "success")
	loop.Bus().PublishOutbound(bus.OutboundMessage{
		Channel:  channel,
		ChatID:   chatID,
		Content:  response,
		Metadata: map[string]interface{}{"type": "assistant_message", "request_id": resumeID, "session_id": t.SessionID},
	})
	logger.InfoCF("internal-api", "finished a reply a restart cut off", map[string]interface{}{
		"session_key": t.SessionID, "resumed": t.RequestID, "request_id": resumeID,
	})
}

// preservePartial writes the words a failed finish still holds into the
// transcript. Recovery's job is that the reply reaches the owner in some
// form: finished if it can be, written out if it cannot.
func preservePartial(st *turnlog.Store, loop *agent.AgentLoop, t *turnlog.Turn) {
	if loop == nil || st == nil {
		return
	}
	if loop.MaterializeInterruptedReply(t) {
		if err := st.MarkMaterialized(t.SessionID, t.RequestID); err != nil {
			logger.WarnCF("internal-api", "could not retire a preserved reply", map[string]interface{}{"error": err.Error()})
		}
	}
}
