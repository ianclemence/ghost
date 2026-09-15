package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/live"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tasks"
	"github.com/ianclemence/ghost/pkg/tools"
)

// Browser governance: the model proposes, the runtime decides.
//
// A browser_* tool call from the model NEVER executes directly. It passes
// through this gate, which resolves every binding server-side (owner,
// context, session, task, generation), consults the permission broker for
// state-changing operations, binds an isolated browser session, executes,
// and attaches runtime evidence to the result. The tool layer re-verifies
// the binding before touching the executor, so the gate cannot be
// bypassed by calling the registry directly with crafted arguments.
//
// Capability vocabulary (explicit operations, never model intent):
//
//	browser.navigate  read-only   go to a URL
//	browser.observe   read-only   read page state (snapshot)
//	browser.click     consequential   drive a page element
//	browser.type      consequential   enter text into the page
//	browser.press     consequential   send a key press to the page
//
// browser.download / browser.upload / browser.transact do not exist as
// tool operations. If they ever do, they MUST be added here as
// consequential-or-higher with explicit broker handling. Unknown
// browser_* tool names are denied outright.

// browserOp maps a tool name to its concrete browser operation. The
// operation IS the tool action: navigate, snapshot (observe), click, type,
// press, fill, submit. The tool layer re-verifies this word equals its own
// action, so a gate binding authorized for one action can never drive a
// different one.
func browserOp(tool string) (string, bool) {
	switch tool {
	case "browser_navigate", "browser_snapshot", "browser_click", "browser_type", "browser_press", "browser_fill", "browser_submit":
		return strings.TrimPrefix(tool, "browser_"), true
	default:
		return "", false
	}
}

// browserRisk derives risk from the operation, never from what the model
// claims it is doing. Observation changes nothing; driving the page can
// change the world; submit declares purchase-class intent and is always
// high impact. The capability vocabulary declares its own risk
// here — the runtime decides, not the model.
func browserRisk(op string) permissions.Risk {
	switch op {
	case "navigate", "snapshot":
		return permissions.RiskReadOnly
	case "submit":
		return permissions.RiskHighImpact
	default:
		return permissions.RiskConsequential
	}
}

// browserCapability is the broker capability every browser operation gates
// on. Actions are per-operation, so grants stay narrow (a grant for
// browser/observe never authorizes browser/click).
const browserCapability = "browser"

// Continuation keys carrying the gate binding through an approval wait.
// None may be secret-shaped: the broker strips keys containing
// key/token/secret/password/credential from continuations.
const (
	contOwner          = "owner"
	contContext        = "context"
	contTask           = "task"
	contGeneration     = "generation"
	contBrowserOp      = "browser_op"
	contBrowserSession = "browser_session"
	contBrowserTool    = "browser_tool"
)

// browserGateResult is the gate's verdict for one call.
type browserGateResult struct {
	// decision is "allow", "wait", or "deny".
	decision string
	// message is the model/user-facing text for wait and deny.
	message string
	// pendingID identifies the durable approval request for wait.
	pendingID string
	// call is the server-resolved binding for allow.
	call tools.BrowserCall
}

// browserSessionLedger opens the session ledger once per loop on the
// loop's database (per-loop, not per-process: tests and golden runs host
// several loops over different databases in one process). The table is
// part of Ghost State (migration v2).
func (al *AgentLoop) browserSessionLedger() (*browser.SessionStore, error) {
	al.browserSessionsOnce.Do(func() {
		if al == nil || al.db == nil || al.db.DB == nil {
			al.browserSessionsErr = fmt.Errorf("no database")
			return
		}
		s, err := browser.NewSessionStore(al.db.DB, filepath.Join(al.workspace, "state", "browser-profiles"))
		if err != nil {
			al.browserSessionsErr = err
			return
		}
		al.browserSessionsInst = s
	})
	return al.browserSessionsInst, al.browserSessionsErr
}

// expireBrowserSessions deletes long-dead sessions so the ledger stays
// bounded. Best-effort: a sweep failure never blocks a call.
func (al *AgentLoop) expireBrowserSessions() {
	if s, err := al.browserSessionLedger(); err == nil && s != nil {
		_, _ = s.ExpireSweep(24 * time.Hour)
	}
}

// resolveBrowserTask binds the turn to its durable work item, if any: the
// single live job carrying this session key. Zero means an interactive
// turn (the session itself is the work item); more than one is ambiguous
// and fails closed. Returns taskID and the task's current generation.
func (al *AgentLoop) resolveBrowserTask(sessionKey string) (taskID, generation string, err error) {
	if al == nil || al.jobs == nil || sessionKey == "" {
		return sessionKey, "", nil
	}
	all, err := al.jobs.List("")
	if err != nil {
		return "", "", err
	}
	var live *tasks.Job
	count := 0
	for i, j := range all {
		if j.SessionKey != sessionKey {
			continue
		}
		switch j.Status {
		case tasks.StatusPending, tasks.StatusRunning, tasks.StatusRetrying,
			tasks.StatusWaitingPermission, tasks.StatusWaitingUser,
			tasks.StatusPaused, tasks.StatusInterrupted:
			live = &all[i]
			count++
		}
	}
	if count > 1 {
		return "", "", fmt.Errorf("multiple live work items for this session")
	}
	if live == nil {
		return sessionKey, "", nil
	}
	return live.ID, live.Generation, nil
}

// authorizeBrowserCall is the enforcement boundary. It resolves bindings,
// consults routine/context scopes and the permission broker, and either
// allows (with a binding), parks for approval (wait), or denies.
func (al *AgentLoop) authorizeBrowserCall(requestID, sessionKey, tool string, args map[string]interface{}) browserGateResult {
	deny := func(format string, a ...interface{}) browserGateResult {
		return browserGateResult{decision: "deny", message: fmt.Sprintf(format, a...)}
	}
	op, ok := browserOp(tool)
	if !ok {
		return deny(denyText(permissions.CodePreconditionFailed, fmt.Sprintf("Unknown browser operation %q.", tool), "Use navigate, snapshot, click, type, fill, press, or submit."))
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return deny(denyText(permissions.CodeUnavailable, "Browser is unavailable: governance is not wired.", "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	g := al.governance
	owner := g.GhostID
	if owner == "" {
		return deny(denyText(permissions.CodeUnavailable, "Browser is unavailable: no Ghost identity.", "The operator must complete onboarding so this Ghost has an identity."))
	}
	// Context comes from the session binding, never from arguments.
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	// Routine and context scopes are set by code (scheduler, contexts
	// API), never by the model.
	if !g.routineAllows(sessionKey, browserCapability) {
		return deny(denyText(permissions.CodeScopeRoutine, "That isn't part of this routine, so I didn't run it.", "Run it as a direct request instead of inside the routine."))
	}
	if !g.contextAllows(sessionKey, browserCapability) {
		return deny(denyText(permissions.CodeScopeContext, "That isn't available in this context, so I didn't run it.", "Switch to a context where it is allowed, or allowlist it there."))
	}
	ledger, err := al.browserSessionLedger()
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, "Browser is unavailable: session ledger failed.", "Try again in a moment; if it persists, the operator must check the database."))
	}
	al.expireBrowserSessions()
	taskID, generation, err := al.resolveBrowserTask(sessionKey)
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, fmt.Sprintf("Browser is unavailable: %v.", err), "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	// Live Surface plane: register the browser session for this
	// owner+context+task and enforce the pause. While a human owns the
	// surface, Ghost is paused. GetOrCreate is deterministic per
	// owner/context/task, so the execution path reuses the same session.
	var liveSessionID string
	if al.livePlane != nil {
		sess, serr := ledger.GetOrCreate(owner, contextID, taskID, "default", 0)
		if serr != nil {
			return deny(denyText(permissions.CodeUnavailable, "Browser is unavailable: session bind failed.", "Ask again to start a fresh session."))
		}
		liveSessionID = sess.ID
		al.livePlane.Register(sess.ID, live.KindBrowser)
		if ok, reason := al.livePlane.GhostMayAct(sess.ID); !ok {
			return deny(denyText(permissions.CodeUnavailable, fmt.Sprintf("The browser is paused: %s.", reason), "Resume the surface and ask again."))
		}
	}
	risk := browserRisk(op)
	scope := scopeFor(sessionKey, args)
	// Canonical action identity: capability + tool:action, matching the
	// governance path so a grant approved in one form is the same grant.
	switch g.Broker.Evaluate(browserCapability, toolAction(tool, args), scope, risk) {
	case permissions.VerdictAllow:
		if al.livePlane != nil && liveSessionID != "" {
			al.livePlane.SetControlOwner(liveSessionID, live.OwnerGhost)
			al.livePlane.SetTask(liveSessionID, taskID)
			al.announceSurface(sessionKey, liveSessionID, live.KindBrowser)
		}
		return browserGateResult{decision: "allow", call: tools.BrowserCall{
			Owner: owner, ContextID: contextID, TaskID: taskID,
			Generation: generation, SessionID: liveSessionID, Sessions: ledger, Op: op,
			Permission: "allow",
		}}
	case permissions.VerdictDeny:
		return browserGateResult{decision: "deny",
			message: denyText(permissions.CodePolicyDenied, "That browser action isn't allowed by permission policy, so I didn't run it.", "Tell me which narrower scope should allow it, or approve it when I ask.")}
	default:
	}
	// Approval wait: mint the browser session NOW and pin it in the
	// continuation, so the resume re-verifies the SAME session instead
	// of silently binding a fresh one. Minting is inert (no cookies, no
	// network) until an approved execution uses it.
	profile := "default"
	sess, err := ledger.GetOrCreate(owner, contextID, taskID, profile, 0)
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, "Browser is unavailable: session mint failed.", "Ask again to start a fresh session."))
	}
	continuation := continuationOf(args)
	continuation[contOwner] = owner
	continuation[contContext] = contextID
	continuation[contTask] = taskID
	continuation[contGeneration] = generation
	continuation[contBrowserOp] = op
	continuation[contBrowserSession] = sess.ID
	continuation[contBrowserTool] = tool
	req, err := g.Broker.RequireWithTrajectory(requestID, sessionKey, g.AgentID, g.trajectoryFor(requestID), browserCapability, tool,
		scopeTarget(args), humanReason(browserCapability, tool, args), risk, continuation)
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, "I couldn't prepare the approval request.", "Ask again; if it repeats, the operator must check the permission store."))
	}
	g.NoteCapability(requestID, browserCapability, "")
	if al.livePlane != nil {
		al.livePlane.Register(sess.ID, live.KindBrowser)
		al.livePlane.SetState(sess.ID, live.StateWaiting)
		al.livePlane.SetTask(sess.ID, taskID)
		al.announceSurface(sessionKey, sess.ID, live.KindBrowser)
	}
	return browserGateResult{decision: "wait", pendingID: req.ID,
		message: approvalAskTextEx(browserCapability, tool, args, req)}
}

// resumeBrowserCall re-verifies a stored approval against CURRENT runtime
// state before executing. The approval proves the user said yes; these
// checks prove yes still means the same thing: same owner, same context,
// same live session, same task generation, grant not revoked.
func (al *AgentLoop) resumeBrowserCall(resume ResumeOutcome, sessionKey, requestID string) (tools.BrowserCall, *tools.ToolResult) {
	refuse := func(format string, a ...interface{}) (tools.BrowserCall, *tools.ToolResult) {
		return tools.BrowserCall{}, tools.ErrorResult(fmt.Sprintf(format, a...))
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return refuse(denyText(permissions.CodeUnavailable, "Browser is unavailable: governance is not wired.", "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	g := al.governance
	args := resume.Args
	// Trust order: the tool name comes from the broker-signed approval
	// action (resume.Tool). The continuation copy is corroboration only —
	// a tampered copy that disagrees refuses instead of redirecting.
	tool := resume.Tool
	if stored, _ := args[contBrowserTool].(string); stored != "" && stored != tool {
		return refuse(denyText(permissions.CodeBindingMismatch, "That approval was for a different browser operation.", "Ask again for this operation."))
	}
	op, ok := browserOp(tool)
	if !ok {
		return refuse(denyText(permissions.CodePreconditionFailed, fmt.Sprintf("Unknown browser operation %q.", tool), "Use navigate, snapshot, click, type, fill, press, or submit."))
	}
	storedOp, _ := args[contBrowserOp].(string)
	if storedOp != "" && storedOp != op {
		return refuse(denyText(permissions.CodeBindingMismatch, "That approval was for a different browser operation.", "Ask again for this operation."))
	}
	// Owner and context are re-resolved from the live session, then
	// compared to the stored binding. A forged or drifted context fails.
	owner := g.GhostID
	if owner == "" {
		return refuse(denyText(permissions.CodeUnavailable, "Browser is unavailable: no Ghost identity.", "The operator must complete onboarding so this Ghost has an identity."))
	}
	if stored, _ := args[contOwner].(string); stored != "" && stored != owner {
		return refuse(denyText(permissions.CodeBindingMismatch, "Owner mismatch: this approval belongs to a different Ghost.", "Approvals never transfer between owners; ask again here."))
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if stored, _ := args[contContext].(string); stored != "" && stored != contextID {
		return refuse(denyText(permissions.CodeBindingMismatch, "Context changed since approval: this approval was granted under a different context.", "Ask again under the current context."))
	}
	if !g.routineAllows(sessionKey, browserCapability) {
		return refuse("That isn't part of this routine, so I didn't run it.")
	}
	if !g.contextAllows(sessionKey, browserCapability) {
		return refuse("That isn't available in this context, so I didn't run it.")
	}
	taskID, _ := args[contTask].(string)
	if taskID == "" {
		taskID = sessionKey
	}
	// Generation: the task must not have moved on while approval waited.
	// A rotated generation means a newer worker owns this work; the stale
	// approval resumes nothing.
	if gen, _ := args[contGeneration].(string); gen != "" && al.jobs != nil {
		if !al.jobs.CheckGeneration(taskID, gen) {
			return refuse(denyText(permissions.CodeSessionExpired, "That work item moved on while approval waited (stale generation).", "Ask again to start fresh."))
		}
	}
	// Standing grants can be revoked between approval and resume:
	// re-evaluate so revocation takes effect immediately.
	if resume.Grant == permissions.GrantAlways {
		if verdict := g.Broker.Evaluate(browserCapability, tool, scopeFor(sessionKey, args), browserRisk(op)); verdict != permissions.VerdictAllow {
			return refuse(denyText(permissions.CodeGrantRevoked, "That grant was revoked before I could use it.", "Ask again for a fresh approval."))
		}
	}
	ledger, err := al.browserSessionLedger()
	if err != nil {
		return refuse(denyText(permissions.CodeUnavailable, "Browser is unavailable: session ledger failed.", "Try again in a moment; if it persists, the operator must check the database."))
	}
	permission := "once"
	if resume.Grant == permissions.GrantAlways {
		permission = "always"
	}
	call := tools.BrowserCall{
		Owner: owner, ContextID: contextID, TaskID: taskID,
		Generation: func() string { s, _ := args[contGeneration].(string); return s }(),
		SessionID:  func() string { s, _ := args[contBrowserSession].(string); return s }(),
		Sessions:   ledger, Op: op, Permission: permission,
	}
	// A takeover may have happened while approval waited: refuse resume if
	// the human now owns the surface.
	if al.livePlane != nil && call.SessionID != "" {
		al.livePlane.Register(call.SessionID, live.KindBrowser)
		if ok, reason := al.livePlane.GhostMayAct(call.SessionID); !ok {
			return tools.BrowserCall{}, tools.ErrorResult(denyText(permissions.CodeUnavailable, "The browser is paused: "+reason+".", "Resume the surface and ask again."))
		}
	}
	return call, nil
}

// authorizeSubagentBrowser implements tools.SubagentBrowserAuth: subagent
// browser calls resolve against the parent turn's session (carried in ctx
// by the registry), so they execute under the same owner/context/task and
// broker posture as the main turn — never as an unbound side channel. An
// approval wait returns as a normal result the subagent relays upward;
// the user's reply resumes the same work in the main turn.
func (al *AgentLoop) authorizeSubagentBrowser(ctx context.Context, tool string, args map[string]interface{}) (tools.BrowserCall, *tools.ToolResult) {
	sessionKey := tools.SessionKeyFromContext(ctx)
	if sessionKey == "" {
		return tools.BrowserCall{}, tools.ErrorResult(denyText(permissions.CodeSubagentUnauthorized, "Browser use is not authorized without a session binding.", "Bind the subagent to the parent session first."))
	}
	// Deterministic per session+operation so a retried subagent turn
	// reuses the same pending approval instead of stacking cards.
	decision := al.authorizeBrowserCall("subturn-"+sessionKey+"-"+tool, sessionKey, tool, args)
	if decision.decision == "allow" {
		return decision.call, nil
	}
	return tools.BrowserCall{}, &tools.ToolResult{ForLLM: decision.message}
}

// authorizeSubagentStandaloneTool implements tools.SubagentConsequentialAuth:
// a subagent standalone consequential call is authorized against the same
// broker as a main-turn call (parent session binding). Denial or wait
// replaces execution with a message the subagent relays; nil allows.
func (al *AgentLoop) authorizeSubagentStandaloneTool(ctx context.Context, tool string, args map[string]interface{}) *tools.ToolResult {
	sessionKey := tools.SessionKeyFromContext(ctx)
	if sessionKey == "" {
		return tools.ErrorResult(denyText(permissions.CodeBindingMismatch, "That operation is not authorized without a session binding.", "Start from a snapshot so the call binds to a live session."))
	}
	decision, handled := al.authorizeStandaloneTool("subturn-"+sessionKey+"-"+tool, sessionKey, tool, args)
	if !handled {
		return nil
	}
	if decision.Allowed {
		return nil
	}
	return &tools.ToolResult{ForLLM: decision.AskMessage}
}

// isBrowserTool reports whether a tool name is a governed browser operation.
func isBrowserTool(name string) bool {
	return len(name) > 8 && name[:8] == "browser_"
}

// maybeRunBrowserTool routes one model tool call. Browser operations go
// through the gate whenever the loop is governed. A browser call on an
// UNWIRED loop (no governance) is refused outright, never executed through
// the ungated legacy path: model-controlled browser execution without a
// runtime authority boundary is a silent downgrade. Non-browser tools are
// untouched. Returns the result, whether the gate handled it, and whether
// the turn must stop.
func (al *AgentLoop) maybeRunBrowserTool(toolCtx context.Context, reg *tools.ToolRegistry, tc providers.ToolCall, opts processOptions, asyncCallback tools.AsyncCallback) (*tools.ToolResult, bool, bool) {
	if !isBrowserTool(tc.Name) {
		res := reg.ExecuteWithContext(toolCtx, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey, asyncCallback)
		return res, false, false
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return &tools.ToolResult{ForLLM: denyText(permissions.CodeUnavailable, "Browser use is unavailable because this runtime is not governed.", "Try again in a moment; if it persists, the operator must check the gateway.")}, true, true
	}
	decision := al.authorizeBrowserCall(opts.RequestID, opts.SessionKey, tc.Name, tc.Arguments)
	switch decision.decision {
	case "allow":
		res := al.runBrowserTool(toolCtx, decision.call, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey)
		al.publishBrowserEvidence(opts.RequestID, opts.SessionKey, tc.Name, res)
		al.recordBrowserSurface(decision.call, tc.Name, res, opts.SessionKey)
		return res, true, false
	default: // wait or deny: the turn ends with the gate message.
		return &tools.ToolResult{ForLLM: decision.message}, true, true
	}
}

// runBrowserTool executes a gate-allowed call through the shared registry
// with the server-resolved binding attached, then enforces the evidence
// rule: a successful state-changing operation without runtime evidence is
// reported as unverified, never as done.
func (al *AgentLoop) runBrowserTool(ctx context.Context, call tools.BrowserCall, tool string, args map[string]interface{}, channel, chatID, sessionKey string) *tools.ToolResult {
	bound := tools.GrantExec(tools.WithBrowserCall(ctx, call), tool)
	res := al.tools.ExecuteWithContext(bound, tool, args, channel, chatID, sessionKey, nil)
	if res == nil {
		return tools.ErrorResult(permissions.Deny(permissions.CodeEvidenceAbsent, "Browser execution returned no result.", "Nothing was proven to run — ask again and watch for the result.").String())
	}
	if !res.IsError && call.Op != "" && call.Op != "navigate" && call.Op != "observe" {
		if res.Evidence == nil {
			return tools.ErrorResult(permissions.Deny(permissions.CodeEvidenceAbsent, "The browser action may not have completed: no runtime evidence was recorded, so I can't claim it worked.", "Here is what did happen — re-snapshot the page to verify state before retrying.").String())
		}
	}
	return res
}

// recordBrowserSurface publishes a safe observation to the Live Surface
// plane after a governed browser execution, keyed by the deterministic
// browser session id. Never carries page source, credentials, or CDP data.
func (al *AgentLoop) recordBrowserSurface(call tools.BrowserCall, tool string, res *tools.ToolResult, sessionKey string) {
	if al.livePlane == nil || call.SessionID == "" || res == nil {
		return
	}
	obs := live.Observation{Title: "Ghost browser"}
	if res.IsError {
		al.livePlane.SetState(call.SessionID, live.StateFailed)
	} else {
		al.livePlane.SetState(call.SessionID, live.StateActive)
		obs.State = live.StateActive
	}
	if u, _ := res.Evidence["url"].(string); u != "" {
		obs.URL = u
	}
	if d, _ := res.Evidence["domain"].(string); d != "" {
		obs.Domain = d
	}
	if t, _ := res.Evidence["title"].(string); t != "" {
		obs.Title = t
	}
	if t, _ := res.Evidence["text"].(string); t != "" {
		obs.Text = t
	}
	// Server-local screenshot path only: the serving layer streams bytes
	// on demand, and the path itself never serializes (json:"-").
	if res.ScreenshotPath != "" {
		obs.ScreenshotPath = res.ScreenshotPath
	}
	_ = tool
	al.livePlane.Register(call.SessionID, live.KindBrowser)
	al.livePlane.SetTask(call.SessionID, call.TaskID)
	al.livePlane.Observe(call.SessionID, obs)
	al.announceSurface(sessionKey, call.SessionID, live.KindBrowser)
}

// publishBrowserEvidence records the governed outcome as a canonical
// event carrying the evidence. Success without evidence never reaches
// here (runBrowserTool converts it); this is the event half of "no
// runtime evidence = no successful execution claim".
func (al *AgentLoop) publishBrowserEvidence(requestID, sessionKey, tool string, res *tools.ToolResult) {
	if al == nil || al.governance == nil || al.governance.Events == nil || res == nil {
		return
	}
	payload := map[string]interface{}{"tool": tool}
	for k, v := range res.Evidence {
		payload[k] = v
	}
	typ := cevents.ToolCompleted
	if res.IsError {
		typ = cevents.ToolFailed
	}
	al.governance.Events.Publish(&cevents.Event{
		Type:      typ,
		RequestID: requestID, SessionID: sessionKey,
		GhostID: al.governance.GhostID, AgentID: al.governance.AgentID,
		Status:  map[bool]string{true: "failed", false: "success"}[res.IsError],
		Payload: payload,
	})
	logger.DebugCF("browser-gate", "governed browser execution",
		map[string]interface{}{"tool": tool, "session": sessionKey})
}
