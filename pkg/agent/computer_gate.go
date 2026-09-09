package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/computer"
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

// computerRisk derives risk from the operation (runtime-declared), never
// from the model.
func computerRisk(op string) permissions.Risk {
	return computerRiskOf(computer.Op(op))
}

func computerRiskOf(op computer.Op) permissions.Risk {
	switch op {
	case computer.OpScreenshot:
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

// computerExecutor returns the appliance's real computer executor.
func (al *AgentLoop) computerExecutor() (computer.Computer, error) {
	computerExecOnce.Do(func() {
		computerExecInst = computer.NewLocalComputer("local")
	})
	return computerExecInst, computerExecErr
}

// setTestComputer pins the executor used by the gate (test injection).
func (al *AgentLoop) setTestComputer(c computer.Computer) {
	computerExecOnce.Do(func() {}) // ensure singleton initialized
	computerExecInst = c
	computerExecErr = nil
}

var (
	computerLeaseOnce sync.Once
	computerLeaseInst *computer.LeaseStore
	computerLeaseErr  error
)

func (al *AgentLoop) computerLeaseStore() (*computer.LeaseStore, error) {
	if al == nil || al.db == nil || al.db.DB == nil {
		return nil, fmt.Errorf("no database")
	}
	computerLeaseOnce.Do(func() {
		s, err := computer.NewLeaseStore(al.db.DB)
		if err != nil {
			computerLeaseErr = err
			return
		}
		computerLeaseInst = s
	})
	return computerLeaseInst, computerLeaseErr
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
		return deny("Unknown computer operation %q. Nothing was run.", tool)
	}
	if al == nil || al.governance == nil || al.governance.Broker == nil {
		return deny("Computer is unavailable: this runtime is not governed. Nothing was run.")
	}
	g := al.governance
	owner := g.GhostID
	if owner == "" {
		return deny("Computer is unavailable: no Ghost identity. Nothing was run.")
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if !g.routineAllows(sessionKey, computerCapability) {
		return deny("That isn't part of this routine, so I didn't run it.")
	}
	if !g.contextAllows(sessionKey, computerCapability) {
		return deny("That isn't available in this context, so I didn't run it.")
	}
	exec, err := al.computerExecutor()
	if err != nil || exec == nil {
		return deny("Computer unavailable on this Ghost. Nothing was run.")
	}
	authority, reason := computerAuthority(exec)
	if authority == "none" {
		return deny("Computer unavailable (%s). Nothing was run.", reason)
	}
	controlOp := op != "screenshot"
	if controlOp && authority != "control" {
		return deny("Computer is view-only: no control authority. Nothing was run.")
	}
	// Screenshot observation is available but must still be authorized as a
	// capability read; control ops require the broker (low/consequential).
	risk := computerRisk(op)
	if !controlOp {
		risk = permissions.RiskReadOnly
	}
	taskID, generation, err := al.resolveBrowserTask(sessionKey)
	if err != nil {
		return deny("Computer is unavailable: %v. Nothing was run.", err)
	}
	// Durable lease for control ops is acquired only after authorization.
	switch g.Broker.Evaluate(computerCapability, tool, scopeFor(sessionKey, args), risk) {
	case permissions.VerdictAllow:
		return al.bindComputer(owner, contextID, sessionKey, taskID, generation, op, "allow")
	case permissions.VerdictDeny:
		return computerGateResult{decision: "deny",
			message: "That action isn't allowed. It was declined by permission policy, so I didn't run it."}
	default:
	}
	continuation := continuationOf(args)
	continuation[contOwner] = owner
	continuation[contContext] = contextID
	continuation[contTask] = taskID
	continuation[contGeneration] = generation
	continuation[contComputerOp] = op
	continuation[contComputerTool] = tool
	req, err := g.Broker.Require(requestID, sessionKey, g.AgentID, computerCapability, tool,
		scopeTarget(args), humanReason(computerCapability, tool, args), risk, continuation)
	if err != nil {
		return deny("I couldn't prepare the approval request. Nothing was run.")
	}
	g.NoteCapability(requestID, computerCapability)
	return computerGateResult{decision: "wait", pending: req.ID,
		message: approvalAskText(computerCapability, tool, args, req.ID)}
}

// bindComputer assembles the execution binding and acquires the durable
// lease for control ops (fail closed if the computer is busy).
func (al *AgentLoop) bindComputer(owner, contextID, sessionKey, taskID, generation, op, permission string) computerGateResult {
	call := tools.ComputerCall{
		Owner: owner, ContextID: contextID, TaskID: taskID,
		SessionKey: sessionKey, Generation: generation, Op: op, Permission: permission,
	}
	if op != "screenshot" {
		ls, err := al.computerLeaseStore()
		if err != nil {
			return computerGateResult{decision: "deny", message: "Computer is unavailable: lease store failed. Nothing was run."}
		}
		lease, err := ls.Acquire("local", owner, taskID, sessionKey, contextID, computer.DefaultLeaseTTL)
		if err != nil {
			return computerGateResult{decision: "deny",
				message: "The computer is busy with another task. I didn't run it."}
		}
		_ = lease
		call.ControlAuthority = true
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
		return refuse("Computer is unavailable: this runtime is not governed. Nothing was run.")
	}
	g := al.governance
	tool := resume.Tool
	if stored, _ := resume.Args[contComputerTool].(string); stored != "" && stored != tool {
		return refuse("Approval was for a different computer operation. Nothing was run.")
	}
	op, ok := computerOp(tool)
	if !ok {
		return refuse("Unknown computer operation %q. Nothing was run.", tool)
	}
	owner := g.GhostID
	if owner == "" {
		return refuse("Computer is unavailable: no Ghost identity. Nothing was run.")
	}
	if stored, _ := resume.Args[contOwner].(string); stored != "" && stored != owner {
		return refuse("Owner mismatch: this approval belongs to a different Ghost. Nothing was run.")
	}
	contextID := ""
	if g.Contexts != nil {
		contextID = g.Contexts.SessionContext(sessionKey)
	}
	if stored, _ := resume.Args[contContext].(string); stored != "" && stored != contextID {
		return refuse("Context changed since approval. Nothing was run.")
	}
	if !g.routineAllows(sessionKey, computerCapability) || !g.contextAllows(sessionKey, computerCapability) {
		return refuse("That isn't allowed anymore in this context/routine. Nothing was run.")
	}
	taskID, _ := resume.Args[contTask].(string)
	if taskID == "" {
		taskID = sessionKey
	}
	if gen, _ := resume.Args[contGeneration].(string); gen != "" && al.jobs != nil {
		if !al.jobs.CheckGeneration(taskID, gen) {
			return refuse("That work item moved on while approval waited (stale generation). Nothing was run.")
		}
	}
	if resume.Grant == permissions.GrantAlways {
		if verdict := g.Broker.Evaluate(computerCapability, tool, scopeFor(sessionKey, resume.Args), computerRisk(op)); verdict != permissions.VerdictAllow {
			return refuse("That grant was revoked before I could use it. Nothing was run.")
		}
	}
	exec, err := al.computerExecutor()
	if err != nil || exec == nil {
		return refuse("Computer unavailable on this Ghost. Nothing was run.")
	}
	authority, _ := computerAuthority(exec)
	if op != "screenshot" && authority != "control" {
		return refuse("Computer no longer has control authority. Nothing was run.")
	}
	permission := "once"
	if resume.Grant == permissions.GrantAlways {
		permission = "always"
	}
	call := tools.ComputerCall{
		Owner: owner, ContextID: contextID, TaskID: taskID,
		SessionKey: sessionKey, Op: op, Permission: permission,
	}
	if op != "screenshot" {
		ls, err := al.computerLeaseStore()
		if err != nil {
			return refuse("Computer is unavailable: lease store failed. Nothing was run.")
		}
		if _, err := ls.Acquire("local", owner, taskID, sessionKey, contextID, computer.DefaultLeaseTTL); err != nil {
			return refuse("The computer is busy with another task. Nothing was run.")
		}
		call.ControlAuthority = true
	}
	return call, nil
}

// runComputerTool executes a gate-allowed operation through the shared
// registry with the server binding attached, enforcing the evidence rule
// for control ops (success requires runtime evidence).
func (al *AgentLoop) runComputerTool(ctx context.Context, call tools.ComputerCall, tool string, args map[string]interface{}, channel, chatID, sessionKey string) *tools.ToolResult {
	bound := tools.WithComputerCall(ctx, call)
	res := al.tools.ExecuteWithContext(bound, tool, args, channel, chatID, sessionKey, nil)
	if res == nil {
		return tools.ErrorResult("Computer execution returned no result. Nothing was proven to run.")
	}
	if !res.IsError && call.Op != "screenshot" {
		if res.Evidence == nil {
			return tools.ErrorResult("The computer action may not have completed: no runtime evidence was recorded, so I can't claim it worked.")
		}
	}
	return res
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
		return &tools.ToolResult{ForLLM: "Computer use is unavailable because this runtime is not governed. Nothing was run."}, true, true
	}
	decision := al.authorizeComputerCall(opts.RequestID, opts.SessionKey, tc.Name, tc.Arguments)
	switch decision.decision {
	case "allow":
		res := al.runComputerTool(toolCtx, decision.call, tc.Name, tc.Arguments, opts.Channel, opts.ChatID, opts.SessionKey)
		al.publishComputerEvidence(opts.RequestID, opts.SessionKey, tc.Name, res)
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
