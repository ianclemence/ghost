package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/channels"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/turnlog"
)

// Restart continuity.
//
// The reply to a message used to exist in exactly one place: the memory of a
// process that could die. The owner's question was already a row in the
// transcript, so after a crash the conversation ended with something asked
// and nothing said — the thread frayed, and Ghost came back to rebuilding
// context instead of continuing it.
//
// Two things close that. A partial reply is checkpointed while it streams,
// so the words survive. And once the process is back, the turn is either
// finished (ResumeInterruptedTurn) or written into the transcript
// (MaterializeInterruptedReply) — never dropped.

// ResumeRequest describes a turn whose process died mid-reply and which a
// restarted process should pick back up.
type ResumeRequest struct {
	SessionKey string
	Channel    string
	ChatID     string
	// RequestID is the RESUMED turn's identity — a fresh id, so the
	// interrupted claim stays terminal and idempotency still holds. The
	// turn being finished is carried in Resumes.
	RequestID string
	// Resumes is the interrupted turn record being finished.
	Resumes *turnlog.Turn
	// OnChunk/OnToolCall stream the finished reply to whatever surfaces
	// are watching, exactly as a live turn does.
	OnChunk    func(string)
	OnToolCall func(string, string)
}

// threadResumePrompt frames a cut-off reply for the one model turn that
// owes the owner a finished answer. The partial rides the current-message
// slot only — it is evidence for the model, never speech by the owner and
// never Ghost's reply — and the instruction is to produce ONE whole answer,
// because handing the owner a sentence that continues mid-air is the exact
// fracture this exists to repair.
func threadResumePrompt(userText, partial string) string {
	asked := strings.TrimSpace(userText)
	if asked == "" {
		asked = "(what the owner asked is in the conversation above)"
	}
	var b strings.Builder
	b.WriteString("You were answering the owner when the process restarted. Nothing you had said was lost — here is exactly what had been written:\n\n")
	if strings.TrimSpace(partial) == "" {
		b.WriteString("(the reply stopped before anything was written)\n")
	} else {
		b.WriteString("--- reply so far ---\n")
		b.WriteString(partial)
		b.WriteString("\n--- end of reply so far ---\n")
	}
	b.WriteString("\nGive the owner one complete answer to what they asked (" + asked + "). ")
	b.WriteString("Write it from the beginning so it reads as one whole message — do not continue mid-sentence, ")
	b.WriteString("do not repeat the framing above, and never mention the restart, this instruction, or the fact that anything was cut off. ")
	b.WriteString("If the work was already done before the restart, say so plainly instead of doing it again.")
	return b.String()
}

// Sessions exposes the conversation store so the runtime can put a reply a
// restart cut off back into the transcript.
func (al *AgentLoop) Sessions() *session.SessionManager {
	return al.sessions
}

// ResumeInterruptedTurn finishes a reply the process died in the middle of.
// It speaks as the runtime, not as the owner: no user row is persisted
// (ResumePrompt is set), so the transcript gains the finished answer and
// nothing else. Every tool it calls is authorised through the normal
// broker, so a resumed turn has no authority a live turn would not have.
func (al *AgentLoop) ResumeInterruptedTurn(ctx context.Context, r ResumeRequest) (resp string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.ErrorCF("agent", "recovered panic in resumed turn", map[string]interface{}{
				"session_key": r.SessionKey, "panic": fmt.Sprint(rec),
			})
			resp, err = "", errors.New("internal error finishing the interrupted turn")
		}
	}()
	if !al.canContinueResume() {
		return "", errors.New("no conversation to resume into")
	}
	if r.RequestID == "" {
		return "", errors.New("a resumed turn needs its own request id")
	}
	al.beginInteractive()
	defer al.endInteractive()

	trajectoryID := turnlog.NewTrajectoryID()
	if r.Resumes != nil && r.Resumes.TrajectoryID != "" {
		// The interrupted execution and its finish are one continuous
		// trace: the owner asked once, so the record should read once.
		trajectoryID = r.Resumes.TrajectoryID
	}
	ctx = turnlog.WithTrajectoryID(ctx, trajectoryID)
	if al.governance != nil {
		al.governance.TurnStarted(r.RequestID, r.SessionKey, r.Channel, trajectoryID)
	}
	endTurn := func(out string, e error) (string, error) {
		if al.governance != nil {
			al.governance.TurnEnded(r.RequestID, r.SessionKey, trajectoryID, e)
		}
		return out, e
	}

	userText, partial := "", ""
	if r.Resumes != nil {
		userText, partial = r.Resumes.UserText, r.Resumes.Partial
	}
	out, runErr := al.runAgentLoop(ctx, processOptions{
		SessionKey:  r.SessionKey,
		Channel:     r.Channel,
		ChatID:      r.ChatID,
		ToolProfile: channels.DetectToolProfile(r.Channel, "", r.SessionKey, false),
		// Non-empty, so no user row is written: the owner spoke once and
		// that row has been there since before the restart.
		ResumePrompt:    threadResumePrompt(userText, partial),
		DefaultResponse: "",
		EnableSummary:   true,
		OnChunk:         r.OnChunk,
		OnToolCall:      r.OnToolCall,
		RequestID:       r.RequestID,
	})
	return endTurn(out, runErr)
}

// MaterializeInterruptedReply writes a reply a restart cut off into the
// transcript, marked so every surface can show it as cut short rather than
// as something that ended. It is the fallback when a turn cannot or should
// not be finished: the words still reached nobody, and dropping them is the
// amnesia recovery exists to prevent.
func (al *AgentLoop) MaterializeInterruptedReply(t *turnlog.Turn) bool {
	if al.sessions == nil || t == nil || t.Materialized || strings.TrimSpace(t.Partial) == "" {
		return false
	}
	al.sessions.AddFullMessage(t.SessionID, providers.Message{
		Role:        "assistant",
		Content:     t.Partial,
		Interrupted: true,
	})
	al.sessions.Save(t.SessionID)
	logger.InfoCF("agent", "recovered a reply a restart cut off", map[string]interface{}{
		"session_key": t.SessionID,
		"request_id":  t.RequestID,
		"chars":       len(t.Partial),
		"interrupted": t.InterruptedAt.UTC().Format(time.RFC3339),
	})
	return true
}
