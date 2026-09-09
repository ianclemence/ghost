package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/cevents"
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

// browserOp maps a tool name to its capability operation.
func browserOp(tool string) (string, bool) {
	switch tool {
	case "browser_navigate":
		return "navigate", true
	case "browser_snapshot":
		return "observe", true
	case "browser_click":
		return "click", true
	case "browser_type":
		return "type", true
	case "browser_press":
		return "press", true
	default:
		return "", false
	}
}

// browserRisk derives risk from the operation, never from what the model
// claims it is doing. Observation changes nothing; driving the page can
// change the world.
func browserRisk(op string) permissions.Risk {
	switch op {
	case "navigate", "observe":
		return permissions.RiskReadOnly
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
		return deny("Unknown browser operation %q. Nothing was run.", tool)
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return deny("Browser is unavailable: governance is not wired. Nothing was run.")
	}
	g := al.governance
	owner := g.GhostID
	if owner == "" {
		return deny("Browser is unavailable: no Ghost identity. Nothing was run.")
	}
	// Context comes from the session binding, never from arguments.
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	// Routine and context scopes are set by code (scheduler, contexts
	// API), never by the model.
	if !g.routineAllows(sessionKey, browserCapability) {
		return deny("That isn't part of this routine, so I didn't run it.")
	}
	if !g.contextAllows(sessionKey, browserCapability) {
		return deny("That isn't available in this context, so I didn't run it.")
	}
	ledger, err := al.browserSessionLedger()
	if err != nil {
		return deny("Browser is unavailable: session ledger failed. Nothing was run.")
	}
	al.expireBrowserSessions()
	taskID, generation, err := al.resolveBrowserTask(sessionKey)
	if err != nil {
		return deny("Browser is unavailable: %v. Nothing was run.", err)
	}
	risk := browserRisk(op)
	scope := scopeFor(sessionKey, args)
	switch g.Broker.Evaluate(browserCapability, tool, scope, risk) {
	case permissions.VerdictAllow:
		return browserGateResult{decision: "allow", call: tools.BrowserCall{
			Owner: owner, ContextID: contextID, TaskID: taskID,
			Generation: generation, Sessions: ledger, Op: op,
			Permission: "allow",
		}}
	case permissions.VerdictDeny:
		return browserGateResult{decision: "deny",
			message: "That action isn't allowed. It was declined by permission policy, so I didn't run it."}
	default:
	}
	// Approval wait: mint the browser session NOW and pin it in the
	// continuation, so the resume re-verifies the SAME session instead
	// of silently binding a fresh one. Minting is inert (no cookies, no
	// network) until an approved execution uses it.
	profile := "default"
	sess, err := ledger.GetOrCreate(owner, contextID, taskID, profile, 0)
	if err != nil {
		return deny("Browser is unavailable: session mint failed. Nothing was run.")
	}
	continuation := continuationOf(args)
	continuation[contOwner] = owner
	continuation[contContext] = contextID
	continuation[contTask] = taskID
	continuation[contGeneration] = generation
	continuation[contBrowserOp] = op
	continuation[contBrowserSession] = sess.ID
	continuation[contBrowserTool] = tool
	req, err := g.Broker.Require(requestID, sessionKey, g.AgentID, browserCapability, tool,
		scopeTarget(args), humanReason(browserCapability, tool, args), risk, continuation)
	if err != nil {
		return deny("I couldn't prepare the approval request. Nothing was run.")
	}
	g.NoteCapability(requestID, browserCapability)
	return browserGateResult{decision: "wait", pendingID: req.ID,
		message: approvalAskText(browserCapability, tool, args, req.ID)}
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
		return refuse("Browser is unavailable: governance is not wired. Nothing was run.")
	}
	g := al.governance
	args := resume.Args
	// Trust order: the tool name comes from the broker-signed approval
	// action (resume.Tool). The continuation copy is corroboration only —
	// a tampered copy that disagrees refuses instead of redirecting.
	tool := resume.Tool
	if stored, _ := args[contBrowserTool].(string); stored != "" && stored != tool {
		return refuse("Approval was for a different browser operation. Nothing was run.")
	}
	op, ok := browserOp(tool)
	if !ok {
		return refuse("Unknown browser operation %q. Nothing was run.", tool)
	}
	storedOp, _ := args[contBrowserOp].(string)
	if storedOp != "" && storedOp != op {
		return refuse("Approval was for a different browser operation. Nothing was run.")
	}
	// Owner and context are re-resolved from the live session, then
	// compared to the stored binding. A forged or drifted context fails.
	owner := g.GhostID
	if owner == "" {
		return refuse("Browser is unavailable: no Ghost identity. Nothing was run.")
	}
	if stored, _ := args[contOwner].(string); stored != "" && stored != owner {
		return refuse("Owner mismatch: this approval belongs to a different Ghost. Nothing was run.")
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if stored, _ := args[contContext].(string); stored != "" && stored != contextID {
		return refuse("Context changed since approval: this approval was granted under a different context. Nothing was run.")
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
			return refuse("That work item moved on while approval waited (stale generation). Nothing was run — ask again to start fresh.")
		}
	}
	// Standing grants can be revoked between approval and resume:
	// re-evaluate so revocation takes effect immediately.
	if resume.Grant == permissions.GrantAlways {
		if verdict := g.Broker.Evaluate(browserCapability, tool, scopeFor(sessionKey, args), browserRisk(op)); verdict != permissions.VerdictAllow {
			return refuse("That grant was revoked before I could use it. Nothing was run.")
		}
	}
	ledger, err := al.browserSessionLedger()
	if err != nil {
		return refuse("Browser is unavailable: session ledger failed. Nothing was run.")
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
		return tools.BrowserCall{}, tools.ErrorResult("Browser use is not authorized without a session binding. Nothing was run.")
	}
	// Deterministic per session+operation so a retried subagent turn
	// reuses the same pending approval instead of stacking cards.
	decision := al.authorizeBrowserCall("subturn-"+sessionKey+"-"+tool, sessionKey, tool, args)
	if decision.decision == "allow" {
		return decision.call, nil
	}
	return tools.BrowserCall{}, &tools.ToolResult{ForLLM: decision.message}
}

// isBrowserTool reports whether a tool name is a governed browser operation.
func isBrowserTool(name string) bool {
	return len(name) > 8 && name[:8] == "browser_"
}

// maybeRunBrowserTool routes one model tool call. Browser operations with
// wired governance go through the gate; everything else (including
// browser calls on an unwired loop) executes exactly as before. Returns
// the result, whether the gate handled it, and whether the turn must stop
// (approval wait or denial ends the turn with the message).
func (al *AgentLoop) maybeRunBrowserTool(toolCtx context.Context, reg *tools.ToolRegistry, tc providers.ToolCall, opts processOptions, asyncCallback tools.AsyncCallback) (*tools.ToolResult, bool, bool) {
	if !isBrowserTool(tc.Name) || al == nil || al.governance == nil || al.governance.Broker == nil {
		res := reg.ExecuteWithContext(toolCtx, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey, asyncCallback)
		return res, false, false
	}
	decision := al.authorizeBrowserCall(opts.RequestID, opts.SessionKey, tc.Name, tc.Arguments)
	switch decision.decision {
	case "allow":
		res := al.runBrowserTool(toolCtx, decision.call, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey)
		al.publishBrowserEvidence(opts.RequestID, opts.SessionKey, tc.Name, res)
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
	bound := tools.WithBrowserCall(ctx, call)
	res := al.tools.ExecuteWithContext(bound, tool, args, channel, chatID, sessionKey, nil)
	if res == nil {
		return tools.ErrorResult("Browser execution returned no result. Nothing was proven to run.")
	}
	if !res.IsError && call.Op != "" && call.Op != "navigate" && call.Op != "observe" {
		if res.Evidence == nil {
			return tools.ErrorResult("The browser action may not have completed: no runtime evidence was recorded, so I can't claim it worked.")
		}
	}
	return res
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
