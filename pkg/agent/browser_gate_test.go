package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/schema"
	"github.com/ianclemence/ghost/pkg/tasks"
	"github.com/ianclemence/ghost/pkg/tools"
)

// stubBrowserTool stands in for the real BrowserTool (whose CLI runner
// cannot execute in tests). It honors the gate binding exactly like the
// enforced tool path: no bag means legacy execution, a bag means bound
// execution with evidence — so gate tests prove what the gate attached,
// and tools-package tests prove what the tool enforces.
type stubBrowserTool struct {
	name       string
	calls      int
	bags       []tools.BrowserCall
	legacy     int
	noEvidence bool
	failWith   bool
	output     string
}

func (s *stubBrowserTool) Name() string        { return s.name }
func (s *stubBrowserTool) Description() string { return "test browser op" }
func (s *stubBrowserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s *stubBrowserTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return s.run(ctx, args)
}

func fakeToolCall(name string, args map[string]interface{}) providers.ToolCall {
	if args == nil {
		args = map[string]interface{}{}
	}
	return providers.ToolCall{ID: "call-1", Name: name, Arguments: args}
}

// gateHarness wires the REAL stores the gate decides from: migrated
// SQLite, permission broker, contexts, canonical events, task store.
type gateHarness struct {
	loop     *AgentLoop
	broker   *permissions.Broker
	events   *cevents.Stream
	contexts *contexts.Store
	jobs     *tasks.Store
	stubs    map[string]*stubBrowserTool
	ghostID  string
	workCtx  string
	raw      *sql.DB
}

// existingContext returns the first live context of a kind, or nil.
func existingContext(cs *contexts.Store, kind contexts.Kind) *contexts.Context {
	for _, c := range cs.List() {
		if c.Kind == kind {
			return c
		}
	}
	return nil
}

func newGateHarness(t *testing.T) *gateHarness {
	t.Helper()
	return newGateHarnessOnWS(t, t.TempDir())
}

// newGateHarnessOnWS wires the real stores over an EXISTING workspace's
// database, so a test can simulate a restart by building a second harness
// on the same workspace. The workspace's ghost.db must already exist and
// be at the schema head (newGateHarness migrates it; restart harnesses
// reuse the file).
func newGateHarnessOnWS(t *testing.T, ws string) *gateHarness {
	t.Helper()
	database, err := db.NewDB(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	raw, err := sql.Open("sqlite", "file:"+ws+"/ghost.db?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { raw.Close() })
	if _, err := schema.MigrateToCurrent(raw); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	broker, err := permissions.Open(raw, permissions.ModeAsk, 0)
	if err != nil {
		t.Fatal(err)
	}
	events, err := cevents.Open(raw, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cs, err := contexts.Open(ws, "ghost-test-1")
	if err != nil {
		t.Fatal(err)
	}
	work := existingContext(cs, contexts.KindWork)
	if work == nil {
		var err error
		work, err = cs.Create(contexts.KindWork, "Work")
		if err != nil {
			t.Fatal(err)
		}
	}
	jobs := tasks.NewStore(raw, nil)
	gov := NewGovernance(events, broker, "ghost-test-1", "agent-test")
	gov.Contexts = cs
	reg := tools.NewToolRegistry()
	stubs := map[string]*stubBrowserTool{}
	for _, name := range []string{"browser_navigate", "browser_snapshot", "browser_click", "browser_type", "browser_press"} {
		st := &stubBrowserTool{name: name, output: "page output for " + name}
		stubs[name] = st
		reg.Register(st)
	}
	al := &AgentLoop{db: database, jobs: jobs, governance: gov, tools: reg, workspace: ws}
	return &gateHarness{loop: al, broker: broker, events: events, contexts: cs, jobs: jobs, stubs: stubs, ghostID: "ghost-test-1", workCtx: work.ID, raw: raw}
}

func (s *stubBrowserTool) run(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	s.calls++
	if bag, ok := tools.BrowserCallFromContext(ctx); ok {
		s.bags = append(s.bags, bag)
		if s.failWith {
			res := &tools.ToolResult{ForLLM: "boom", IsError: true}
			if !s.noEvidence {
				res.Evidence = stubEvidence(bag, "error")
			}
			return res
		}
		res := &tools.ToolResult{ForLLM: s.output, ForUser: s.output}
		if !s.noEvidence {
			res.Evidence = stubEvidence(bag, "ok")
		}
		return res
	}
	s.legacy++
	return &tools.ToolResult{ForLLM: s.output, ForUser: s.output}
}

func stubEvidence(bag tools.BrowserCall, outcome string) map[string]interface{} {
	return map[string]interface{}{
		"op": "browser." + bag.Op, "session": "sess-test", "task": bag.TaskID,
		"owner": bag.Owner, "context": bag.ContextID,
		"permission": bag.Permission, "outcome": outcome,
	}
}

func (h *gateHarness) toolCtx() context.Context { return context.Background() }

func (h *gateHarness) opts(session string) processOptions {
	return processOptions{SessionKey: session, Channel: "test", ChatID: "chat-1", RequestID: "req-" + session + "-1"}
}

// A. Allowed low-risk operation executes with a server-resolved binding.
func TestGateObserveExecutes(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	tc := fakeToolCall("browser_snapshot", map[string]interface{}{})
	res, governed, stop := al.maybeRunBrowserTool(h.toolCtx(), al.tools, tc, h.opts("sess-a"), nil)
	if !governed || stop {
		t.Fatalf("observe must execute, governed=%v stop=%v", governed, stop)
	}
	if res.IsError {
		t.Fatalf("observe failed: %s", res.ForLLM)
	}
	st := h.stubs["browser_snapshot"]
	if st.calls != 1 || len(st.bags) != 1 {
		t.Fatalf("executor must run once under binding: %+v", st)
	}
	bag := st.bags[0]
	if bag.Owner != h.ghostID || bag.TaskID != "sess-a" || bag.Sessions == nil || bag.Permission != "allow" {
		t.Fatalf("binding must be server-resolved: %+v", bag)
	}
	// Untrusted labeling is the tool layer's job (proven in pkg/tools);
	// the gate's job is the binding and the evidence-carrying event.
	if res.ForLLM == "" {
		t.Fatal("governed execution must return page output")
	}
	// Canonical event carries the evidence, not a bare success claim.
	found := false
	for _, e := range h.events.ByRequest("req-sess-a-1") {
		if e.Type == cevents.ToolCompleted {
			found = true
			if e.Payload["op"] == nil || e.Payload["session"] == nil || e.Payload["permission"] == nil {
				t.Fatalf("event must carry evidence: %+v", e.Payload)
			}
		}
	}
	if !found {
		t.Fatal("governed execution must emit a canonical tool event")
	}
}

// B. Denied operation never reaches the executor.
func TestGateDeniedNeverExecutes(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	// Seed a denial: ask first, then deny, then re-request.
	first := al.authorizeBrowserCall("req-deny-1", "sess-d", "browser_click", map[string]interface{}{})
	if first.decision != "wait" {
		t.Fatalf("consequential op without grant must wait, got %s", first.decision)
	}
	pending, ok := h.broker.PendingForSession("sess-d")
	if !ok {
		t.Fatal("wait must leave a durable pending request")
	}
	if _, err := h.broker.Resolve(pending.ID, permissions.GrantDeny, "session:sess-d"); err != nil {
		t.Fatal(err)
	}
	second := al.authorizeBrowserCall("req-deny-2", "sess-d", "browser_click", map[string]interface{}{})
	if second.decision != "deny" {
		t.Fatalf("denied op must deny, got %s", second.decision)
	}
	_, governed, stop := al.maybeRunBrowserTool(h.toolCtx(), al.tools, fakeToolCall("browser_click", nil), h.opts("sess-d"), nil)
	if !governed || !stop {
		t.Fatalf("deny must end the turn, governed=%v stop=%v", governed, stop)
	}
	if h.stubs["browser_click"].calls != 0 {
		t.Fatal("denied operation reached the executor")
	}
}

// C. Operation requiring approval waits with a durable pending request.
func TestGateApprovalWaits(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	res, governed, stop := al.maybeRunBrowserTool(h.toolCtx(), al.tools,
		fakeToolCall("browser_type", map[string]interface{}{"ref": "@e1", "text": "hi"}),
		h.opts("sess-c"), nil)
	if !governed || !stop {
		t.Fatalf("ungranted act must wait, governed=%v stop=%v", governed, stop)
	}
	if !strings.Contains(res.ForLLM, "approval") {
		t.Fatalf("wait message must mention approval: %s", res.ForLLM)
	}
	if h.stubs["browser_type"].calls != 0 {
		t.Fatal("waiting operation must not execute")
	}
	pending, ok := h.broker.PendingForSession("sess-c")
	if !ok {
		t.Fatal("wait must persist a pending request for restart-safe resume")
	}
	// The continuation carries the gate binding (and no secrets).
	for _, k := range []string{contOwner, contContext, contTask, contBrowserOp, contBrowserSession, contBrowserTool} {
		if pending.Continuation[k] == "" {
			t.Fatalf("continuation must carry %q: %+v", k, pending.Continuation)
		}
	}
	if pending.Continuation[contOwner] != h.ghostID || pending.Continuation[contBrowserTool] != "browser_type" {
		t.Fatalf("binding wrong: %+v", pending.Continuation)
	}
}

// An UNWIRED loop (no governance) refuses browser calls outright rather
// than executing them through the ungated legacy path.
func TestUnwiredLoopRefusesBrowser(t *testing.T) {
	database, err := db.NewDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	reg := tools.NewToolRegistry()
	st := &stubBrowserTool{name: "browser_navigate", output: "page"}
	stubs := []*stubBrowserTool{st}
	reg.Register(st)
	al := &AgentLoop{db: database, tools: reg, workspace: t.TempDir()}
	res, governed, stop := al.maybeRunBrowserTool(context.Background(), reg,
		fakeToolCall("browser_navigate", map[string]interface{}{"url": "https://example.com"}),
		processOptions{SessionKey: "s", RequestID: "r"}, nil)
	if !governed || !stop || res == nil || !strings.Contains(res.ForLLM, "not governed") {
		t.Fatalf("unwired loop must refuse browser: governed=%v stop=%v res=%+v", governed, stop, res)
	}
	if len(stubs) != 1 || stubs[0].calls != 0 {
		t.Fatal("refused browser call must not reach the executor")
	}
}

// Unknown browser operations fail closed.
func TestGateUnknownOpDenied(t *testing.T) {
	h := newGateHarness(t)
	res := h.loop.authorizeBrowserCall("req-u-1", "sess-u", "browser_exfiltrate", map[string]interface{}{})
	if res.decision != "deny" {
		t.Fatalf("unknown op must deny, got %s", res.decision)
	}
}
