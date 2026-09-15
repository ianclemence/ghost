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
func (t *BrowserTool) Classify() string {
	switch t.action {
	case "navigate", "snapshot":
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
	if call.Owner == "" || call.Sessions == nil {
		return deny("no owner or session ledger bound")
	}
	// The gate binds one concrete operation; it must be this tool's own
	// action. An authorize-snapshot binding can never drive a click.
	if call.Op != "" && call.Op != t.action {
		return deny("operation binding mismatch")
	}
	op := t.Classify()
	if (op == "act" || op == "transact") && call.Permission == "" {
		return deny("state-changing browser operation requires broker authorization")
	}
	taskID := call.TaskID
	if taskID == "" {
		taskID = SessionKeyFromContext(ctx)
		if taskID == "" {
			return deny("no work item bound")
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
			return deny(err.Error())
		}
		sess = row
		if err := call.Sessions.Touch(sess.ID, 0); err != nil {
			return deny("browser session unavailable")
		}
	} else {
		profile := call.Profile
		if profile == "" {
			profile = "default"
		}
		var err error
		sess, err = call.Sessions.GetOrCreate(call.Owner, call.ContextID, taskID, profile, 0)
		if err != nil {
			return deny("browser session unavailable")
		}
	}
	// Epoch pre-check: act-class ops must name a ref from the session's
	// live snapshot epoch. Stale or never-observed refs fail here,
	// before the CLI runs — the model re-snapshots instead of acting
	// blind. Observations (snapshot/navigate) always pass through.
	if ref, ok := args["ref"].(string); ok && ref != "" && (t.action == "click" || t.action == "type" || t.action == "fill" || t.action == "submit") {
		if err := call.Sessions.CheckRef(sess.ID, ref); err != nil {
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
	// Page summary for safe observation: navigate/snapshot emit the page
	// as JSON. Parsed defensively, bounded, and redacted — the same
	// material the Live Surface plane may show the owner. Anything
	// unparseable is simply absent, never an error.
	if !res.IsError && (t.action == "navigate" || t.action == "snapshot") {
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
	if !res.IsError && (t.action == "click" || t.action == "type" || t.action == "fill" || t.action == "submit" || t.action == "press") {
		call.Sessions.Mutate(sess.ID)
	}
	if res.IsError || res.ForLLM == "" {
		return res
	}
	// Bounded visual capture on meaningful state changes only: navigation
	// and interactions that change the page. Snapshots already return full
	// state (capturing there too would double executor cost per observe
	// cycle); typing captures only when it submits (press_enter).
	if t.action == "navigate" || t.action == "click" || t.action == "submit" || (t.action == "type" && submitsOnType(args)) {
		if path, ok := captureBrowserShot(ctx, sess.ID); ok {
			res.ScreenshotPath = path
		}
	}
	labeled := browser.ObserveText(res.ForLLM)
	return &ToolResult{ForLLM: labeled, ForUser: res.ForUser, Silent: res.Silent, IsError: false, Evidence: res.Evidence}
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

// snapshotURLText extracts url + text from agent-browser snapshot JSON.
func snapshotURLText(output string) (string, string) {
	var page struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(output), &page); err != nil {
		return "", output
	}
	return page.URL, page.Title + "\n" + page.Text
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

// browserShotKeep bounds retained screenshots per appliance.
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

// attachPageEvidence extracts url/title/text from raw agent-browser page
// JSON into the result evidence map. Defensive by design: unparseable or
// empty output leaves evidence untouched.
func attachPageEvidence(res *ToolResult) {
	if res == nil || res.Evidence == nil {
		return
	}
	var page struct {
		URL   string `json:"url"`
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &page); err != nil {
		return
	}
	if page.URL != "" {
		res.Evidence["url"] = page.URL
		if u, err := url.Parse(page.URL); err == nil && u.Host != "" {
			res.Evidence["domain"] = u.Host
		}
	}
	if page.Title != "" {
		res.Evidence["title"] = page.Title
	}
	if text := strings.TrimSpace(page.Text); text != "" {
		runes := []rune(text)
		if len(runes) > pageEvidenceBound {
			runes = runes[:pageEvidenceBound]
		}
		res.Evidence["text"] = redact.Any(string(runes))
	}
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
		return t.run(ctx, "navigate", url)

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

	default:
		return ErrorResult(fmt.Sprintf("Unknown browser action: %s", t.action))
	}
}
