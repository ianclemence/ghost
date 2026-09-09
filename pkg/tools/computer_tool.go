package tools

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ianclemence/ghost/pkg/computer"
)

// ComputerCall is the server-resolved binding for one computer operation.
// Every field is derived by the runtime from the turn — never from
// model-supplied arguments. A forged owner/context/task/generation cannot
// reach the executor: the tool honors only this bag.
type ComputerCall struct {
	Owner      string
	ContextID  string
	TaskID     string
	SessionKey string
	Generation string
	// Op is the concrete computer operation word (screenshot/click/type/
	// press_key). The tool refuses to run a different action than the one
	// the gate authorized.
	Op string
	// ControlAuthority is true only when the runtime has verified the
	// executor has control (not view-only/none) and the lease is held.
	ControlAuthority bool
	// Permission describes the broker decision ("allow"/"once"/"always").
	Permission string
	// OnEvidence receives the execution record.
	OnEvidence func(taskID string, ev computer.Evidence)
}

type computerCallKey struct{}

func WithComputerCall(ctx context.Context, call ComputerCall) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, computerCallKey{}, &call)
}

func ComputerCallFromContext(ctx context.Context) (ComputerCall, bool) {
	if ctx == nil {
		return ComputerCall{}, false
	}
	c, _ := ctx.Value(computerCallKey{}).(*ComputerCall)
	if c == nil {
		return ComputerCall{}, false
	}
	return *c, true
}

// ComputerTool is the model-visible surface for one bounded computer
// operation. Exec is provided by the runtime (the real LocalComputer
// executor); it is never the result of a model-supplied command string.
type ComputerTool struct {
	action string // screenshot | click | type | press_key
	// Exec returns the executor or nil with an error when no computer is
	// available on this Ghost.
	Exec func() (computer.Computer, error)
}

// NewComputerTool creates the model-visible computer operation tool.
func NewComputerTool(action string) *ComputerTool {
	return &ComputerTool{action: action}
}

func (t *ComputerTool) Name() string { return "computer_" + t.action }

func (t *ComputerTool) Description() string {
	switch t.action {
	case "inspect_ui":
		return "Read the current UI as a compact structured description: the focused window, visible text, and the interactive elements a user can see (role, value, enabled, click target). Observation only — never a control action."
	case "screenshot":
		return "Capture the current computer screen to a file and return its path. Observation only — never a control action."
	case "click":
		return "Click the computer at pixel coordinates (x, y). State-changing; requires authorization and control authority."
	case "type":
		return "Type text into the focused field on the computer. State-changing; requires authorization and control authority."
	case "press_key":
		return "Press a bounded keyboard key on the computer (Enter, Tab, Escape, arrows...). State-changing; requires authorization and control authority."
	default:
		return "Interact with the computer."
	}
}

func (t *ComputerTool) Parameters() map[string]interface{} {
	props := map[string]interface{}{}
	required := []string{}
	switch t.action {
	case "inspect_ui":
		// No parameters: the current UI is observed as it is.
	case "screenshot":
		props["path"] = map[string]interface{}{
			"type": "string", "description": "Destination path for the screenshot (within the workspace or temp dir).",
		}
		required = []string{"path"}
	case "click":
		props["x"] = map[string]interface{}{"type": "integer", "description": "X pixel coordinate"}
		props["y"] = map[string]interface{}{"type": "integer", "description": "Y pixel coordinate"}
		required = []string{"x", "y"}
	case "type":
		props["text"] = map[string]interface{}{"type": "string", "description": "Text to type into the focused field (bounded length)."}
		required = []string{"text"}
	case "press_key":
		props["key"] = map[string]interface{}{"type": "string", "description": "Key to press, e.g. Return, Tab, Escape."}
		required = []string{"key"}
	}
	return map[string]interface{}{"type": "object", "properties": props, "required": required}
}

// Execute refuses unbound/forged calls and only runs the exact operation
// the gate authorized, through the real executor. Success is whatever the
// executor actually reports — never model narration.
func (t *ComputerTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	call, ok := ComputerCallFromContext(ctx)
	if !ok {
		return ErrorResult("Computer policy: this operation was not bound by the runtime. Nothing was run.")
	}
	if call.Op != "" && call.Op != t.action {
		return ErrorResult("Computer policy: operation binding mismatch. Nothing was run.")
	}
	controlOp := computer.IsObservation(computer.Op(t.action)) == false
	if controlOp && call.Permission == "" {
		return ErrorResult("Computer policy: state-changing computer operation requires broker authorization. Nothing was run.")
	}
	if controlOp && !call.ControlAuthority {
		return ErrorResult("Computer policy: no control authority for this computer (view-only or none). Nothing was run.")
	}
	exec, err := t.executor()
	if err != nil || exec == nil {
		return ErrorResult("Computer unavailable: no computer executor on this Ghost. Nothing was run.")
	}
	op := computer.Op(t.action)
	a := computer.Args{}
	switch t.action {
	case "screenshot":
		if p, _ := args["path"].(string); p != "" {
			a["path"] = p
		}
	case "click":
		a["x"] = fmt.Sprintf("%v", args["x"])
		a["y"] = fmt.Sprintf("%v", args["y"])
	case "type":
		if s, _ := args["text"].(string); s != "" {
			a["text"] = s
		}
	case "press_key":
		if s, _ := args["key"].(string); s != "" {
			a["key"] = s
		}
	}
	res, derr := exec.Do(ctx, op, a)
	if derr != nil {
		return ErrorResult("Computer operation failed: " + derr.Error())
	}
	out := describeComputerResult(t.action, res)
	evidence := map[string]interface{}{"op": "computer." + t.action, "owner": call.Owner, "context": call.ContextID, "task": call.TaskID, "permission": call.Permission}
	for k, v := range res.Evidence {
		evidence[k] = v
	}
	if res.Verified {
		evidence["verified"] = true
	}
	// Canonical evidence record for audit/activity: safe metadata only.
	if call.OnEvidence != nil {
		ev := computer.BeginEvidence(op, exec.ID(), call.TaskID, call.SessionKey, call.ContextID)
		if res.Verified {
			ev.Finish(computer.OutcomeSuccess, "executor verified")
		} else {
			ev.Finish(computer.OutcomeSuccess, "dispatched (not independently screen-verified)")
		}
		ev.Refs = res.Evidence
		call.OnEvidence(call.TaskID, *ev)
	}
	return &ToolResult{ForLLM: out, ForUser: out, Evidence: evidence}
}

func (t *ComputerTool) executor() (computer.Computer, error) {
	if t.Exec == nil {
		return nil, fmt.Errorf("no executor bound")
	}
	return t.Exec()
}

func describeComputerResult(action string, res computer.Result) string {
	switch action {
	case "inspect_ui":
		if res.Verified {
			return res.Output
		}
		return "UI inspection produced no verifiable output."
	case "screenshot":
		if res.Verified {
			return fmt.Sprintf("Screenshot captured and verified: %s (%s bytes)", res.Output, res.Evidence["bytes"])
		}
		return "Screenshot produced no verifiable output."
	default:
		if res.Verified {
			return fmt.Sprintf("Computer action completed and verified: %s", res.Output)
		}
		// Dispatched but not independently observed is reported honestly.
		return fmt.Sprintf("Computer action %s (executor reported %s; not independently screen-verified).", action, strconv.FormatBool(res.Verified))
	}
}
