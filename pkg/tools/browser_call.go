package tools

import (
	"context"

	"github.com/ianclemence/ghost/pkg/browser"
)

// BrowserCall is the server-resolved binding for one browser operation.
// Every field is derived by the runtime from the turn (owner, context,
// session, task) — never from model-supplied arguments, which the gate
// ignores for binding purposes. A forged owner/context/session in tool
// args cannot escalate: this bag is the only binding the tool honors.
type BrowserCall struct {
	// Owner is the Ghost principal (ghost_id). Empty fails closed.
	Owner string
	// ContextID is the turn's context ("personal", "work", ...). Empty
	// means the device's default single context; it still binds — a call
	// resolved under one context never uses another's session.
	ContextID string
	// TaskID identifies the logical work item: a durable job id when the
	// turn runs under one, else the conversation session key. It scopes
	// the browser session and the evidence record.
	TaskID string
	// Generation is the task's worker generation at gate time. Checked
	// against the durable store on resume; empty skips the check (no
	// durable task bound).
	Generation string
	// SessionID pins an already-minted browser session (approval resume
	// path). Empty mints-or-reuses via owner/context/task. When set, the
	// stored row's owner/context/task must match or the call is denied.
	SessionID string
	// Sessions is the live session ledger. Nil fails closed.
	Sessions *browser.SessionStore
	// Profile names the cookie jar inside the context. "" = "default".
	Profile string
	// Op is the classified operation (navigate/observe/click/type/press).
	Op string
	// Permission describes the broker decision that authorized this call:
	// "allow", "grant:once:<id>", "grant:always". Empty means the gate
	// has not authorized act-class work — the tool denies act ops.
	Permission string
	// OnEvidence receives the execution record. Nil keeps the record in
	// the result only.
	OnEvidence func(taskID string, ev browser.Evidence)
}

type browserCallKey struct{}

// WithBrowserCall attaches the server-resolved binding to the execution
// context. Called by the agent gate, never by the model.
func WithBrowserCall(ctx context.Context, call BrowserCall) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, browserCallKey{}, call)
}

// BrowserCallFromContext returns the gate-attached binding, or false when
// the call did not pass through the gate (legacy/internal path).
func BrowserCallFromContext(ctx context.Context) (BrowserCall, bool) {
	if ctx == nil {
		return BrowserCall{}, false
	}
	call, _ := ctx.Value(browserCallKey{}).(BrowserCall)
	return call, call.Sessions != nil
}
