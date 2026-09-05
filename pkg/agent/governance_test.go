package agent

import (
	"database/sql"
	"testing"

	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/permissions"
	_ "modernc.org/sqlite"
)

func openTestGovernance(t *testing.T) *Governance {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	b, err := permissions.Open(db, permissions.ModeAsk, 0)
	if err != nil {
		t.Fatal(err)
	}
	return &Governance{Broker: b, GhostID: "g1", AgentID: "agent-main"}
}

func TestGateReadOnlyAllows(t *testing.T) {
	g := openTestGovernance(t)
	res := g.AuthorizeTool("req-1", "sess-1", "weather.current", "weather_now", map[string]interface{}{"location": "Bangkok"})
	if !res.Allowed {
		t.Fatal("read-only must pass the gate")
	}
}

func TestGateConsequentialAsksDurably(t *testing.T) {
	g := openTestGovernance(t)
	res := g.AuthorizeTool("req-1", "sess-1", "calendar.create", "exec", map[string]interface{}{"title": "Meet"})
	if res.Allowed || res.PendingID == "" {
		t.Fatalf("must pause with durable request: %+v", res)
	}
	if got := res.AskMessage; got == "" {
		t.Fatal("model needs pause text")
	}
	// Restart-equivalent: new governance over same broker DB still sees it.
	pending, ok := g.Broker.PendingForSession("sess-1")
	if !ok || pending.ID != res.PendingID {
		t.Fatal("pending must be durable")
	}
}

func TestApprovalReplyResumes(t *testing.T) {
	g := openTestGovernance(t)
	g.AuthorizeTool("req-1", "sess-1", "calendar.create", "exec", map[string]interface{}{"title": "Meet"})
	out := g.CheckApprovalReply("sess-1", "allow once")
	if !out.Resumed || out.Capability != "calendar.create" || out.Tool != "exec" {
		t.Fatalf("must resume: %+v", out)
	}
	if out.Args["title"] != "Meet" {
		t.Fatal("continuation must preserve intent")
	}
	// Second identical reply cannot re-execute (consumed).
	out2 := g.CheckApprovalReply("sess-1", "allow once")
	if out2.Resumed {
		t.Fatal("approval is single-use")
	}
}

func TestApprovalReplyDeny(t *testing.T) {
	g := openTestGovernance(t)
	g.AuthorizeTool("req-1", "sess-1", "calendar.create", "exec", map[string]interface{}{})
	out := g.CheckApprovalReply("sess-1", "deny")
	if !out.Denied || out.Resumed {
		t.Fatalf("deny must cancel cleanly: %+v", out)
	}
}

func TestOrdinaryChatNeverHijacked(t *testing.T) {
	g := openTestGovernance(t)
	// No pending request: "allow once" is ordinary chat.
	if out := g.CheckApprovalReply("sess-1", "allow once"); out.Resumed || out.Denied {
		t.Fatal("must not hijack chat without pending request")
	}
	// Pending exists but message is ordinary chat.
	g.AuthorizeTool("req-1", "sess-1", "calendar.create", "exec", map[string]interface{}{})
	if out := g.CheckApprovalReply("sess-1", "what time is it"); out.Resumed || out.Denied {
		t.Fatal("ordinary chat must not resolve approvals")
	}
}

func TestNilGovernanceSafe(t *testing.T) {
	var g *Governance
	if !g.AuthorizeTool("r", "s", "c", "t", nil).Allowed {
		t.Fatal("nil governance must not block")
	}
	if out := g.CheckApprovalReply("s", "allow once"); out.Resumed {
		t.Fatal("nil governance must not resume")
	}
	g2 := &Governance{}
	if !g2.AuthorizeTool("r", "s", "c", "t", nil).Allowed {
		t.Fatal("brokerless governance must not block")
	}
}

func TestContextCapabilityGate(t *testing.T) {
	g := openTestGovernance(t)
	cs, err := contexts.Open(t.TempDir(), "ghost-1")
	if err != nil {
		t.Fatal(err)
	}
	g.Contexts = cs
	if _, err := cs.Create("work", "Work"); err != nil {
		t.Fatal(err)
	}
	cs.SetSessionContext("sess-w", "work")
	// Simulate an allowlisted work context.
	cs.SetCapabilities("work", []string{"calendar.read"})
	if res := g.AuthorizeTool("r1", "sess-w", "weather.current", "weather_now", nil); res.Allowed {
		t.Fatal("work context without weather must deny")
	}
	if res := g.AuthorizeTool("r1", "sess-w", "calendar.read", "exec", nil); res.Allowed {
		t.Fatal("consequential calendar must still ask (broker), not allow")
	}
	// Personal session unaffected.
	if res := g.AuthorizeTool("r2", "sess-p", "weather.current", "weather_now", nil); !res.Allowed {
		t.Fatal("personal read-only must allow")
	}
}
