package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/browser"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/redact"
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
// page state, changes nothing), act (drives the page), or transact
// (declares purchase-class intent: quote + approval + receipt). Exposed so
// the capability broker can distinguish the three without trusting
// model-supplied arguments.
//
// Observation covers reads plus viewport-only moves (scroll): no ref
// epoch is invalidated and no world state changes. Everything that can
// drive the page — including dialogs, downloads, and file uploads — is
// act (or transact for submit), so the broker always decides.
func (t *BrowserTool) Classify() string {
	switch t.action {
	case "navigate", "snapshot", "wait", "find", "screenshot", "scroll", "console", "network", "a11y":
		return "observe"
	case "submit":
		return "transact"
	default:
		return "act"
	}
}

func (t *BrowserTool) Name() string {
	return "browser_" + t.action
}

// Timeout bounds one browser operation well under the turn budget: a hung
// Chromium subprocess must fail honestly instead of eating the whole turn.
// (No retry opt-in here: click/type/submit can carry side effects, and
// retrying a write could duplicate an action.)
func (t *BrowserTool) Timeout() time.Duration {
	return 90 * time.Second
}

// OnTimeout closes the browser session after a hung call so leaked Chromium
// processes cannot accumulate and starve later turns. Best-effort: failures
// only log. Warm sessions from successful calls are never touched.
func (t *BrowserTool) OnTimeout(ctx context.Context) {
	cmd := exec.CommandContext(ctx, "agent-browser", "close", "--json")
	cmd.Env = browserEnvironment()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		logger.DebugCF("browser", "timeout cleanup close failed",
			map[string]interface{}{"error": err.Error(), "stderr": stderr.String()})
		return
	}
	logger.InfoCF("browser", "timeout cleanup closed the browser session", nil)
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
		return "Press a keyboard key (e.g. 'Enter', 'Tab', 'Escape')."
	case "fill":
		return "Clear an input field and fill it with text (e.g. address, cardholder name). Requires the element reference ID. Prefer fill over type for forms."
	case "submit":
		return "Submit a form or order by clicking its submit button. TRANSACTIONAL: on checkout-like pages you must also declare merchant and amount matching the page quote, and broker approval is required. The result carries receipt evidence; never claim success from the model side."
	case "wait":
		return "Wait for a page condition — an element to appear, text to show up, a URL pattern, or a load state — then return. Prefer this over re-snapshot polling after actions that load content. Never sleep a fixed amount; name the condition you need."
	case "find":
		return "Find page elements containing literal text and return their FRESH @eN refs without acting. Find supersedes your last snapshot's refs: re-find or re-snapshot before acting again."
	case "screenshot":
		return "Capture the current page as an image. When the model supports vision the image is attached to this turn's context; otherwise read page state with browser_find or browser_snapshot."
	case "scroll":
		return "Scroll the viewport in a direction by pixels (page content itself does not change; existing refs stay valid)."
	case "console":
		return "Read the page's console messages (log/warn/error), bounded and recent-first. Use it to see why a page is broken."
	case "network":
		return "List the page's recent network requests (method, URL, status, type) — header-free and bounded. Use it to find failed or blocked loads."
	case "a11y":
		return "Run an axe-core accessibility audit of the current page: violation counts plus each rule with example targets."
	case "select":
		return "Select a dropdown option on a <select> element by its @eN ref and the option's value."
	case "check":
		return "Check or uncheck a checkbox by its @eN ref (checked defaults to true)."
	case "hover":
		return "Hover over an element by its @eN ref (reveals hover menus, tooltips)."
	case "drag":
		return "Drag from a source element ref to a target element ref (drag and drop)."
	case "fill_form":
		return "Fill several form fields in ONE call with [{ref, text}, ...] (max 10). Each field is validated against the live snapshot epoch; the result reports which fields filled."
	case "dialog":
		return "Handle an open browser dialog: accept (optionally with text for prompts) or dismiss. 'status' reports whether a dialog is open without touching it."
	case "upload":
		return "Upload local files to a file input by its @eN ref. HIGH IMPACT: file contents leave this device for the page's origin — approval is always required and names the exact paths."
	case "download":
		return "Click an element to download its file into Ghost's managed download directory. Returns the saved path."
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
	case "fill":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e5').",
		}
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "The text to fill into the field (clears first).",
		}
		required = []string{"ref", "text"}
	case "submit":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The submit/order button reference ID from the accessibility tree (e.g. '@e9').",
		}
		props["merchant"] = map[string]interface{}{
			"type":        "string",
			"description": "Declared merchant host (required on checkout pages; must match the page).",
		}
		props["amount"] = map[string]interface{}{
			"type":        "string",
			"description": "Declared total to charge, e.g. '42.50' (required on checkout pages; must match the page quote).",
		}
		required = []string{"ref"}
	case "wait":
		props["selector"] = map[string]interface{}{
			"type":        "string",
			"description": "CSS selector of an element to wait for (e.g. '#loading-spinner').",
		}
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "Literal page text to wait for (substring match).",
		}
		props["url"] = map[string]interface{}{
			"type":        "string",
			"description": "URL pattern to wait for (e.g. '/dashboard').",
		}
		props["load"] = map[string]interface{}{
			"type":        "string",
			"description": "Load state to wait for: 'load', 'domcontentloaded', or 'networkidle'.",
		}
		props["timeout_ms"] = map[string]interface{}{
			"type":        "number",
			"description": "Give up after this many milliseconds (default: runtime bound).",
		}
	case "find":
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "Literal text to locate on the page (case-insensitive).",
		}
		required = []string{"text"}
	case "screenshot":
		// No parameters required
	case "scroll":
		props["direction"] = map[string]interface{}{
			"type":        "string",
			"description": "Direction: 'up', 'down', 'left', or 'right' (default: 'down').",
		}
		props["pixels"] = map[string]interface{}{
			"type":        "number",
			"description": "Pixels to scroll (default: 300, max: 20000).",
		}
	case "console":
		// No parameters required
	case "network":
		props["filter"] = map[string]interface{}{
			"type":        "string",
			"description": "Only requests whose URL contains this pattern.",
		}
	case "a11y":
		// No parameters required
	case "select":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The <select> element reference ID from the accessibility tree (e.g. '@e4').",
		}
		props["value"] = map[string]interface{}{
			"type":        "string",
			"description": "The option value to select (as it appears in the page, not the label).",
		}
		required = []string{"ref", "value"}
	case "check":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The checkbox reference ID from the accessibility tree (e.g. '@e7').",
		}
		props["checked"] = map[string]interface{}{
			"type":        "boolean",
			"description": "True to check, false to uncheck (default: true).",
		}
		required = []string{"ref"}
	case "hover":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID from the accessibility tree (e.g. '@e3').",
		}
		required = []string{"ref"}
	case "drag":
		props["source_ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element to drag, from the accessibility tree (e.g. '@e2').",
		}
		props["target_ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The drop target element, from the accessibility tree (e.g. '@e8').",
		}
		required = []string{"source_ref", "target_ref"}
	case "fill_form":
		props["fields"] = map[string]interface{}{
			"type": "array",
			"items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ref": map[string]interface{}{
						"type":        "string",
						"description": "Field reference ID from the accessibility tree (e.g. '@e5').",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to fill (replaces existing content).",
					},
				},
				"required": []string{"ref", "text"},
			},
			"description": "Fields to fill in order, max 10.",
		}
		required = []string{"fields"}
	case "dialog":
		props["action"] = map[string]interface{}{
			"type":        "string",
			"description": "'accept', 'dismiss', or 'status'.",
		}
		props["text"] = map[string]interface{}{
			"type":        "string",
			"description": "Text to type when accepting a prompt dialog.",
		}
		required = []string{"action"}
	case "upload":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The file input reference ID from the accessibility tree (e.g. '@e6').",
		}
		props["paths"] = map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Local file paths to upload (must exist on this device).",
		}
		required = []string{"ref", "paths"}
	case "download":
		props["ref"] = map[string]interface{}{
			"type":        "string",
			"description": "The element reference ID whose click triggers the download (e.g. '@e10').",
		}
		required = []string{"ref"}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// browserEnvironment returns the environment for the agent-browser child.
// If Ghost discovered a working browser executable (Playwright's
// headless_shell) and the operator has not chosen one, it is exported so
// agent-browser steers clear of a system Chromium that may crash silently
// under --remote-debugging-port. An operator-set value is never overridden.
func browserEnvironment() []string {
	env := os.Environ()
	if os.Getenv(browser.ExecutableEnv) == "" {
		if exe := browser.DiscoverExecutable(nil); exe != "" {
			env = append(env, browser.ExecutableEnv+"="+exe)
		}
	}
	return env
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
	cmd.Env = browserEnvironment()

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

		return ErrorResult(browserFailureMessage(action, err, stderr.String(), ctx))
	}

	output := strings.TrimSpace(stdout.String())
	return UserResult(output)
}

// browserFailureMessage renders one honest failure for a browser CLI
// call. Timeouts, transport drops, and kills are OUTCOME-UNKNOWN: the
// CLI may have dispatched the action before it died, so the message
// must forbid blind retries of mutating actions and point at
// re-observation instead. (Adopted from OpenCode's browser error
// semantics; the same honesty serves Ghost's trust line as the
// evidence rule — a claim never outruns what the runtime proved.)
func browserFailureMessage(action string, runErr error, stderr string, ctx context.Context) string {
	if ctx != nil && ctx.Err() != nil {
		return fmt.Sprintf("[browser.timeout] The browser '%s' action did not finish before its deadline — its outcome is unknown. "+
			"Do not repeat a mutating action until you know whether it ran: re-snapshot (browser_snapshot) and check the page state first. "+
			"Underlying error: %v", action, runErr)
	}
	lower := strings.ToLower(stderr + " " + runErr.Error())
	switch {
	case strings.Contains(lower, "unknown ref"):
		return fmt.Sprintf("Browser '%s' refused a stale element ref: %s. Re-snapshot and act on a fresh ref.", action, strings.TrimSpace(stderr))
	case containsAnyFold(lower, "websocket", "disconnected", "econnrefused", "target closed", "session not found",
		"connection closed", "browser is not running", "no browser", "target is closed", "tried to connect to closing browser"):
		return fmt.Sprintf("[browser.disconnected] The browser connection dropped during '%s' — the action may already have run. "+
			"Do not repeat a mutating action until its outcome is known: re-snapshot and verify page state (a fresh navigate may be needed to reconnect). "+
			"Underlying error: %v (stderr: %s)", action, runErr, boundedBrowserText(stderr))
	default:
		return fmt.Sprintf("Browser action '%s' failed: %v\nStderr: %s", action, runErr, stderr)
	}
}

// containsAnyFold reports whether s contains any of the substrings
// (case-insensitive; s is expected pre-lowered by the caller).
func containsAnyFold(lower string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// boundedBrowserText caps free-form CLI text carried in model-facing
// error strings.
func boundedBrowserText(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 400 {
		return string(r[:400]) + "…"
	}
	return s
}

func (t *BrowserTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	// The gate-attached binding takes precedence: model-reachable calls
	// run enforced. The Policy path is the previous slice's guarded mode
	// for explicitly configured callers; the bare path is legacy/internal.
	if _, ok := BrowserCallFromContext(ctx); ok {
		return t.executeEnforced(ctx, args)
	}
	if t.Policy != nil && t.Policy.Sessions != nil {
		return t.executeGuarded(ctx, args)
	}
	return t.executeBare(ctx, args)
}

// executeEnforced runs one gate-bound browser operation. Every check is
// server-side: owner/context/task come from the BrowserCall bag, never
// from tool args, so forged arguments cannot widen authority.
func (t *BrowserTool) executeEnforced(ctx context.Context, args map[string]interface{}) *ToolResult {
	call, _ := BrowserCallFromContext(ctx)
	deny := func(reason string) *ToolResult {
		return ErrorResult("Browser policy denied this operation: " + reason)
	}
	denyCode := func(code permissions.DenialCode, reason, remedy string) *ToolResult {
		return ErrorResult(policyDeny(code, "Browser policy: "+reason, remedy))
	}
	// denyResume codes restore-check failures: expired sessions expire,
	// everything else is a binding violation.
	denyResume := func(err error) *ToolResult {
		if err != nil && strings.Contains(err.Error(), "expired") {
			return denyCode(permissions.CodeSessionExpired, strings.TrimPrefix(err.Error(), "browser: "), "Ask again to start a fresh session.")
		}
		return denyCode(permissions.CodeBindingMismatch, strings.TrimPrefix(strings.TrimPrefix(err.Error(), "browser: "), "Browser policy denied this operation: "), "Ask again so the call binds to a live session.")
	}
	if call.Owner == "" || call.Sessions == nil {
		return denyCode(permissions.CodeBindingMismatch, "no owner or session ledger bound.", "Start from an authorized call so the runtime binds owner and session.")
	}
	// The gate binds one concrete operation; it must be this tool's own
	// action. An authorize-snapshot binding can never drive a click.
	if call.Op != "" && call.Op != t.action {
		return denyCode(permissions.CodeBindingMismatch, "operation binding mismatch.", "Call the operation the approval bound.")
	}
	op := t.Classify()
	if (op == "act" || op == "transact") && call.Permission == "" {
		return denyCode(permissions.CodePolicyDenied, "state-changing browser operation requires broker authorization.", "Ask for approval first, then retry.")
	}
	taskID := call.TaskID
	if taskID == "" {
		taskID = SessionKeyFromContext(ctx)
		if taskID == "" {
			return denyCode(permissions.CodeBindingMismatch, "no work item bound.", "Start the call inside a task or session so work binds.")
		}
	}
	started := time.Now().UTC()
	var sess *browser.Session
	if call.SessionID != "" {
		// Pinned session (approval resume): explicit restore-check —
		// the stored row must still belong to this owner/context/task
		// and still be live. Anything else fails closed.
		row, err := call.Sessions.Revalidate(call.SessionID, call.Owner, call.ContextID, taskID)
		if err != nil {
			return denyResume(err)
		}
		sess = row
		if err := call.Sessions.Touch(sess.ID, 0); err != nil {
			return denyCode(permissions.CodeSessionExpired, "browser session unavailable.", "Ask again to start a fresh session.")
		}
	} else {
		profile := call.Profile
		if profile == "" {
			profile = "default"
		}
		var err error
		sess, err = call.Sessions.GetOrCreate(call.Owner, call.ContextID, taskID, profile, 0)
		if err != nil {
			return denyCode(permissions.CodeSessionExpired, "browser session unavailable.", "Ask again to start a fresh session.")
		}
	}
	// Epoch pre-check: act-class ops must name refs from the session's
	// live snapshot epoch. Stale or never-observed refs fail here,
	// before the CLI runs — the model re-snapshots instead of acting
	// blind. Observations always pass through.
	for _, ref := range browserRefArgs(t.action, args) {
		if err := call.Sessions.CheckRef(sess.ID, ref); err != nil {
			if _, stale := err.(*browser.StaleRefError); stale {
				return denyCode(permissions.CodeRefStale, strings.TrimPrefix(err.Error(), "browser: "), "Re-snapshot and act on the fresh refs.")
			}
			return deny(err.Error())
		}
	}
	res := t.executeBare(ctx, args)
	outcome := "ok"
	if res.IsError {
		outcome = "error"
	}
	ev := browser.Evidence{
		Operation: t.action + ":" + op,
		SessionID: sess.ID, TaskID: taskID, ContextID: call.ContextID,
		Outcome: outcome, Detail: "browser." + t.action,
		StartedAt: started, EndedAt: time.Now().UTC(),
	}
	if call.OnEvidence != nil {
		call.OnEvidence(taskID, ev)
	}
	res.Evidence = map[string]interface{}{
		"type":       "action",
		"op":         "browser." + t.action,
		"class":      op,
		"owner":      call.Owner,
		"context":    call.ContextID,
		"task":       taskID,
		"session":    sess.ID,
		"permission": call.Permission,
		"outcome":    outcome,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
	// Page summary for safe observation: navigate/snapshot/find emit the
	// page as JSON. Parsed defensively, bounded, and redacted — the same
	// material the Live Surface plane may show the owner. Anything
	// unparseable is simply absent, never an error.
	if !res.IsError && browserActionObserves(t.action) {
		attachPageEvidence(res)
		// Open a new ref epoch from the observed snapshot and record
		// the page as an untrusted-content (taint) span: everything
		// page-derived enters model context marked, and the broker can
		// scope consequential approvals against tainted domains.
		epoch := call.Sessions.Observe(sess.ID, browser.ParseRefs(res.ForLLM))
		res.Evidence["ref_epoch"] = epoch
		if url, _ := res.Evidence["url"].(string); url != "" {
			domain, _ := res.Evidence["domain"].(string)
			call.Sessions.RecordTaint(browser.TaintSpan{SessionID: sess.ID, URL: url, Domain: domain})
			res.Evidence["taint_sources"] = call.Sessions.TaintedDomains(sess.ID)
		}
	}
	// Successful mutations close the epoch: the next act requires a
	// fresh snapshot. Epochs advance exactly on real mutations.
	if !res.IsError && browserActionMutates(t.action) {
		call.Sessions.Mutate(sess.ID)
	}
	if res.IsError || res.ForLLM == "" {
		return res
	}
	// Bounded visual capture on meaningful state changes only: navigation
	// and interactions that change the page. Snapshots already return full
	// state (capturing there too would double executor cost per observe
	// cycle); typing captures only when it submits (press_enter).
	if t.action == "navigate" || t.action == "click" || t.action == "submit" || t.action == "fill_form" || (t.action == "type" && submitsOnType(args)) {
		if path, ok := captureBrowserShot(ctx, sess.ID); ok {
			res.ScreenshotPath = path
		}
	}
	labeled := browser.ObserveText(res.ForLLM)
	// ScreenshotPath survives on purpose: the Live Surface plane streams
	// it to the owner, and the loop attaches it as model context for the
	// explicit browser_screenshot tool.
	return &ToolResult{ForLLM: labeled, ForUser: res.ForUser, Silent: res.Silent, IsError: false, Evidence: res.Evidence, ScreenshotPath: res.ScreenshotPath}
}

// browserRefArgs returns the element refs an action must validate
// against the live snapshot epoch before the CLI runs. Empty strings
// are never returned, so no validation is skipped or spuriously failed.
func browserRefArgs(action string, args map[string]interface{}) []string {
	str := func(key string) string {
		s, _ := args[key].(string)
		return strings.TrimSpace(s)
	}
	switch action {
	case "click", "type", "fill", "submit", "select", "check", "hover", "upload", "download":
		if r := str("ref"); r != "" {
			return []string{r}
		}
	case "drag":
		var out []string
		if r := str("source_ref"); r != "" {
			out = append(out, r)
		}
		if r := str("target_ref"); r != "" {
			out = append(out, r)
		}
		return out
	case "fill_form":
		fields, _ := args["fields"].([]interface{})
		var out []string
		for _, f := range fields {
			m, _ := f.(map[string]interface{})
			if r, _ := m["ref"].(string); strings.TrimSpace(r) != "" {
				out = append(out, strings.TrimSpace(r))
			}
		}
		return out
	}
	return nil
}

// browserActionObserves reports whether a successful run re-opens the
// ref epoch (its output carries fresh, actionable refs). Only
// snapshot-class observations do: wait/screenshot/console/network/
// a11y/scroll return no refs and must not invalidate the epoch the
// model is working from.
func browserActionObserves(action string) bool {
	switch action {
	case "navigate", "snapshot", "find":
		return true
	}
	return false
}

// browserActionMutates reports whether a successful run closes the ref
// epoch (page state may have changed, so the next act re-observes).
func browserActionMutates(action string) bool {
	switch action {
	case "click", "type", "fill", "submit", "press",
		"select", "check", "hover", "drag", "fill_form",
		"upload", "download", "dialog":
		return true
	}
	return false
}

// submitsOnType reports whether a type call submits its input.
func submitsOnType(args map[string]interface{}) bool {
	enter, _ := args["press_enter"].(bool)
	return enter
}

// submitBare executes a declared transactional submit: snapshot the page,
// detect checkout, bind the declared merchant+amount to the detected quote,
// click the submit ref, re-snapshot, and record receipt evidence. Nothing
// here authorizes payment — the gate/broker must approve first (high
// impact), and the approval is bound to the exact merchant+amount.
func (t *BrowserTool) submitBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return ErrorResult("ref is required")
	}
	snap := t.run(ctx, "snapshot")
	if snap.IsError {
		return ErrorResult("submit refused: couldn't read the page before submitting")
	}
	pageURL, pageText := snapshotURLText(snap.ForLLM)
	quote := browser.DetectCheckout(pageURL, pageText)
	merchant, _ := args["merchant"].(string)
	amount, _ := args["amount"].(string)
	if quote.IsCheckout {
		if !matchMerchant(merchant, quote.Merchant) {
			return ErrorResult(fmt.Sprintf("submit refused: declared merchant %q does not match page %q (total %s). Declare the exact merchant to proceed.",
				merchant, quote.Merchant, quote.Total))
		}
		if !matchAmount(amount, quote.Total) {
			return ErrorResult(fmt.Sprintf("submit refused: declared amount %q does not match page total %q at %s. Declare the exact total to proceed.",
				amount, quote.Total, quote.Merchant))
		}
	}
	clicked := t.run(ctx, "click", ref)
	if clicked.IsError {
		return ErrorResult(fmt.Sprintf("submit failed at click: %s", clicked.ForLLM))
	}
	after := t.run(ctx, "snapshot")
	confirmed := false
	afterURL, afterText := "", ""
	if !after.IsError {
		afterURL, afterText = snapshotURLText(after.ForLLM)
		confirmed = browser.ConfirmationKeywords(afterText)
	}
	res := NewToolResult(fmt.Sprintf("Submitted %s at %s (total %s). Confirmation observed: %v.",
		ref, quote.Merchant, quote.Total, confirmed))
	res.Evidence = map[string]interface{}{
		"type":      "action",
		"op":        "browser.submit",
		"class":     "transact",
		"merchant":  quote.Merchant,
		"amount":    quote.Total,
		"currency":  quote.Currency,
		"confirmed": confirmed,
		"url":       afterURL,
		"outcome":   "ok",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	return res
}

// snapshotURLText extracts url + text from agent-browser snapshot JSON
// (both wire shapes; raw output is the fallback text).
func snapshotURLText(output string) (string, string) {
	if view, ok := browserPageViewOf(output); ok && (view.URL != "" || view.Text != "") {
		return view.URL, view.Title + "\n" + view.Text
	}
	return "", output
}

// matchMerchant binds the declared merchant to the detected host
// (either direction, case-insensitive): approval for one merchant can
// never drive another.
func matchMerchant(declared, detected string) bool {
	declared = strings.ToLower(strings.TrimSpace(declared))
	detected = strings.ToLower(strings.TrimSpace(detected))
	if declared == "" || detected == "" {
		return false
	}
	return strings.Contains(detected, declared) || strings.Contains(declared, detected)
}

// matchAmount binds the declared total to the detected quote by numeric
// value (currency symbols and separators ignored).
func matchAmount(declared, detected string) bool {
	if strings.TrimSpace(declared) == "" || strings.TrimSpace(detected) == "" {
		return false
	}
	return parseAmountLoose(declared) == parseAmountLoose(detected) && parseAmountLoose(declared) != ""
}

func parseAmountLoose(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pageEvidenceBound caps page text carried in evidence/observations.
const pageEvidenceBound = 4000

// browserShotBound caps screenshot bytes (Pi 5 + mobile bandwidth).
const browserShotBound = 2 << 20

// browserShotKeep bounds retained screenshots per personal AI.
const browserShotKeep = 20

// captureBrowserShot attempts one bounded screenshot of the browser's
// current page after a meaningful visual state change (navigation or a
// state-changing interaction). Best-effort by contract: the CLI may not
// support capture, and any failure — non-zero exit, missing file,
// oversize, non-PNG bytes — silently yields no screenshot while the
// structured text observation stands. Accepted output is strictly a
// fresh PNG at the requested path. Files are latest-per-session
// transient observations, never persisted artifacts: the directory is
// pruned to a small bound on every capture.
func captureBrowserShot(ctx context.Context, sessionID string) (string, bool) {
	return captureBrowserShotWith(ctx, sessionID, defaultBrowserShotDir(), runBrowserScreenshot)
}

// runBrowserScreenshot invokes the browser CLI's capture command. The
// exact command surface belongs to the CLI; strict output acceptance in
// captureBrowserShotWith keeps unknown CLIs harmless.
func runBrowserScreenshot(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "agent-browser", "screenshot", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd.Run()
}

func defaultBrowserShotDir() string {
	return filepath.Join(os.TempDir(), "ghost-browser-shots")
}

func captureBrowserShotWith(
	ctx context.Context,
	sessionID, dir string,
	run func(ctx context.Context, path string) error,
) (string, bool) {
	safe := sanitizeShotSession(sessionID)
	if safe == "" || dir == "" || run == nil {
		return "", false
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", false
	}
	path := filepath.Join(dir, "shot-"+safe+".png")
	// Remove any stale file first so a failed capture can never serve
	// a previous page as the current observation.
	_ = os.Remove(path)
	if err := run(ctx, path); err != nil {
		return "", false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= 0 || fi.Size() > browserShotBound {
		_ = os.Remove(path)
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		_ = os.Remove(path)
		return "", false
	}
	magic := make([]byte, 8)
	_, err = f.Read(magic)
	_ = f.Close()
	if err != nil || string(magic) != "\x89PNG\r\n\x1a\n" {
		_ = os.Remove(path)
		return "", false
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = os.Remove(path)
		return "", false
	}
	pruneBrowserShots(dir)
	return path, true
}

// sanitizeShotSession keeps only filename-safe characters so session ids
// can never escape the shot directory.
func sanitizeShotSession(sessionID string) string {
	var b strings.Builder
	for _, r := range sessionID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 64 {
			break
		}
	}
	return b.String()
}

// pruneBrowserShots keeps the shot directory bounded by recency.
func pruneBrowserShots(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= browserShotKeep {
		return
	}
	type named struct {
		name string
		mod  time.Time
	}
	var pngs []named
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".png") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		pngs = append(pngs, named{e.Name(), fi.ModTime()})
	}
	for i := 0; i < len(pngs); i++ {
		for j := i + 1; j < len(pngs); j++ {
			if pngs[j].mod.Before(pngs[i].mod) {
				pngs[i], pngs[j] = pngs[j], pngs[i]
			}
		}
	}
	for _, p := range pngs[:len(pngs)-browserShotKeep] {
		_ = os.Remove(filepath.Join(dir, p.name))
	}
}

// browserPageView unwraps one CLI JSON payload (envelope or flat) into
// the page fields Ghost's evidence and submit flows read. Tolerates
// both wire shapes: the legacy {url,title,text} form and the current
// agent-browser {origin,refs,snapshot} form. ok=false only when the
// payload is not JSON at all.
type browserPageView struct {
	URL   string
	Title string
	Text  string
}

func browserPageViewOf(output string) (browserPageView, bool) {
	obj := []byte(output)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(obj, &env); err == nil && len(env.Data) > 0 && string(env.Data) != "null" {
		obj = env.Data
	}
	var page struct {
		URL      string `json:"url"`
		Origin   string `json:"origin"`
		Title    string `json:"title"`
		Text     string `json:"text"`
		Snapshot string `json:"snapshot"`
	}
	if err := json.Unmarshal(obj, &page); err != nil {
		return browserPageView{}, false
	}
	v := browserPageView{URL: page.URL, Title: page.Title, Text: page.Text}
	if v.URL == "" {
		v.URL = page.Origin
	}
	if v.Text == "" {
		v.Text = page.Snapshot
	}
	return v, true
}

// attachPageEvidence extracts url/title/text from raw agent-browser
// JSON into the result evidence map. Defensive by design: unparseable
// or empty output leaves evidence untouched.
func attachPageEvidence(res *ToolResult) {
	if res == nil || res.Evidence == nil {
		return
	}
	view, ok := browserPageViewOf(res.ForLLM)
	if !ok {
		return
	}
	if view.URL != "" {
		res.Evidence["url"] = view.URL
		if u, err := url.Parse(view.URL); err == nil && u.Host != "" {
			res.Evidence["domain"] = u.Host
		}
	}
	if view.Title != "" {
		res.Evidence["title"] = view.Title
	}
	if text := strings.TrimSpace(view.Text); text != "" {
		runes := []rune(text)
		if len(runes) > pageEvidenceBound {
			runes = runes[:pageEvidenceBound]
		}
		res.Evidence["text"] = redact.Any(string(runes))
	}
}

// mergeBrowserResults unions two CLI JSON payloads into one result:
// envelope keys are preserved and data objects are merged (the second
// payload wins on key conflicts). Used by navigate, which fetches the
// page identity and the accessibility tree in two CLI calls so the
// model's contract ("navigate returns the tree") and the ref epoch
// both hold. Non-JSON payloads pass through as the first result.
func mergeBrowserResults(a, b *ToolResult) *ToolResult {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	mergeJSON := func(dst, src string) (string, bool) {
		var dm, sm map[string]interface{}
		if json.Unmarshal([]byte(dst), &dm) != nil || json.Unmarshal([]byte(src), &sm) != nil {
			return "", false
		}
		if dData, ok := dm["data"].(map[string]interface{}); ok {
			if sData, ok := sm["data"].(map[string]interface{}); ok {
				for k, v := range sData {
					dData[k] = v
				}
				if out, err := json.Marshal(dm); err == nil {
					return string(out), true
				}
				return "", false
			}
		}
		for k, v := range sm {
			dm[k] = v
		}
		if out, err := json.Marshal(dm); err == nil {
			return string(out), true
		}
		return "", false
	}
	out := *a
	if merged, ok := mergeJSON(a.ForLLM, b.ForLLM); ok {
		out.ForLLM = merged
	}
	if merged, ok := mergeJSON(a.ForUser, b.ForUser); ok {
		out.ForUser = merged
	}
	return &out
}

// snapshotTreeOf extracts the accessibility tree text from snapshot
// CLI JSON (current data.snapshot, legacy data.text), falling back to
// the raw string when the payload is not JSON.
func snapshotTreeOf(output string) string {
	if view, ok := browserPageViewOf(output); ok && view.Text != "" {
		return view.Text
	}
	return output
}

// findTreeLines returns up to limit trimmed lines of tree containing
// needle (case-insensitive).
func findTreeLines(tree, needle string, limit int) []string {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(tree, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(line), needle) {
			continue
		}
		out = append(out, strings.TrimSpace(line))
		if len(out) >= limit {
			break
		}
	}
	return out
}

// projectBrowserOutput rewrites a CLI payload's data object in place,
// keeping the envelope shape. f returns false to leave output
// untouched. Non-JSON payloads are left alone.
func projectBrowserOutput(res *ToolResult, f func(data map[string]interface{}) bool) {
	if res == nil || res.IsError {
		return
	}
	var env map[string]interface{}
	if json.Unmarshal([]byte(res.ForLLM), &env) != nil {
		return
	}
	data, wrapped := env["data"].(map[string]interface{})
	if !wrapped {
		data = env
		env = nil
	}
	if !f(data) {
		return
	}
	var out []byte
	var err error
	if env != nil {
		env["data"] = data
		out, err = json.Marshal(env)
	} else {
		out, err = json.Marshal(data)
	}
	if err != nil {
		return
	}
	res.ForLLM = string(out)
	res.ForUser = string(out)
}

// boundStr caps a free-form string carried through projections.
func boundStr(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// projectConsoleOutput keeps console output bounded and recent-first:
// at most 40 messages, each field capped. Page-controlled text stays
// (the enforced path labels it untrusted), but it can never flood
// context.
func projectConsoleOutput(res *ToolResult) {
	projectBrowserOutput(res, func(data map[string]interface{}) bool {
		msgs, _ := data["messages"].([]interface{})
		if msgs == nil {
			return false
		}
		const maxMsgs = 40
		start := 0
		if len(msgs) > maxMsgs {
			start = len(msgs) - maxMsgs
		}
		out := make([]interface{}, 0, len(msgs)-start)
		for _, m := range msgs[start:] {
			mm, _ := m.(map[string]interface{})
			if mm == nil {
				continue
			}
			entry := map[string]interface{}{}
			if lv, ok := mm["type"].(string); ok {
				entry["level"] = lv
			} else if lv, ok := mm["level"].(string); ok {
				entry["level"] = lv
			}
			if txt, ok := mm["text"].(string); ok {
				entry["text"] = boundStr(txt, 400)
			} else if txt, ok := mm["message"].(string); ok {
				entry["text"] = boundStr(txt, 400)
			}
			if loc, ok := mm["location"].(map[string]interface{}); ok {
				u, _ := loc["url"].(string)
				if u != "" {
					entry["at"] = boundStr(fmt.Sprintf("%v:%v", u, loc["lineNumber"]), 200)
				}
			}
			if ts, ok := mm["timestamp"].(float64); ok {
				entry["ts"] = ts
			}
			out = append(out, entry)
		}
		data["messages"] = out
		if len(msgs) > maxMsgs {
			data["note"] = fmt.Sprintf("last %d of %d messages", len(out), len(msgs))
		}
		return true
	})
}

// projectNetworkOutput keeps only header-free request summaries
// (method, URL, status, type, timing). Headers, cookies, auth
// material, and bodies are dropped here — before any model-bound
// redaction runs — so credentials never ride along as bulk page data.
func projectNetworkOutput(res *ToolResult) {
	projectBrowserOutput(res, func(data map[string]interface{}) bool {
		reqs, _ := data["requests"].([]interface{})
		if reqs == nil {
			return false
		}
		const maxReqs = 60
		start := 0
		if len(reqs) > maxReqs {
			start = len(reqs) - maxReqs
		}
		allow := []string{"method", "url", "status", "statusText", "mimeType",
			"type", "resourceType", "fromCache", "failure", "wallTime",
			"startTime", "encodedDataLength", "decodedDataLength"}
		out := make([]interface{}, 0, len(reqs)-start)
		for _, r := range reqs[start:] {
			rm, _ := r.(map[string]interface{})
			if rm == nil {
				continue
			}
			entry := map[string]interface{}{}
			for _, k := range allow {
				if v, ok := rm[k]; ok {
					entry[k] = v
				}
			}
			if u, ok := entry["url"].(string); ok {
				entry["url"] = boundStr(u, 300)
			}
			out = append(out, entry)
		}
		data["requests"] = out
		if len(reqs) > maxReqs {
			data["note"] = fmt.Sprintf("last %d of %d requests", len(out), len(reqs))
		}
		return true
	})
}

// projectA11yOutput keeps the audit's signal (counts + violations with
// example targets) and drops pass/incomplete node dumps, which are
// large and carry no actionable violation.
func projectA11yOutput(res *ToolResult) {
	projectBrowserOutput(res, func(data map[string]interface{}) bool {
		viols, _ := data["violations"].([]interface{})
		if viols == nil {
			return false
		}
		delete(data, "passes")
		delete(data, "incomplete")
		out := make([]interface{}, 0, len(viols))
		for _, v := range viols {
			vm, _ := v.(map[string]interface{})
			if vm == nil {
				continue
			}
			entry := map[string]interface{}{}
			for _, k := range []string{"id", "impact", "help", "description"} {
				if val, ok := vm[k]; ok {
					entry[k] = val
				}
			}
			if nodes, ok := vm["nodes"].([]interface{}); ok {
				entry["nodes"] = len(nodes)
				var examples []interface{}
				for i, n := range nodes {
					if i >= 3 {
						break
					}
					nm, _ := n.(map[string]interface{})
					if nm == nil {
						continue
					}
					if tgt, ok := nm["target"].([]interface{}); ok && len(tgt) > 0 {
						examples = append(examples, fmt.Sprintf("%v", tgt[0]))
					} else if html, ok := nm["html"].(string); ok {
						examples = append(examples, boundStr(html, 160))
					}
				}
				if len(examples) > 0 {
					entry["examples"] = examples
				}
			}
			out = append(out, entry)
		}
		data["violations"] = out
		return true
	})
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
		if ok, reason := ValidateURL(url, URLSafetyConfig{}); !ok {
			return ErrorResult(fmt.Sprintf("Browser navigation refused: %s", reason))
		}
		nav := t.run(ctx, "navigate", url)
		if nav.IsError {
			return nav
		}
		// navigate returns only url/title; the model's contract (and the
		// ref epoch) promises the accessibility tree. Take the snapshot
		// in the same call so refs are live the moment navigation
		// succeeds. A snapshot failure is not a navigation failure — the
		// model sees the page URL and snapshots next.
		snap := t.run(ctx, "snapshot")
		if snap.IsError {
			return nav
		}
		return mergeBrowserResults(nav, snap)

	case "snapshot":
		return t.run(ctx, "snapshot")

	case "click":
		ref, _ := args["ref"].(string)
		if ref == "" {
			return ErrorResult("ref is required")
		}
		return t.run(ctx, "click", ref)

	case "fill":
		ref, _ := args["ref"].(string)
		text, _ := args["text"].(string)
		if ref == "" || text == "" {
			return ErrorResult("ref and text are required")
		}
		return t.run(ctx, "fill", ref, text)

	case "submit":
		return t.submitBare(ctx, args)

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

	case "wait":
		return t.waitBare(ctx, args)

	case "find":
		return t.findBare(ctx, args)

	case "screenshot":
		return t.screenshotBare(ctx)

	case "scroll":
		dir, _ := args["direction"].(string)
		switch strings.ToLower(strings.TrimSpace(dir)) {
		case "":
			dir = "down"
		case "up", "down", "left", "right":
			dir = strings.ToLower(strings.TrimSpace(dir))
		default:
			return ErrorResult("direction must be one of: up, down, left, right")
		}
		px := 300
		if v, ok := args["pixels"].(float64); ok && v > 0 {
			px = int(v)
		}
		if px > 20000 {
			px = 20000
		}
		return t.run(ctx, "scroll", dir, fmt.Sprintf("%d", px))

	case "console":
		res := t.run(ctx, "console")
		if !res.IsError {
			projectConsoleOutput(res)
		}
		return res

	case "network":
		cliArgs := []string{"requests"}
		if filter, _ := args["filter"].(string); strings.TrimSpace(filter) != "" {
			cliArgs = append(cliArgs, "--filter", strings.TrimSpace(filter))
		}
		res := t.run(ctx, "network", cliArgs...)
		if !res.IsError {
			projectNetworkOutput(res)
		}
		return res

	case "a11y":
		res := t.run(ctx, "a11y")
		if !res.IsError {
			projectA11yOutput(res)
		}
		return res

	case "select":
		ref, _ := args["ref"].(string)
		value, _ := args["value"].(string)
		if ref == "" || value == "" {
			return ErrorResult("ref and value are required")
		}
		return t.run(ctx, "select", ref, value)

	case "check":
		ref, _ := args["ref"].(string)
		if ref == "" {
			return ErrorResult("ref is required")
		}
		checked := true
		if v, ok := args["checked"].(bool); ok {
			checked = v
		}
		if checked {
			return t.run(ctx, "check", ref)
		}
		return t.run(ctx, "uncheck", ref)

	case "hover":
		ref, _ := args["ref"].(string)
		if ref == "" {
			return ErrorResult("ref is required")
		}
		return t.run(ctx, "hover", ref)

	case "drag":
		src, _ := args["source_ref"].(string)
		dst, _ := args["target_ref"].(string)
		if src == "" || dst == "" {
			return ErrorResult("source_ref and target_ref are required")
		}
		return t.run(ctx, "drag", src, dst)

	case "fill_form":
		return t.fillFormBare(ctx, args)

	case "dialog":
		action, _ := args["action"].(string)
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "accept":
			text, _ := args["text"].(string)
			if strings.TrimSpace(text) != "" {
				return t.run(ctx, "dialog", "accept", text)
			}
			return t.run(ctx, "dialog", "accept")
		case "dismiss":
			return t.run(ctx, "dialog", "dismiss")
		case "status":
			return t.run(ctx, "dialog", "status")
		default:
			return ErrorResult("action must be one of: accept, dismiss, status")
		}

	case "upload":
		return t.uploadBare(ctx, args)

	case "download":
		return t.downloadBare(ctx, args)

	default:
		return ErrorResult(fmt.Sprintf("Unknown browser action: %s", t.action))
	}
}

// waitBare maps Ghost's condition-style wait onto the CLI's wait modes.
// Exactly one condition is required; no fixed sleeps are ever issued
// (the model names what it needs, the CLI waits for that).
func (t *BrowserTool) waitBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	selector, _ := args["selector"].(string)
	text, _ := args["text"].(string)
	urlPat, _ := args["url"].(string)
	load, _ := args["load"].(string)
	conditions := 0
	for _, c := range []string{selector, text, urlPat, load} {
		if strings.TrimSpace(c) != "" {
			conditions++
		}
	}
	if conditions == 0 {
		return ErrorResult("one condition is required: selector, text, url, or load")
	}
	if conditions > 1 {
		return ErrorResult("give exactly one wait condition: selector, text, url, or load")
	}
	if v, ok := args["timeout_ms"].(float64); ok && v > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(v)*time.Millisecond)
		defer cancel()
	}
	switch {
	case strings.TrimSpace(selector) != "":
		return t.run(ctx, "wait", strings.TrimSpace(selector))
	case strings.TrimSpace(text) != "":
		return t.run(ctx, "wait", "--text", strings.TrimSpace(text))
	case strings.TrimSpace(urlPat) != "":
		return t.run(ctx, "wait", "--url", strings.TrimSpace(urlPat))
	default:
		load = strings.TrimSpace(load)
		if load != "load" && load != "domcontentloaded" && load != "networkidle" {
			return ErrorResult("load must be one of: load, domcontentloaded, networkidle")
		}
		return t.run(ctx, "wait", "--load", load)
	}
}

// findBare reads a fresh snapshot and returns the lines whose text
// matches, with their refs. Deterministic Ghost-side search — no new
// CLI surface, and the refs it returns open the epoch via the enforced
// observation path (browserActionObserves includes "find").
func (t *BrowserTool) findBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	needle, _ := args["text"].(string)
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return ErrorResult("text is required")
	}
	snap := t.run(ctx, "snapshot")
	if snap.IsError {
		return ErrorResult("find failed: could not read the page (" + boundedBrowserText(snap.ForLLM) + ")")
	}
	tree := snapshotTreeOf(snap.ForLLM)
	matches := findTreeLines(tree, needle, 20)
	if len(matches) == 0 {
		return NewToolResult(fmt.Sprintf("No element containing %q on this page. Re-check the wording, or browser_snapshot to see everything.", needle))
	}
	return NewToolResult(fmt.Sprintf("Elements containing %q (act on these fresh refs; they supersede your last snapshot):\n%s",
		needle, strings.Join(matches, "\n")))
}

// screenshotBare captures the current page and returns the path; the
// loop attaches the image as model context when vision is available.
func (t *BrowserTool) screenshotBare(ctx context.Context) *ToolResult {
	tag := fmt.Sprintf("manual-%d", time.Now().UnixNano())
	path, ok := captureBrowserShotWith(ctx, tag, defaultBrowserShotDir(), runBrowserScreenshot)
	if !ok {
		return ErrorResult("Screenshot capture failed (the page may not be capturable right now). Read page state with browser_find or browser_snapshot instead.")
	}
	return &ToolResult{
		ForLLM:         "Screenshot captured: " + path + " — attached as an image to this turn when the model supports vision; otherwise use browser_find.",
		ForUser:        "Screenshot saved: " + path,
		ScreenshotPath: path,
	}
}

// fillFormBare fills each field in order through the same CLI runner
// the gate binds, collecting a per-field report. Partial success is
// reported honestly (which fields filled, which failed and why); an
// all-failed run is an error.
func (t *BrowserTool) fillFormBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	fields, _ := args["fields"].([]interface{})
	if len(fields) == 0 {
		return ErrorResult("fields is required (array of {ref, text})")
	}
	if len(fields) > 10 {
		return ErrorResult("fill_form accepts at most 10 fields per call")
	}
	var filled, failed []string
	for i, f := range fields {
		m, _ := f.(map[string]interface{})
		ref, _ := m["ref"].(string)
		text, _ := m["text"].(string)
		if strings.TrimSpace(ref) == "" {
			failed = append(failed, fmt.Sprintf("field %d: ref missing", i+1))
			continue
		}
		res := t.run(ctx, "fill", ref, text)
		if res.IsError {
			failed = append(failed, fmt.Sprintf("field %d (%s): %s", i+1, ref, boundedBrowserText(res.ForLLM)))
			continue
		}
		filled = append(filled, ref)
	}
	if len(filled) == 0 {
		return ErrorResult("fill_form filled no fields:\n" + strings.Join(failed, "\n"))
	}
	report := fmt.Sprintf("Filled %d/%d fields: %s", len(filled), len(fields), strings.Join(filled, ", "))
	if len(failed) > 0 {
		report += "\nNot filled:\n" + strings.Join(failed, "\n") + "\nRe-snapshot to see the current form state before retrying."
	}
	return NewToolResult(report)
}

// uploadBare sends local files to a page file input. Each path must
// exist and be a regular file; the ask (broker approval) carries these
// exact paths because upload is high impact — contents leave the
// device for the page's origin and approval is never auto-authorized.
func (t *BrowserTool) uploadBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return ErrorResult("ref is required")
	}
	raw, _ := args["paths"].([]interface{})
	if len(raw) == 0 {
		return ErrorResult("paths is required (array of local file paths)")
	}
	if len(raw) > 5 {
		return ErrorResult("upload accepts at most 5 files per call")
	}
	paths := make([]string, 0, len(raw))
	for _, p := range raw {
		s, _ := p.(string)
		s = strings.TrimSpace(s)
		if s == "" {
			return ErrorResult("paths contains an empty value")
		}
		abs, err := filepath.Abs(s)
		if err != nil {
			return ErrorResult(fmt.Sprintf("upload path %q is not resolvable: %v", s, err))
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return ErrorResult(fmt.Sprintf("upload refused: file %q does not exist on this device", s))
		}
		if !fi.Mode().IsRegular() {
			return ErrorResult(fmt.Sprintf("upload refused: %q is not a regular file", s))
		}
		paths = append(paths, abs)
	}
	return t.run(ctx, "upload", append([]string{ref}, paths...)...)
}

// downloadDir is Ghost's managed download location: downloads never
// write to model-chosen paths — only inside this directory.
func (t *BrowserTool) downloadDir() string {
	return filepath.Join(t.workspace, "state", "browser-downloads")
}

// downloadBare clicks to download into the managed directory and
// returns where the file landed.
func (t *BrowserTool) downloadBare(ctx context.Context, args map[string]interface{}) *ToolResult {
	ref, _ := args["ref"].(string)
	if ref == "" {
		return ErrorResult("ref is required")
	}
	dir := t.downloadDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ErrorResult(fmt.Sprintf("download directory unavailable: %v", err))
	}
	dest := filepath.Join(dir, fmt.Sprintf("download-%d", time.Now().UnixNano()))
	res := t.run(ctx, "download", ref, dest)
	if res.IsError {
		return res
	}
	return NewToolResult(fmt.Sprintf("Download saved: %s (verify the file exists before telling the user it succeeded).", dest))
}
