// Package archtest holds the Capability Architecture Contract Suite: fast,
// deterministic tests that protect Ghost's architectural invariants from
// regression. It is deliberately separate from the behavioral Golden suite.
//
// The single most important invariants:
//
//	availability  != authority
//	implementation != permission
//	authentication != authorization
//	tool name      != capability identity
//	model assertion != runtime evidence
package archtest

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/permissions"
	"github.com/ianclemence/ghost/pkg/tools"
	_ "modernc.org/sqlite"
)

func yes() func() bool { return func() bool { return true } }

func testResolver(a capability.Availability) *capability.Resolver {
	r := capability.NewResolver()
	capability.RegisterDefaults(r, a)
	return r
}

// --- Resolution ---

func TestCapabilityArchitectureContracts_Resolution(t *testing.T) {
	r := testResolver(capability.Availability{OpenWeather: yes(), HomeAssistant: yes()})

	// capability resolves to an available implementation
	if impl, ok := r.Resolve("weather.get", false); !ok || impl.Provider != "openweather" {
		t.Fatalf("expected openweather, got %+v ok=%v", impl, ok)
	}
	// unavailable implementation is not selected
	r2 := testResolver(capability.Availability{})
	if impl, ok := r2.Resolve("weather.get", false); !ok || impl.Provider != "open-meteo" {
		t.Fatalf("without key, keyless must serve: %+v ok=%v", impl, ok)
	}
	if _, ok := r2.Resolve("device.control", false); ok {
		t.Fatal("device.control must be unavailable without Home Assistant")
	}
	// provider replacement does not change capability identity or tool
	base, _ := r2.Resolve("weather.get", false)
	keyed, _ := r.Resolve("weather.get", false)
	if base.Capability != keyed.Capability || base.Tool != keyed.Tool {
		t.Fatalf("provider replacement changed identity: %+v vs %+v", base, keyed)
	}
	// provider selection is deterministic
	for i := 0; i < 5; i++ {
		if impl, _ := r.Resolve("weather.get", false); impl.Provider != "openweather" {
			t.Fatal("provider selection must be deterministic")
		}
	}
	// resolver can report unavailable without granting authority
	if _, ok := r2.Resolve("device.control", false); ok {
		t.Fatal("resolver must be able to report unavailable")
	}
}

// --- Authority: resolver/connected app/skill/mcp never grant authority ---

func TestCapabilityArchitectureContracts_Authority(t *testing.T) {
	// A resolved implementation is not permission: the broker still decides.
	broker := openBroker(t)
	if broker.Evaluate("device.control", "device:turn_on", "home", permissions.RiskConsequential) != permissions.VerdictAsk {
		t.Fatal("availability must not imply authority")
	}
	// A connected app does not grant permission.
	apps := connectedapp.NewRegistry()
	apps.Register(connectedapp.App{ID: "home-assistant", Capabilities: []string{"device.control"}, Status: connectedapp.StatusConnected})
	if !apps.Usable("home-assistant") {
		t.Fatal("connected app should be available")
	}
	if broker.Evaluate("device.control", "device:turn_on", "home", permissions.RiskConsequential) != permissions.VerdictAsk {
		t.Fatal("connected app availability must not grant authority")
	}
	// Revoking the app removes availability but the broker still decides.
	apps.Revoke("home-assistant")
	if apps.Usable("home-assistant") {
		t.Fatal("revoked app must be unavailable")
	}
	// A standing grant is the ONLY thing that turns ask into allow.
	if err := broker.GrantStanding("device.control", "device:turn_on", "home", false); err != nil {
		t.Fatal(err)
	}
	if broker.Evaluate("device.control", "device:turn_on", "home", permissions.RiskConsequential) != permissions.VerdictAllow {
		t.Fatal("an explicit broker grant must authorize")
	}
	if err := broker.Revoke("device.control", "device:turn_on", "home"); err != nil {
		t.Fatal(err)
	}
	if broker.Evaluate("device.control", "device:turn_on", "home", permissions.RiskConsequential) == permissions.VerdictAllow {
		t.Fatal("revocation must immediately withdraw authority")
	}
}

// --- Evidence: only valid runtime evidence yields success ---

func TestCapabilityArchitectureContracts_Evidence(t *testing.T) {
	cases := []struct {
		name string
		kind capability.EvidenceKind
		ev   map[string]interface{}
		ok   bool
	}{
		{"missing", capability.EvidenceAcknowledgement, nil, false},
		{"wrong type", capability.EvidenceAcknowledgement, map[string]interface{}{"type": "file_write", "path": "x", "timestamp": "t"}, false},
		{"missing required field", capability.EvidenceFileWrite, map[string]interface{}{"type": "file_write", "timestamp": "t"}, false},
		{"malformed action", capability.EvidenceAction, map[string]interface{}{"type": "action", "outcome": "ok"}, false},
		{"valid file", capability.EvidenceFileWrite, map[string]interface{}{"type": "file_write", "path": "x", "timestamp": "t"}, true},
		{"valid acknowledgement", capability.EvidenceAcknowledgement, map[string]interface{}{"type": "acknowledgement", "recipient": "sarah", "timestamp": "t"}, true},
		{"valid state transition", capability.EvidenceStateTransition, map[string]interface{}{"type": "state_transition", "entity": "light", "requested": "on", "timestamp": "t"}, true},
		{"valid action", capability.EvidenceAction, map[string]interface{}{"type": "action", "op": "browser.click", "outcome": "ok", "timestamp": "t"}, true},
	}
	for _, c := range cases {
		err := capability.ValidateEvidence(c.kind, c.ev)
		if (err == nil) != c.ok {
			t.Errorf("%s: ValidateEvidence err=%v want ok=%v", c.name, err, c.ok)
		}
	}
}

// --- Delegation scope ---

func TestCapabilityArchitectureContracts_Delegation(t *testing.T) {
	scope := []string{"calendar.read", "web.search"}
	if !capability.InScope(scope, "calendar", map[string]interface{}{"action": "list"}) {
		t.Fatal("delegated calendar.read must be in scope")
	}
	if capability.InScope(scope, "calendar", map[string]interface{}{"action": "create"}) {
		t.Fatal("calendar.modify must not be delegated when only read was")
	}
	if capability.InScope(scope, "exec", nil) || capability.InScope(scope, "message", nil) {
		t.Fatal("generic/actuator escape hatches must be out of scope")
	}
}

// --- MCP ---

func TestCapabilityArchitectureContracts_MCP(t *testing.T) {
	// known tool -> semantic capability
	if spec, ok := capability.ForTool("mcp_github_search"); !ok || spec.ID != "repository.search" {
		t.Fatalf("known MCP tool must map semantically: %+v ok=%v", spec, ok)
	}
	// unknown/write tool -> high-impact mcp.execute
	spec, ok := capability.ForTool("mcp_github_delete_repo")
	if !ok || spec.ID != "mcp.execute" || spec.Risk != capability.RiskHighImpact {
		t.Fatalf("unknown MCP tool must stay high-impact mcp.execute: %+v ok=%v", spec, ok)
	}
	// free-tool governance: unknown MCP is high impact
	ft, ok := tools.FreeToolCapability("mcp_unknown_tool")
	if !ok || ft.Risk != tools.RiskHighImpact {
		t.Fatalf("unknown MCP must be high impact: %+v ok=%v", ft, ok)
	}
}

// --- Broker isolation (no self-authorization) ---

func TestCapabilityArchitectureContracts_BrokerIsAuthority(t *testing.T) {
	broker := openBroker(t)
	// A consequential capability with no grant is ask/deny, never allow.
	for _, cap := range []string{"message.send", "calendar.modify", "exec.shell", "mcp.execute"} {
		if broker.Evaluate(cap, "x", "owner", permissions.RiskHighImpact) == permissions.VerdictAllow {
			t.Fatalf("%s must not be allowed without a grant", cap)
		}
	}
	// Read-only is allowed without a grant.
	if broker.Evaluate("web.fetch", "web_fetch", "owner", permissions.RiskReadOnly) != permissions.VerdictAllow {
		t.Fatal("read-only must be allowed without a grant")
	}
}

// --- Registry evidence enforcement end-to-end ---

func TestCapabilityArchitectureContracts_RegistryEvidence(t *testing.T) {
	reg := tools.NewToolRegistry()
	reg.Register(&stub{name: "message"}) // message.send requires acknowledgement
	res := reg.ExecuteWithContext(context.Background(), "message", nil, "cli", "direct", "s", nil)
	if !res.IsError {
		t.Fatal("message.send success without evidence must be refused")
	}
	reg2 := tools.NewToolRegistry()
	reg2.Register(&stub{name: "message", ev: map[string]interface{}{"type": "acknowledgement", "recipient": "sarah", "timestamp": "t"}})
	res = reg2.ExecuteWithContext(context.Background(), "message", nil, "cli", "direct", "s", nil)
	if res.IsError {
		t.Fatalf("valid evidence must be accepted: %q", res.ForLLM)
	}
}

type stub struct {
	name string
	ev   map[string]interface{}
}

func (s *stub) Name() string                            { return s.name }
func (s *stub) Description() string                     { return "stub" }
func (s *stub) Parameters() map[string]interface{}      { return map[string]interface{}{"type": "object"} }
func (s *stub) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return &tools.ToolResult{ForLLM: "ok", Evidence: s.ev}
}

func openBroker(t *testing.T) *permissions.Broker {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	b, err := permissions.Open(db, permissions.ModeAsk, 0)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
