package agent

// Governance hooks: the agent loop's connection to the Ghost substrate —
// canonical Ghost identity, permission broker, and canonical event
// stream. All hooks are nil-safe: an unwired loop behaves exactly as
// before (fail-open only in the sense that nothing changes; the broker
// itself fails closed when consulted).
//
// Execution authority rule: the broker verdict — not the model — decides
// whether a consequential tool call runs. On ASK the turn pauses durably
// (SQLite permission request + continuation) and the model is told to
// present inline approval actions. Approval resolves via the API and the
// NEXT turn resumes through PendingForRequest — the original intent is
// never restarted and never repeated by the user.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/permissions"
)

// Governance carries the substrate handles for one AgentLoop.
type Governance struct {
	Events *cevents.Stream
	Broker *permissions.Broker
	// Contexts scopes sessions (nil = single personal context behavior).
	Contexts *contexts.Store
	GhostID  string
	AgentID  string

	capMu    sync.Mutex
	seenCaps map[string]bool
	// routineCtx scopes unattended routine turns (session → scope).
	routineCtx map[string]routineScope
}

func (g *Governance) active() bool { return g != nil }

// TurnStarted opens the canonical trace for a request. trajectoryID links
// every event of this execution (model calls, tools, verification,
// fallback); empty when the entry path minted no trajectory.
func (g *Governance) TurnStarted(requestID, sessionKey, channel, trajectoryID string) {
	if !g.active() || g.Events == nil {
		return
	}
	g.Events.Publish(&cevents.Event{
		Type: cevents.AgentStarted, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID,
		Payload: map[string]interface{}{"channel": channel},
	})
}

// TurnEnded closes the trace with the canonical outcome.
func (g *Governance) TurnEnded(requestID, sessionKey, trajectoryID string, err error) {
	if !g.active() || g.Events == nil {
		return
	}
	typ := cevents.AgentCompleted
	status := "success"
	if err != nil {
		typ = cevents.AgentFailed
		status = "failed"
	}
	g.Events.Publish(&cevents.Event{
		Type: typ, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID, Status: status,
	})
}

// CapabilityCommitted records capability.started once per turn.
func (g *Governance) CapabilityCommitted(requestID, sessionKey, capabilityID, trajectoryID string) {
	if !g.active() || g.Events == nil {
		return
	}
	g.Events.Publish(&cevents.Event{
		Type: cevents.CapabilityStarted, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID,
		Payload: map[string]interface{}{"capability": capabilityID},
	})
}

// ToolRan records tool completion/failure (user-safe summaries only;
// raw outputs never enter payloads).
func (g *Governance) ToolRan(requestID, sessionKey, tool, trajectoryID string, failed bool) {
	if !g.active() || g.Events == nil {
		return
	}
	typ := cevents.ToolCompleted
	status := "success"
	if failed {
		typ = cevents.ToolFailed
		status = "failed"
	}
	// ToolStarted is transient by taxonomy (never persisted); completed
	// and failed are durable outcomes.
	g.Events.Publish(&cevents.Event{
		Type: typ, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID, Status: status,
		Payload: map[string]interface{}{"tool": tool},
	})
}

// CapabilityDone records the terminal capability outcome.
func (g *Governance) CapabilityDone(requestID, sessionKey, capabilityID, trajectoryID string, failed bool) {
	if !g.active() || g.Events == nil {
		return
	}
	typ := cevents.CapabilityCompleted
	status := "success"
	if failed {
		typ = cevents.CapabilityFailed
		status = "failed"
	}
	g.Events.Publish(&cevents.Event{
		Type: typ, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID, Status: status,
		Payload: map[string]interface{}{"capability": capabilityID},
	})
}

// VerificationRan records a world-state verification outcome for a
// VerifiableTool: the tool ran, then the runtime independently confirmed
// (or refuted) the claimed outcome. A failed verification is durable
// evidence — it is what lets Doctor distinguish "tool errored" from
// "tool lied".
func (g *Governance) VerificationRan(sessionKey, tool, trajectoryID string, latencyMs int64, failed bool, detail string) {
	if !g.active() || g.Events == nil {
		return
	}
	typ := cevents.VerificationCompleted
	status := "success"
	if failed {
		typ = cevents.VerificationFailed
		status = "failed"
	}
	payload := map[string]interface{}{"tool": tool, "latency_ms": latencyMs}
	if detail != "" {
		payload["detail"] = detail
	}
	g.Events.Publish(&cevents.Event{
		Type: typ, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID, Status: status,
		Payload: payload,
	})
}

// FallbackRan records engaging a non-primary model candidate after the
// primary failed or was unavailable. from/to are "provider/model" specs;
// escalated is true when the switch crosses local→cloud.
func (g *Governance) FallbackRan(requestID, sessionKey, trajectoryID, from, to string, escalated bool) {
	if !g.active() || g.Events == nil {
		return
	}
	typ := cevents.FallbackStarted
	if escalated {
		typ = cevents.ModelEscalated
	}
	g.Events.Publish(&cevents.Event{
		Type: typ, RequestID: requestID, SessionID: sessionKey,
		GhostID: g.GhostID, AgentID: g.AgentID, TrajectoryID: trajectoryID, Status: "success",
		Payload: map[string]interface{}{"from": from, "to": to, "escalated": escalated},
	})
}

// AuthorizeResult is the gate decision for one tool call.
type AuthorizeResult struct {
	// Allowed: the call may execute.
	Allowed bool
	// AskMessage is the model-facing pause text when approval is needed.
	AskMessage string
	// PendingID identifies the durable request for approval UX.
	PendingID string
}

// AuthorizeTool enforces the broker BEFORE consequential execution.
// Read-only/low-risk calls pass through (broker policy decides);
// consequential/high-impact calls without a grant return allowed=false
// with a durable PENDING request the approval UI resolves. The model can
// never bypass this: the gate sits between capability resolution and
// ExecuteWithContext, not in a prompt.
func (g *Governance) AuthorizeTool(requestID, sessionKey, capabilityID, tool string, args map[string]interface{}) AuthorizeResult {
	return g.authorize(requestID, sessionKey, capabilityID, tool, args, authorizedToolRisk(capabilityID, tool))
}

// authorizedToolRisk returns the broker risk class for one tool call inside a
// committed capability. The floor is the capability's declared risk, BUT
// privileged execution primitives (exec, sandbox) are ALWAYS classified
// high_impact, no matter how low-risk the surrounding skill declares itself.
// Without this, a read-only skill that lists exec in its AllowedTools (for a
// legitimate curl) would let a skill's instructions run ARBITRARY shell with
// no broker decision. This closes that gap: shell still runs (the skill may
// legitimately need it), but only with an explicit grant/approval and
// evidence — never silently.
func authorizedToolRisk(capabilityID, tool string) permissions.Risk {
	switch tool {
	case "exec", "sandbox":
		return permissions.RiskHighImpact
	}
	return permissions.RiskOf(capabilityID)
}

// AuthorizeStandalone authorizes a standalone consequential tool (one not
// tied to a committed capability) with an explicit risk class. The risk is
// declared by the runtime table (pkg/tools.FreeConsequentialTools), never
// by the model.
func (g *Governance) AuthorizeStandalone(requestID, sessionKey, capabilityID, tool string, args map[string]interface{}, risk permissions.Risk) AuthorizeResult {
	return g.authorize(requestID, sessionKey, capabilityID, tool, args, risk)
}

func (g *Governance) authorize(requestID, sessionKey, capabilityID, tool string, args map[string]interface{}, risk permissions.Risk) AuthorizeResult {
	if !g.active() || g.Broker == nil {
		return AuthorizeResult{Allowed: true}
	}
	// Routine scope: unattended runs stay inside their allowed
	// capabilities. This cannot escalate: scope is set by the scheduler
	// executor (code), never by model output.
	if !g.routineAllows(sessionKey, capabilityID) {
		return AuthorizeResult{Allowed: false,
			AskMessage: "That isn't part of this routine, so I didn't run it."}
	}
	// Context scope: a session inside a scoped context cannot use
	// capabilities outside its allowlist (set by explicit user action
	// through the contexts API, never by the model).
	if !g.contextAllows(sessionKey, capabilityID) {
		return AuthorizeResult{Allowed: false,
			AskMessage: "That isn't available in this context, so I didn't run it."}
	}
	scope := scopeFor(sessionKey, args)
	switch g.Broker.Evaluate(capabilityID, toolAction(tool, args), scope, risk) {
	case permissions.VerdictAllow:
		return AuthorizeResult{Allowed: true}
	case permissions.VerdictDeny:
		return AuthorizeResult{Allowed: false,
			AskMessage: "That action isn't allowed. It was declined by permission policy, so I didn't run it."}
	default:
		req, err := g.Broker.Require(requestID, sessionKey, g.AgentID, capabilityID,
			toolAction(tool, args), scopeTarget(args), humanReason(capabilityID, tool, args),
			risk, continuationOf(args))
		if err != nil {
			return AuthorizeResult{Allowed: false,
				AskMessage: "I couldn't prepare the approval request. Nothing was run."}
		}
		return AuthorizeResult{Allowed: false, PendingID: req.ID,
			AskMessage: approvalAskText(capabilityID, tool, args, req.ID)}
	}
}

// ConsumeApproval atomically claims an approved allow_once request for
// this turn: one approval executes exactly once. allow_always grants
// never reach this path (Evaluate allows them directly).
func (g *Governance) ConsumeApproval(requestID string) bool {
	if !g.active() || g.Broker == nil {
		return false
	}
	_, ok := g.Broker.ConsumeApproved(requestID)
	return ok
}

func toolAction(tool string, args map[string]interface{}) string {
	if a, _ := args["action"].(string); a != "" {
		return tool + ":" + a
	}
	return tool
}

func scopeFor(sessionKey string, args map[string]interface{}) string {
	// Single source of truth lives in the broker package; this adapter
	// only extracts the untyped tool args. Narrowest stable scope:
	// explicit target > contact > session owner.
	to, _ := args["to"].(string)
	contact, _ := args["contact"].(string)
	return permissions.ScopeFor(sessionKey, to, contact)
}

func scopeTarget(args map[string]interface{}) string {
	if t, _ := args["to"].(string); t != "" {
		return fmt.Sprintf("%v", t)
	}
	return ""
}

func humanReason(capabilityID, tool string, args map[string]interface{}) string {
	target := scopeTarget(args)
	if target != "" {
		return fmt.Sprintf("%s via %s (%s)", capabilityID, tool, target)
	}
	return fmt.Sprintf("%s via %s", capabilityID, tool)
}

func continuationOf(args map[string]interface{}) map[string]string {
	out := map[string]string{}
	for k, v := range args {
		// Never persist secret-shaped values into continuations.
		lk := strings.ToLower(k)
		if strings.Contains(lk, "key") || strings.Contains(lk, "token") || strings.Contains(lk, "secret") {
			continue
		}
		if s, ok := v.(string); ok && len(s) < 500 {
			out[k] = s
		}
	}
	return out
}

func approvalAskText(capabilityID, tool string, args map[string]interface{}, pendingID string) string {
	desc := humanReason(capabilityID, tool, args)
	return fmt.Sprintf("I can do that (%s), but I need your approval first.\n\nActions: [allow_once] [always_allow] [deny] (permission request %s).", desc, pendingID)
}

// approvalPhrases match chat replies that answer a pending approval
// card. Matching ONLY applies when a PENDING request exists for the
// session — ordinary chat containing these words is never hijacked.
var approvalPhrases = map[string]permissions.GrantType{
	"allow once": permissions.GrantOnce, "approve": permissions.GrantOnce,
	"allow": permissions.GrantOnce, "yes": permissions.GrantOnce,
	"always allow": permissions.GrantAlways, "allow always": permissions.GrantAlways,
	"deny": permissions.GrantDeny, "deny it": permissions.GrantDeny,
	"no": permissions.GrantDeny, "cancel": permissions.GrantDeny,
}

// ResumeOutcome is a deterministically resumed approval: no LLM restart,
// no repeated request — the paused call re-executes with its preserved
// continuation and the runtime reports the verified result. Grant records
// how the approval was given so the resume path can re-verify standing
// grants (revocation) instead of treating every resume identically.
type ResumeOutcome struct {
	Resumed    bool
	Denied     bool
	Capability string
	Tool       string
	Args       map[string]interface{}
	Grant      permissions.GrantType
	Message    string
}

// CheckApprovalReply interprets a chat message as an answer to the
// session's pending approval card. Returns Resumed=false for ordinary
// chat (or when nothing is pending).
func (g *Governance) CheckApprovalReply(sessionKey, text string) ResumeOutcome {
	if !g.active() || g.Broker == nil {
		return ResumeOutcome{}
	}
	normalized := strings.ToLower(strings.TrimSpace(text))
	grant, ok := approvalPhrases[normalized]
	if !ok {
		return ResumeOutcome{}
	}
	pending, ok := g.Broker.PendingForSession(sessionKey)
	if !ok {
		return ResumeOutcome{}
	}
	contArgs := map[string]interface{}{}
	for k, v := range pending.Continuation {
		contArgs[k] = v
	}
	resolved, err := g.Broker.Resolve(pending.ID, grant, scopeFor(sessionKey, contArgs))
	if err != nil {
		return ResumeOutcome{Message: "That approval already expired. Please make the request again."}
	}
	if grant == permissions.GrantDeny {
		return ResumeOutcome{Denied: true,
			Message: "Understood — I didn't run it. Nothing was changed."}
	}
	// allow_once resumes by consuming the approval (exactly one execution
	// per approval — a second reply finds nothing to consume). allow_always
	// stores a standing grant and resumes from the approved request; the
	// caller re-evaluates current policy before executing so a revoked
	// grant stops the resume.
	if grant == permissions.GrantOnce {
		consumed, ok := g.Broker.ConsumeApproved(resolved.RequestID)
		if !ok {
			return ResumeOutcome{Message: "That approval was already used or expired. Please make the request again."}
		}
		return resumeFrom(consumed, grant)
	}
	approved, ok := g.Broker.ApprovedRequest(resolved.RequestID)
	if !ok {
		return ResumeOutcome{Message: "That approval couldn't be applied. Please make the request again."}
	}
	return resumeFrom(approved, grant)
}

// resumeFrom rebuilds the paused call from an approved request's durable
// continuation. The action field may carry a ":detail" suffix (see
// toolAction); only the tool name resumes.
func resumeFrom(r *permissions.Request, grant permissions.GrantType) ResumeOutcome {
	args := map[string]interface{}{}
	for k, v := range r.Continuation {
		args[k] = v
	}
	tool := r.Action
	if idx := strings.Index(tool, ":"); idx > 0 {
		tool = tool[:idx]
	}
	return ResumeOutcome{Resumed: true, Capability: r.Capability, Tool: tool, Args: args, Grant: grant}
}

// NewGovernance builds the substrate handle and wires broker lifecycle
// events into the canonical stream (permission.requested/approved/
// denied/expired become first-class events automatically).
func NewGovernance(events *cevents.Stream, broker *permissions.Broker, ghostID, agentID string) *Governance {
	g := &Governance{Events: events, Broker: broker, GhostID: ghostID, AgentID: agentID, seenCaps: map[string]bool{}}
	if broker != nil && events != nil {
		broker.SetEmitter(func(t string, r *permissions.Request) {
			typ := cevents.PermissionRequested
			switch t {
			case "permission.approved":
				typ = cevents.PermissionApproved
			case "permission.denied":
				typ = cevents.PermissionDenied
			case "permission.expired":
				typ = cevents.PermissionExpired
			}
			events.Publish(&cevents.Event{
				Type: typ, RequestID: r.RequestID, GhostID: ghostID, AgentID: agentID,
				Payload: map[string]interface{}{
					"capability": r.Capability, "action": r.Action,
					"target": r.Target, "summary": r.Reason,
				},
			})
		})
	}
	return g
}

// NoteCapability records first sighting of a capability per request
// (drives capability.started exactly once per turn).
func (g *Governance) NoteCapability(requestID, capabilityID, trajectoryID string) bool {
	if !g.active() {
		return false
	}
	g.capMu.Lock()
	key := requestID + "\x00" + capabilityID
	if g.seenCaps[key] {
		g.capMu.Unlock()
		return false
	}
	g.seenCaps[key] = true
	g.capMu.Unlock()
	g.CapabilityCommitted(requestID, "", capabilityID, trajectoryID)
	return true
}

// SessionScopes returns the memory scopes for a session (nil-safe:
// no contexts store means global visibility, backward compatible).
func (g *Governance) SessionScopes(sessionKey string) []string {
	if !g.active() || g.Contexts == nil {
		return nil
	}
	return g.Contexts.ScopesForSession(sessionKey)
}

// SessionWriteScopes returns scope tags for new memories from a session.
func (g *Governance) SessionWriteScopes(sessionKey string) []string {
	if !g.active() || g.Contexts == nil {
		return nil
	}
	return g.Contexts.WriteScopes(sessionKey)
}

// contextAllows checks the session context's capability allowlist.
// Unconfigured contexts (or no store) allow all — V1 default.
func (g *Governance) contextAllows(sessionKey, capabilityID string) bool {
	if !g.active() || g.Contexts == nil {
		return true
	}
	ctx, ok := g.Contexts.Get(g.Contexts.SessionContext(sessionKey))
	if !ok {
		return true
	}
	return contexts.CanUseCapability(ctx, capabilityID)
}

// execute (empty = all allowed, matching contexts.CanUseCapability).
type routineScope struct {
	routineID string
	allowed   []string
}

// SetRoutineContext scopes a session's turns to a routine's allowed
// capabilities. Set by the scheduler executor before an unattended run,
// cleared after. Interactive turns never carry routine context.
func (g *Governance) SetRoutineContext(sessionKey, routineID string, allowed []string) {
	if !g.active() {
		return
	}
	g.capMu.Lock()
	defer g.capMu.Unlock()
	if g.routineCtx == nil {
		g.routineCtx = map[string]routineScope{}
	}
	g.routineCtx[sessionKey] = routineScope{routineID: routineID, allowed: allowed}
}

// ClearRoutineContext removes the unattended scope.
func (g *Governance) ClearRoutineContext(sessionKey string) {
	if !g.active() {
		return
	}
	g.capMu.Lock()
	defer g.capMu.Unlock()
	delete(g.routineCtx, sessionKey)
}

// routineAllows reports whether a routine-scoped turn may use a
// capability. No scope (interactive) always allows; an empty allowlist
// allows all (the broker still gates consequential actions).
func (g *Governance) routineAllows(sessionKey, capabilityID string) bool {
	if !g.active() {
		return true
	}
	g.capMu.Lock()
	defer g.capMu.Unlock()
	scope, ok := g.routineCtx[sessionKey]
	if !ok {
		return true
	}
	if len(scope.allowed) == 0 {
		return true
	}
	for _, a := range scope.allowed {
		if a == capabilityID {
			return true
		}
	}
	return false
}
