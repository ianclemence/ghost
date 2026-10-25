package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/ianclemence/ghost/pkg/computer"
	"github.com/ianclemence/ghost/pkg/permissions"
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
	// Op is the concrete computer operation word (screenshot, inspect_ui,
	// click, type, press_key, scroll, drag, clipboard_get, clipboard_set).
	// The tool refuses to run a different action than the one
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

// Timeout gives one computer action the same ten-minute ceiling as the
// browser. Reading a screen is quick, but a drag, a long paste or a slow UI
// should finish rather than be cut off for speed.
func (t *ComputerTool) Timeout() time.Duration { return 10 * time.Minute }

func (t *ComputerTool) Description() string {
	switch t.action {
	case "inspect_ui":
		return "Read the current UI as a compact structured description: the focused window, visible text, and the interactive elements a user can see, each with a ref like e3. Observation only — never a control action. Click an element by its ref while the snapshot is live; refs expire after any action."
	case "screenshot":
		return "Capture the current computer screen to a file and return its path. The image is also shown to the model for visual grounding. Observation only — never a control action."
	case "click":
		return "Click the computer at pixel coordinates (x, y), or name an element ref like e3 from the latest inspect_ui (preferred when the target has one; elements without a click target need coordinates from a screenshot). State-changing; requires authorization and control authority."
	case "scroll":
		return "Scroll the view under the pointer (or at x, y) one or more wheel clicks up, down, left or right. State-changing; requires authorization and control authority."
	case "drag":
		return "Press the left button at (x1, y1), move to (x2, y2) and release: move things, select text, resize. State-changing; requires authorization and control authority."
	case "clipboard_get":
		return "Read the computer clipboard as text (bounded). What comes back may be credentials or pasted secrets: use it for the task at hand and never repeat it. Observation of user data; requires authorization."
	case "clipboard_set":
		return "Write bounded text to the computer clipboard so a later paste lands it. State-changing; requires authorization and control authority."
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
		props["x"] = map[string]interface{}{"type": "integer", "description": "X pixel coordinate (omit when clicking an element ref)"}
		props["y"] = map[string]interface{}{"type": "integer", "description": "Y pixel coordinate (omit when clicking an element ref)"}
		props["element"] = map[string]interface{}{"type": "string", "description": "Element ref like e3 from the latest inspect_ui; preferred over coordinates. Expires after any action."}
	case "scroll":
		props["direction"] = map[string]interface{}{"type": "string", "enum": []string{"up", "down", "left", "right"}, "description": "Wheel direction"}
		props["amount"] = map[string]interface{}{"type": "integer", "description": "Wheel clicks, 1..100 (default 1)"}
		props["x"] = map[string]interface{}{"type": "integer", "description": "Optional focus point: scrolls under these coordinates"}
		props["y"] = map[string]interface{}{"type": "integer", "description": "Optional focus point: scrolls under these coordinates"}
		required = []string{"direction"}
	case "drag":
		props["x1"] = map[string]interface{}{"type": "integer", "description": "Press-down X pixel coordinate"}
		props["y1"] = map[string]interface{}{"type": "integer", "description": "Press-down Y pixel coordinate"}
		props["x2"] = map[string]interface{}{"type": "integer", "description": "Release X pixel coordinate"}
		props["y2"] = map[string]interface{}{"type": "integer", "description": "Release Y pixel coordinate"}
		required = []string{"x1", "y1", "x2", "y2"}
	case "clipboard_get":
		// No parameters: the clipboard is read as it is.
	case "clipboard_set":
		props["text"] = map[string]interface{}{"type": "string", "description": "Text for the clipboard (bounded length). Evidence records the length, never the bytes."}
		required = []string{"text"}
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
		return ErrorResult(policyDeny(permissions.CodeBindingMismatch, "Computer policy: this operation was not bound by the runtime.", "Start from an authorized call so the runtime binds it."))
	}
	if call.Op != "" && call.Op != t.action {
		return ErrorResult(policyDeny(permissions.CodeBindingMismatch, "Computer policy: operation binding mismatch.", "Call the operation the approval bound."))
	}
	controlOp := computer.IsObservation(computer.Op(t.action)) == false
	if controlOp && call.Permission == "" {
		return ErrorResult(policyDeny(permissions.CodePolicyDenied, "Computer policy: state-changing computer operation requires broker authorization.", "Ask for approval first, then retry."))
	}
	if controlOp && !call.ControlAuthority {
		return ErrorResult(policyDeny(permissions.CodeUnavailable, "Computer policy: no control authority for this computer (view-only or none).", "Ask the operator to grant control authority, then ask again."))
	}
	exec, err := t.executor()
	if err != nil || exec == nil {
		return ErrorResult(policyDeny(permissions.CodeUnavailable, "Computer unavailable: no computer executor on this Ghost.", "The operator must attach a computer executor first."))
	}
	op := computer.Op(t.action)
	a := computer.Args{}
	setStr := func(key string) {
		if s, _ := args[key].(string); s != "" {
			a[key] = s
		}
	}
	setNum := func(key string) {
		if _, ok := args[key]; ok {
			a[key] = fmt.Sprintf("%v", args[key])
		}
	}
	switch t.action {
	case "screenshot":
		setStr("path")
	case "click":
		setNum("x")
		setNum("y")
		setStr("element")
	case "type":
		setStr("text")
	case "press_key":
		setStr("key")
	case "scroll":
		setStr("direction")
		setNum("amount")
		setNum("x")
		setNum("y")
	case "drag":
		setNum("x1")
		setNum("y1")
		setNum("x2")
		setNum("y2")
	case "clipboard_set":
		setStr("text")
	}
	res, derr := exec.Do(ctx, op, a)
	if derr != nil {
		return ErrorResult("Computer operation failed: " + derr.Error())
	}
	out := describeComputerResult(t.action, res)
	evidence := map[string]interface{}{
		"type":       "action",
		"op":         "computer." + t.action,
		"owner":      call.Owner,
		"context":    call.ContextID,
		"task":       call.TaskID,
		"permission": call.Permission,
		"outcome":    "ok",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
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
	tr := &ToolResult{ForLLM: out, ForUser: out, Evidence: evidence}
	// A verified screenshot doubles as vision input: the loop attaches the
	// image for the model, exactly like browser_screenshot. Only the path
	// travels here; the loop reads and caps the bytes.
	if t.action == "screenshot" && res.Verified {
		if p, _ := res.Evidence["path"]; p != "" {
			tr.ScreenshotPath = p
		}
	}
	return tr
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
		// The wording must never read as executor failure: Verified=false
		// means "not independently observed", while a real failure returns
		// an error. ("executor reported false" misled models into hedging
		// a success the runtime had recorded — cc-01.)
		return fmt.Sprintf("Computer action %s dispatched but not independently screen-verified.", action)
	}
}
