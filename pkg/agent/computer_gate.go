package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/computer"
	"github.com/ianclemence/ghost/pkg/live"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/tools"
)

// Computer governance: the model proposes, the runtime decides.
//
// A computer_* tool call from the model NEVER reaches an executor directly.
// It passes through this gate, which resolves owner/context/task/generation
// server-side, consults the Permission Broker, verifies the executor has
// real control authority (not view-only), acquires/renews the durable lease
// for state-changing operations, executes through the bounded LocalComputer
// executor, and records evidence as a canonical event.
//
// The operation taxonomy is the boundary: screenshot is observation;
// click/type/press_key are control. Unknown computer_* names are denied.

// computerToolNames are the model-visible computer operations.
var computerToolNames = map[string]bool{
	"computer_inspect_ui": true,
	"computer_screenshot": true,
	"computer_click":      true,
	"computer_type":       true,
	"computer_press_key":  true,
}

// isComputerTool reports whether a tool name is a governed computer op.
func isComputerTool(name string) bool { return computerToolNames[name] }

// computerOp maps a tool name to its concrete operation word.
func computerOp(tool string) (string, bool) {
	if !computerToolNames[tool] {
		return "", false
	}
	return strings.TrimPrefix(tool, "computer_"), true
}

// computerControlOp reports whether an operation changes state. Observation
// (screenshot, inspect_ui) never confers control authority; control ops
// (click/type/press_key) require the full authority state, broker decision,
// and lease.
func computerControlOp(op string) bool {
	return !computer.IsObservation(computer.Op(op))
}

// computerRisk derives risk from the operation (runtime-declared), never
// from the model.
func computerRisk(op string) permissions.Risk {
	return computerRiskOf(computer.Op(op))
}

func computerRiskOf(op computer.Op) permissions.Risk {
	switch op {
	case computer.OpScreenshot, computer.OpInspectUI:
		return permissions.RiskReadOnly
	case computer.OpClick:
		return permissions.RiskLow
	case computer.OpType, computer.OpPressKey:
		return permissions.RiskConsequential
	default:
		return permissions.RiskConsequential
	}
}

const computerCapability = "computer"

var (
	computerExecOnce sync.Once
	computerExecInst computer.Computer
	computerExecErr  error
)

// computerExecutor returns the personal AI's real computer executor.
func (al *AgentLoop) computerExecutor() (computer.Computer, error) {
	computerExecOnce.Do(func() {
		computerExecInst = computer.NewLocalComputer("local")
	})
	return computerExecInst, computerExecErr
}

// setTestComputer pins the executor used by the gate (fixture/test
// injection: golden computer cases bind the deterministic VirtualUI here;
// the gate, broker, lease, and evidence rules still apply unchanged).
func (al *AgentLoop) setTestComputer(c computer.Computer) {
	computerExecOnce.Do(func() {}) // ensure singleton initialized
	computerExecInst = c
	computerExecErr = nil
}

// SetComputerExecutor pins the executor used by the computer gate. It is
// the seam a golden fixture (or an operator with an explicit executor)
// uses to bind a specific computer. It does not weaken any boundary: the
// gate still enforces taxonomy, broker, authority, lease, and evidence.
func (al *AgentLoop) SetComputerExecutor(c computer.Computer) {
	al.setTestComputer(c)
}

func (al *AgentLoop) computerLeaseStore() (*computer.LeaseStore, error) {
	if al == nil || al.db == nil || al.db.DB == nil {
		return nil, fmt.Errorf("no database")
	}
	al.computerLeaseOnce.Do(func() {
		s, err := computer.NewLeaseStore(al.db.DB)
		if err != nil {
			al.computerLeaseErr = err
			return
		}
		al.computerLeaseInst = s
	})
	return al.computerLeaseInst, al.computerLeaseErr
}

// RecoverStaleLeases expires any computer lease left active by a crash.
// Call once at startup: after a restart no pre-restart task is alive, so a
// surviving hold is stale by definition and must not block new work until
// its TTL lapses. Nil-safe.
func (al *AgentLoop) RecoverStaleLeases() (int64, error) {
	s, err := al.computerLeaseStore()
	if err != nil {
		return 0, err
	}
	return s.RecoverStale()
}

type computerGateResult struct {
	decision string // allow | wait | deny
	message  string
	pending  string
	call     tools.ComputerCall
}

func (al *AgentLoop) authorizeComputerCall(requestID, sessionKey, tool string, args map[string]interface{}) computerGateResult {
	deny := func(format string, a ...interface{}) computerGateResult {
		return computerGateResult{decision: "deny", message: fmt.Sprintf(format, a...)}
	}
	op, ok := computerOp(tool)
	if !ok {
		return deny(denyText(permissions.CodePreconditionFailed, fmt.Sprintf("Unknown computer operation %q.", tool), "Use an operation this computer supports; ask what it can do."))
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return deny(denyText(permissions.CodeUnavailable, "Computer is unavailable: this runtime is not governed.", "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	g := al.governance
	owner := g.GhostID
	if owner == "" {
		return deny(denyText(permissions.CodeUnavailable, "Computer is unavailable: no Ghost identity.", "The operator must complete onboarding so this Ghost has an identity."))
	}
	// Live Surface plane: register the personal AI computer and enforce the
	// pause. While a human owns the surface, Ghost is paused.
	if al.livePlane != nil {
		al.livePlane.Register("local", live.KindComputer)
		if ok, reason := al.livePlane.GhostMayAct("local"); !ok {
			return deny(denyText(permissions.CodeUnavailable, fmt.Sprintf("The computer is paused: %s.", reason), "Resume the surface and ask again."))
		}
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if !g.routineAllows(sessionKey, computerCapability) {
		return deny(denyText(permissions.CodeScopeRoutine, "That isn't part of this routine, so I didn't run it.", "Run it as a direct request instead of inside the routine."))
	}
	if !g.contextAllows(sessionKey, computerCapability) {
		return deny(denyText(permissions.CodeScopeContext, "That isn't available in this context, so I didn't run it.", "Switch to a context where it is allowed, or allowlist it there."))
	}
	exec, err := al.computerExecutor()
	if err != nil || exec == nil {
		return deny(denyText(permissions.CodeUnavailable, "Computer unavailable on this Ghost.", "The operator must attach a computer executor first."))
	}
	authority, reason := computerAuthority(exec)
	if authority == "none" {
		return deny(denyText(permissions.CodeUnavailable, fmt.Sprintf("Computer unavailable (%s).", reason), "Try again in a moment; if it persists, the operator must check the computer."))
	}
	controlOp := computerControlOp(op)
	if controlOp && authority != "control" {
		return deny(denyText(permissions.CodeUnavailable, "Computer is view-only: no control authority.", "Ask the operator to grant control authority, then ask again."))
	}
	// Screenshot/inspect observation is available but must still be
	// authorized as a capability read; control ops require the broker
	// (low/consequential).
	risk := computerRisk(op)
	if !controlOp {
		risk = permissions.RiskReadOnly
	}
	taskID, generation, err := al.resolveBrowserTask(sessionKey)
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, fmt.Sprintf("Computer unavailable: %v.", err), "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	// Durable lease for control ops is acquired only after authorization.
	// Canonical action identity: capability + tool:action, matching the
	// governance path so a grant approved in one form is the same grant.
	switch g.Broker.Evaluate(computerCapability, toolAction(tool, args), scopeFor(sessionKey, args), risk) {
	case permissions.VerdictAllow:
		return al.bindComputer(owner, contextID, sessionKey, taskID, generation, op, "allow")
	case permissions.VerdictDeny:
		return computerGateResult{decision: "deny",
			message: denyText(permissions.CodePolicyDenied, "That computer action isn't allowed by permission policy, so I didn't run it.", "Tell me which narrower scope should allow it, or approve it when I ask.")}
	default:
	}
	continuation := continuationOf(args)
	continuation[contOwner] = owner
	continuation[contContext] = contextID
	continuation[contTask] = taskID
	continuation[contGeneration] = generation
	continuation[contComputerOp] = op
	continuation[contComputerTool] = tool
	req, err := g.Broker.RequireWithTrajectory(requestID, sessionKey, g.AgentID, g.trajectoryFor(requestID), computerCapability, tool,
		scopeTarget(args), humanReason(computerCapability, tool, args), risk, continuation)
	if err != nil {
		return deny(denyText(permissions.CodeUnavailable, "I couldn't prepare the approval request.", "Ask again; if it repeats, the operator must check the permission store."))
	}
	g.NoteCapability(requestID, computerCapability, "")
	if al.livePlane != nil {
		al.livePlane.Register("local", live.KindComputer)
		al.livePlane.SetState("local", live.StateWaiting)
		al.livePlane.SetTask("local", taskID)
		al.announceSurface(sessionKey, "local", live.KindComputer)
	}
	return computerGateResult{decision: "wait", pending: req.ID,
		message: approvalAskTextEx(computerCapability, tool, args, req)}
}

// bindComputer assembles the execution binding and acquires the durable
// lease for control ops (fail closed if the computer is busy).
func (al *AgentLoop) bindComputer(owner, contextID, sessionKey, taskID, generation, op, permission string) computerGateResult {
	call := tools.ComputerCall{
		Owner: owner, ContextID: contextID, TaskID: taskID,
		SessionKey: sessionKey, Generation: generation, Op: op, Permission: permission,
	}
	if computerControlOp(op) {
		ls, err := al.computerLeaseStore()
		if err != nil {
			return computerGateResult{decision: "deny", message: denyText(permissions.CodeUnavailable, "Computer is unavailable: lease store failed.", "Try again in a moment; if it persists, the operator must check the database.")}
		}
		lease, err := ls.Acquire("local", owner, taskID, sessionKey, contextID, computer.DefaultLeaseTTL)
		if err != nil {
			return computerGateResult{decision: "deny",
				message: denyText(permissions.CodeUnavailable, "The computer is busy with another task.", "Wait for the current task to finish, then ask again.")}
		}
		_ = lease
		call.ControlAuthority = true
	}
	if al.livePlane != nil {
		al.livePlane.SetControlOwner("local", live.OwnerGhost)
		al.livePlane.SetTask("local", taskID)
		al.announceSurface(sessionKey, "local", live.KindComputer)
	}
	return computerGateResult{decision: "allow", call: call}
}

// resumeComputerCall re-verifies a stored approval against live state
// before executing a computer operation (owner, context, generation,
// grant, lease/authority).
func (al *AgentLoop) resumeComputerCall(resume ResumeOutcome, sessionKey, requestID string) (tools.ComputerCall, *tools.ToolResult) {
	refuse := func(format string, a ...interface{}) (tools.ComputerCall, *tools.ToolResult) {
		return tools.ComputerCall{}, tools.ErrorResult(fmt.Sprintf(format, a...))
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return refuse(denyText(permissions.CodeUnavailable, "Computer is unavailable: this runtime is not governed.", "Try again in a moment; if it persists, the operator must check the gateway."))
	}
	g := al.governance
	tool := resume.Tool
	if stored, _ := resume.Args[contComputerTool].(string); stored != "" && stored != tool {
		return refuse(denyText(permissions.CodeBindingMismatch, "That approval was for a different computer operation.", "Ask again for this operation."))
	}
	op, ok := computerOp(tool)
	if !ok {
		return refuse(denyText(permissions.CodePreconditionFailed, fmt.Sprintf("Unknown computer operation %q.", tool), "Use an operation this computer supports; ask what it can do."))
	}
	owner := g.GhostID
	if owner == "" {
		return refuse(denyText(permissions.CodeUnavailable, "Computer is unavailable: no Ghost identity.", "The operator must complete onboarding so this Ghost has an identity."))
	}
	// Live Surface pause must be rechecked on resume (a takeover may have
	// happened while approval waited).
	if al.livePlane != nil {
		al.livePlane.Register("local", live.KindComputer)
		if ok, reason := al.livePlane.GhostMayAct("local"); !ok {
			return refuse(denyText(permissions.CodeUnavailable, fmt.Sprintf("The computer is paused: %s.", reason), "Resume the surface and ask again."))
		}
	}
	if stored, _ := resume.Args[contOwner].(string); stored != "" && stored != owner {
		return refuse(denyText(permissions.CodeBindingMismatch, "Owner mismatch: this approval belongs to a different Ghost.", "Approvals never transfer between owners; ask again here."))
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if stored, _ := resume.Args[contContext].(string); stored != "" && stored != contextID {
		return refuse(denyText(permissions.CodeBindingMismatch, "Context changed since approval.", "Ask again under the current context."))
	}
	if !g.routineAllows(sessionKey, computerCapability) || !g.contextAllows(sessionKey, computerCapability) {
		return refuse(denyText(permissions.CodeScopeContext, "That isn't allowed anymore in this context/routine.", "Ask again where it is allowed."))
	}
	taskID, _ := resume.Args[contTask].(string)
	if taskID == "" {
		taskID = sessionKey
	}
	if gen, _ := resume.Args[contGeneration].(string); gen != "" && al.jobs != nil {
		if !al.jobs.CheckGeneration(taskID, gen) {
			return refuse(denyText(permissions.CodeSessionExpired, "That work item moved on while approval waited (stale generation).", "Ask again to start fresh."))
		}
	}
	if resume.Grant == permissions.GrantAlways {
		if verdict := g.Broker.Evaluate(computerCapability, tool, scopeFor(sessionKey, resume.Args), computerRisk(op)); verdict != permissions.VerdictAllow {
			return refuse(denyText(permissions.CodeGrantRevoked, "That grant was revoked before I could use it.", "Ask again for a fresh approval."))
		}
	}
	exec, err := al.computerExecutor()
	if err != nil || exec == nil {
		return refuse(denyText(permissions.CodeUnavailable, "Computer unavailable on this Ghost.", "The operator must attach a computer executor first."))
	}
	authority, _ := computerAuthority(exec)
	if computerControlOp(op) && authority != "control" {
		return refuse(denyText(permissions.CodeUnavailable, "Computer no longer has control authority.", "Ask the operator to grant control authority, then ask again."))
	}
	permission := "once"
	if resume.Grant == permissions.GrantAlways {
		permission = "always"
	}
	call := tools.ComputerCall{
		Owner: owner, ContextID: contextID, TaskID: taskID,
		SessionKey: sessionKey, Op: op, Permission: permission,
	}
	if computerControlOp(op) {
		ls, err := al.computerLeaseStore()
		if err != nil {
			return refuse(denyText(permissions.CodeUnavailable, "Computer is unavailable: lease store failed.", "Try again in a moment; if it persists, the operator must check the database."))
		}
		if _, err := ls.Acquire("local", owner, taskID, sessionKey, contextID, computer.DefaultLeaseTTL); err != nil {
			return refuse(denyText(permissions.CodeUnavailable, "The computer is busy with another task.", "Wait for the current task to finish, then ask again."))
		}
		call.ControlAuthority = true
	}
	return call, nil
}

// runComputerTool executes a gate-allowed operation through the shared
// registry with the server binding attached, enforcing the evidence rule
// for control ops (success requires runtime evidence).
func (al *AgentLoop) runComputerTool(ctx context.Context, call tools.ComputerCall, tool string, args map[string]interface{}, channel, chatID, sessionKey string) *tools.ToolResult {
	bound := tools.GrantExec(tools.WithComputerCall(ctx, call), tool)
	res := al.tools.ExecuteWithContext(bound, tool, args, channel, chatID, sessionKey, nil)
	if res == nil {
		return tools.ErrorResult(permissions.Deny(permissions.CodeEvidenceAbsent, "Computer execution returned no result.", "Nothing was proven to run — ask again and watch for the result.").String())
	}
	if !res.IsError && computerControlOp(call.Op) {
		if res.Evidence == nil {
			return tools.ErrorResult(permissions.Deny(permissions.CodeEvidenceAbsent, "The computer action may not have completed: no runtime evidence was recorded, so I can't claim it worked.", "Verify state on the machine before retrying.").String())
		}
	}
	return res
}

// recordComputerSurface publishes a safe observation to the Live Surface
// plane after a governed computer execution. For screenshots the internal
// file path is retained server-side so the serving layer can stream the
// image; nothing secret or internal is placed in the observation JSON.
func (al *AgentLoop) recordComputerSurface(call tools.ComputerCall, res *tools.ToolResult, sessionKey string) {
	if al.livePlane == nil || res == nil {
		return
	}
	obs := live.Observation{Title: "Ghost computer"}
	if res.IsError {
		al.livePlane.SetState("local", live.StateFailed)
	} else {
		al.livePlane.SetState("local", live.StateActive)
		obs.State = live.StateActive
	}
	if p, _ := res.Evidence["path"].(string); p != "" && call.Op == "screenshot" {
		obs.Title = "Computer screen"
		obs.ScreenshotPath = p
	}
	if t, _ := res.Evidence["window"].(string); t != "" && call.Op == "inspect_ui" {
		obs.Title = t
	}
	al.livePlane.Register("local", live.KindComputer)
	al.livePlane.SetTask("local", call.TaskID)
	al.livePlane.Observe("local", obs)
	al.announceSurface(sessionKey, "local", live.KindComputer)
}

// publishComputerEvidence records the governed outcome as a canonical
// event carrying the evidence.
func (al *AgentLoop) publishComputerEvidence(requestID, sessionKey, tool string, res *tools.ToolResult) {
	if al == nil || al.governance == nil || al.governance.Events == nil || res == nil {
		return
	}
	payload := map[string]interface{}{"tool": tool, "capability": computerCapability}
	for k, v := range res.Evidence {
		payload[k] = v
	}
	typ := cevents.ToolCompleted
	if res.IsError {
		typ = cevents.ToolFailed
	}
	al.governance.Events.Publish(&cevents.Event{
		Type: typ, RequestID: requestID, SessionID: sessionKey,
		GhostID: al.governance.GhostID, AgentID: al.governance.AgentID,
		Status:  map[bool]string{true: "failed", false: "success"}[res.IsError],
		Payload: payload,
	})
}

// maybeRunComputerTool routes one model computer call through the gate.
func (al *AgentLoop) maybeRunComputerTool(toolCtx context.Context, reg *tools.ToolRegistry, tc providers.ToolCall, opts processOptions, asyncCallback tools.AsyncCallback) (*tools.ToolResult, bool, bool) {
	if !isComputerTool(tc.Name) {
		res := reg.ExecuteWithContext(toolCtx, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey, asyncCallback)
		return res, false, false
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return &tools.ToolResult{ForLLM: denyText(permissions.CodeUnavailable, "Computer use is unavailable because this runtime is not governed.", "Try again in a moment; if it persists, the operator must check the gateway.")}, true, true
	}
	decision := al.authorizeComputerCall(opts.RequestID, opts.SessionKey, tc.Name, tc.Arguments)
	switch decision.decision {
	case "allow":
		res := al.runComputerTool(toolCtx, decision.call, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey)
		al.publishComputerEvidence(opts.RequestID, opts.SessionKey, tc.Name, res)
		al.recordComputerSurface(decision.call, res, opts.SessionKey)
		return res, true, false
	default:
		return &tools.ToolResult{ForLLM: decision.message}, true, true
	}
}

// computerAuthority exposes the executor's control state.
func computerAuthority(c computer.Computer) (string, string) {
	if lc, ok := c.(*computer.LocalComputer); ok {
		return lc.Authority()
	}
	if len(c.SupportedOps()) > 0 {
		return "control", "executor present"
	}
	return "none", "no executor capabilities"
}

// computerContinuation keys (reuse shared naming).
const (
	contComputerOp   = "computer_op"
	contComputerTool = "computer_tool"
)
