package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/logger"
)

// BrowserTool uses the 'agent-browser' CLI (a Node.js CDP wrapper) to provide
// interactive, accessibility-tree-based browser control.
// This replaces the old screenshot-only headless browser with full navigation, clicking, and typing.
type BrowserTool struct {
	workspace string
	action    string // e.g. "navigate", "click", "type", "press", "snapshot"

	// Policy attaches Ghost runtime guarantees. Nil keeps the exact
	// legacy behavior (no session binding, raw CLI output). Set it to
	// bind every call to an isolated owner+context+task session and to
	// label all page output untrusted before it reaches the model.
	Policy *BrowserPolicy

	// run executes the CLI. Overridable in tests; production uses executeCLI.
	run func(ctx context.Context, action string, args ...string) *ToolResult
}

// BrowserPolicy binds a BrowserTool to Ghost's browser runtime contract:
// one isolated session per owner+context+task (cookie jars never cross
// contexts), redacted untrusted-labeled observations, and an evidence
// record per state-changing op.
//
// Approval note: this tool's surface (click/type/press on element refs)
// cannot see Transact-class intent — a click is a click. Consequential
// gating therefore lives upstream in the capability broker (unknown
// browser capabilities default to consequential, i.e. ask), and every Act
// is evidenced here so the ledger shows exactly what ran. Purchase-class
// flows with declared intent belong on the computer-gated path, not here.
type BrowserPolicy struct {
	Sessions *browser.SessionStore
	// Owner is the device principal; ContextID selects the isolated
	// profile ("" = default context). Both must be set by code, never by
	// the model.
	Owner     string
	ContextID string
	// Profile names the cookie jar inside the context. "" = "default".
	Profile string
	// SessionTTL bounds idle session lifetime. <=0 = store default.
	SessionTTL time.Duration
	// OnEvidence receives one record per executed op. Nil disables.
	OnEvidence func(taskID string, ev browser.Evidence)
}

func NewBrowserTool(workspace string, action string) *BrowserTool {
	t := &BrowserTool{workspace: workspace, action: action}
	t.run = t.executeCLI
	return t
}

// Classify maps this tool's action to its risk class: observation (reads
// page state, changes nothing) or act (drives the page). Exposed so the
// capability broker can distinguish the two without trusting
// model-supplied arguments.
func (t *BrowserTool) Classify() string {
	switch t.action {
	case "navigate", "snapshot":
		return "observe"
	default:
		return "act"
	}
}

func (t *BrowserTool) Name() string {
	return "browser_" + t.action
}

func (t *BrowserTool) Description() string {
	switch t.action {
	case "navigate":
		return "Navigate the browser to a specific URL. Returns the accessibility tree of the page so you can see elements."
	case "snapshot":
		return "Returns the current page accessibility tree (ARIA snapshot) with element reference IDs (like @e5) that you can use to interact with the page."
	case "click":
		return "Click on an element on the current page using its reference ID (e.g. '@e5'). Always use the exact reference string from the accessibility tree."
	case "type":
		return "Type text into an input field on the current page. Requires the element reference ID."
	case "press":
		return "Press a keyboard key (e.g. 'Enter', 'Tab', 'Escape', 'ArrowDown') on the current page."
	default:
		return "Interact with the browser."
	}
}

func (t *BrowserTool) Parameters() map[string]interface{} {
	props := map[string]interface{}{}
	required := []string{}

	switch t.action {
	case "navigate":
		props["url"] = map[string]interface{}{
			"type":        "string",
			"description": "The URL to navigate to (must include http/https).",
		}
		required = []string{"url"}
	case "snapshot":
		// No parameters required
	case "click":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e5').",
		}
		required = []string{"ref"}
	case "type":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e5').",
		}
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "The text to type into the field.",
		}
		props["press_enter"] = map[string]interface{}{
			"type":        "boolean",
			"description": "Whether to press Enter after typing (default: false).",
		}
		required = []string{"ref", "text"}
	case "press":
		props["key"] = map[string]interface{}{
			"type":        "string",
			"description": "The key to press (e.g. 'Enter', 'Tab', 'Escape').",
		}
		required = []string{"key"}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// executeCLI runs the agent-browser CLI.
func (t *BrowserTool) executeCLI(ctx context.Context, action string, args ...string) *ToolResult {
	// Ensure temp directory exists for session tracking (agent-browser usually uses ~/.agent-browser)
	// but we'll let the CLI manage its own state for now.

	cmdArgs := append([]string{action}, args...)

	// Default to non-interactive json output
	cmdArgs = append(cmdArgs, "--json")

	logger.DebugCF("browser", "Executing agent-browser", map[string]interface{}{
		"action": action,
		"args":   args,
	})

	cmd := exec.CommandContext(ctx, "agent-browser", cmdArgs...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// If command not found, give a helpful error
		pathErr, isPathErr := err.(*exec.Error)
		if isPathErr && pathErr.Err == exec.ErrNotFound {
			return ErrorResult("The 'agent-browser' command is not installed. Please install it with: npm install -g agent-browser")
		}

		return ErrorResult(fmt.Sprintf("Browser action '%s' failed: %v\nStderr: %s", action, err, stderr.String()))
	}

	output := strings.TrimSpace(stdout.String())
	return UserResult(output)
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.Policy != nil && t.Policy.Sessions != nil {
		return t.executeGuarded(ctx, args)
	}
	return t.executeBare(ctx, args)
}

// executeGuarded binds the call to an isolated session, runs it, then
// redacts and labels the observation before it reaches the model.
func (t *BrowserTool) executeGuarded(ctx context.Context, args map[string]interface{}) *ToolResult {
	p := t.Policy
	taskID := SessionKeyFromContext(ctx)
	if taskID == "" {
		taskID = "interactive"
	}
	owner := p.Owner
	if owner == "" {
		owner = "local"
	}
	profile := p.Profile
	if profile == "" {
		profile = "default"
	}
	sess, err := p.Sessions.GetOrCreate(owner, p.ContextID, taskID, profile, p.SessionTTL)
	if err != nil {
		return ErrorResult(fmt.Sprintf("browser session unavailable: %v", err))
	}
	started := time.Now().UTC()
	detail := fmt.Sprintf("%s %v", t.action, args)
	res := t.executeBare(ctx, args)
	if p.OnEvidence != nil {
		outcome := "ok"
		if res.IsError {
			outcome = "error"
		}
		p.OnEvidence(taskID, browser.Evidence{
			Operation: t.action + ":" + t.Classify(),
			SessionID: sess.ID, TaskID: taskID, ContextID: p.ContextID,
			Outcome: outcome, Detail: detail,
			StartedAt: started, EndedAt: time.Now().UTC(),
		})
	}
	if res.IsError || res.ForLLM == "" {
		return res
	}
	// Page content is untrusted web input: redact secrets, label it, and
	// keep the user-visible text unchanged.
	labeled := browser.ObserveText(res.ForLLM)
	return &ToolResult{ForLLM: labeled, ForUser: res.ForUser, Silent: res.Silent, IsError: false}
}

func (t *BrowserTool) executeBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	_ = ctx
	switch t.action {
	case "navigate":
		url, _ := args["url"].(string)
		if url == "" {
			return ErrorResult("url is required")
		}
		return t.run(ctx, "navigate", url)

	case "snapshot":
		return t.run(ctx, "snapshot")

	case "click":
		ref, _ := args["ref"].(string)
		if ref == "" {
			return ErrorResult("ref is required")
		}
		return t.run(ctx, "click", ref)

	case "type":
		ref, _ := args["ref"].(string)
		text, _ := args["text"].(string)
		if ref == "" || text == "" {
			return ErrorResult("ref and text are required")
		}

		cliArgs := []string{ref, text}
		if pressEnter, ok := args["press_enter"].(bool); ok && pressEnter {
			cliArgs = append(cliArgs, "--enter")
		}
		return t.run(ctx, "type", cliArgs...)

	case "press":
		key, _ := args["key"].(string)
		if key == "" {
			return ErrorResult("key is required")
		}
		return t.run(ctx, "press", key)

	default:
		return ErrorResult(fmt.Sprintf("Unknown browser action: %s", t.action))
	}
}
