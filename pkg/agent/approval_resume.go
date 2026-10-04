package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/tools"
	"github.com/ianclemence/ghost/pkg/turnlog"
)

// resumeApproval runs the paused call behind an approval the owner has
// given, exactly once, and returns the owner-facing text. It is the single
// body shared by the chat path and the API approval buttons, so a tapped
// "Allow once" and a typed "yes" resume through the same governed step —
// neither can authorize around the broker, and neither can run the call
// twice.
func (al *AgentLoop) resumeApproval(ctx context.Context, resume ResumeOutcome, sessionKey, channel, chatID, requestID string, profile tools.ToolProfile, onChunk func(string), onToolCall func(string, string)) string {
	// The stored approval was re-verified against live state before this
	// ran, so carry the execution grant the registry requires for
	// primitives. It covers exactly this tool, this turn.
	ctx = tools.GrantExec(ctx, resume.Tool)
	var toolResult *tools.ToolResult
	switch {
	case isComputerTool(resume.Tool):
		if call, refuse := al.resumeComputerCall(resume, sessionKey, requestID); refuse != nil {
			toolResult = refuse
		} else {
			toolResult = al.runComputerTool(ctx, call, resume.Tool, resume.Args, channel, chatID, sessionKey)
		}
		al.publishComputerEvidence(requestID, sessionKey, resume.Tool, toolResult)
	case isBrowserTool(resume.Tool):
		if call, refuse := al.resumeBrowserCall(resume, sessionKey, requestID); refuse != nil {
			toolResult = refuse
		} else {
			// The approved step runs now: the card leaves "waiting" and
			// shows the step, then what the page looks like after it.
			al.announceBrowserStart(call, sessionKey, browserStepLabel(resume.Tool, resume.Args))
			toolResult = al.runBrowserTool(ctx, call, resume.Tool, resume.Args, channel, chatID, sessionKey)
			al.recordBrowserSurface(call, resume.Tool, toolResult, sessionKey)
		}
		al.publishBrowserEvidence(requestID, sessionKey, resume.Tool, toolResult)
	default:
		toolResult = al.tools.ExecuteWithContext(ctx, resume.Tool, resume.Args, channel, chatID, sessionKey, nil)
		al.governance.ToolRan(requestID, sessionKey, resume.Tool, turnlog.TrajectoryIDFromContext(ctx), toolResult.IsError, toolResult.Obs)
	}
	al.governance.CapabilityDone(requestID, sessionKey, resume.Capability, turnlog.TrajectoryIDFromContext(ctx), toolResult.IsError)
	// The resumed result is evidence for the model, never Ghost's own
	// words. Piping a page or a command's stdout straight into the reply is
	// what turned "approve it and I'll give you the rundown" into a wall of
	// markup: raw payload is context, not an answer. Hand it back for one
	// model turn so the owner gets what was promised. The paused call
	// itself still runs exactly once — the request is never repeated.
	if al.canContinueResume() {
		response, cerr := al.runAgentLoop(ctx, processOptions{
			SessionKey:         sessionKey,
			Channel:            channel,
			ChatID:             chatID,
			ToolProfile:        profile,
			UserMessage:        resumeContinuationLabel(resume.Tool),
			ContinuationOutput: toolResult.ForLLM,
			DefaultResponse:    "Hmm — that came back empty. Could you say it another way?",
			EnableSummary:      true,
			OnChunk:            onChunk,
			OnToolCall:         onToolCall,
			RequestID:          requestID,
		})
		if cerr == nil && strings.TrimSpace(response) != "" {
			return response
		}
		logger.WarnCF("agent", "approval continuation failed, reporting receipt",
			map[string]interface{}{"session_key": sessionKey, "error": fmt.Sprint(cerr)})
	}
	// No model turn available (or it failed): the receipt must still reach
	// the live transcript, not just storage — a resumed approval that
	// reports only to the database shows the owner "(no response)" while
	// Ghost claims it acted. It is bounded so a payload can never be pasted
	// at the owner as Ghost's words.
	text := resumeReceiptText(toolResult)
	if onChunk != nil && text != "" {
		onChunk(text)
	}
	if al.sessions != nil {
		al.sessions.AddMessage(sessionKey, "assistant", text)
		al.sessions.Save(sessionKey)
	}
	return text
}

// ExecuteApprovedRequest runs the continuation of an approval the owner
// gave through an API surface — a button, not a typed reply. The broker
// request is already resolved; this executes the paused call exactly once
// and writes the reply to the conversation, off the request that answered
// the button.
//
// It exists because resolving the broker request alone cleared the card
// while the action never ran: the conversation kept showing "waiting for
// your approval" because, as far as the paused call was concerned, nothing
// had happened. Returns false when there is nothing executable to resume
// (a deny, or a request that had no continuation).
func (al *AgentLoop) ExecuteApprovedRequest(r *permissions.Request, grant permissions.GrantType, channel, chatID string) bool {
	if al == nil || al.governance == nil || r == nil {
		return false
	}
	resume, ok := al.governance.ResumeApproved(r, grant)
	if !ok || strings.TrimSpace(resume.Tool) == "" {
		return false
	}
	// The owner answered, so any background job parked at this approval is
	// waiting on that answer: hand it back to the runner.
	al.resumeWaitingJobs(r.SessionKey)
	sessionKey, requestID := r.SessionKey, r.RequestID
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorCF("agent", "panic resuming approval",
					map[string]interface{}{"panic": fmt.Sprint(rec), "tool": resume.Tool})
			}
		}()
		al.resumeApproval(context.Background(), resume, sessionKey, channel, chatID, requestID, tools.ToolProfile(""), nil, nil)
	}()
	return true
}
