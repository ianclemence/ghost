package agent

import (
	"sync"
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// A deny-only guard must be able to refuse a call that broker policy allows.
// This is the explicit monotonic invariant: a downstream component may
// reduce authority but may never overturn a stronger denial into an allow.
func TestGuardDeniesBrokerAllowedCall(t *testing.T) {
	g := openTestGovernance(t)
	g.AddGuard(func(requestID, sessionKey, capabilityID, tool string, args map[string]interface{}) string {
		if tool == "danger_tool" {
			return "that tool is not permitted here"
		}
		return ""
	})

	// Read-only risk is allowed by the broker.
	res := g.AuthorizeStandalone("req-1", "sess-1", "weather.current", "danger_tool",
		map[string]interface{}{}, permissions.RiskReadOnly)
	if res.Allowed {
		t.Fatal("a deny-only guard must override a broker allow")
	}
	if res.AskMessage == "" {
		t.Fatal("denial must carry a reason")
	}

	// A different tool passes unchanged.
	ok := g.AuthorizeStandalone("req-1", "sess-1", "weather.current", "weather_now",
		map[string]interface{}{}, permissions.RiskReadOnly)
	if !ok.Allowed {
		t.Fatal("unrelated tool must still be allowed")
	}
}

// Guards compose monotonically: adding more guards can only shrink the
// allowed set, never widen it, regardless of order.
func TestGuardCompositionMonotonic(t *testing.T) {
	g := openTestGovernance(t)
	g.AddGuard(func(_, _, _, tool string, _ map[string]interface{}) string {
		if tool == "a" || tool == "b" {
			return "guard-1"
		}
		return ""
	})
	g.AddGuard(func(_, _, _, tool string, _ map[string]interface{}) string {
		if tool == "b" || tool == "c" {
			return "guard-2"
		}
		return ""
	})
	for _, tc := range []struct {
		tool string
		want bool
	}{{"a", false}, {"b", false}, {"c", false}, {"d", true}} {
		res := g.AuthorizeStandalone("r", "s", "weather.current", tc.tool,
			map[string]interface{}{}, permissions.RiskReadOnly)
		if res.Allowed != tc.want {
			t.Fatalf("tool %s allowed=%v want %v", tc.tool, res.Allowed, tc.want)
		}
	}
}

// Guards must not create approval requests: a denied ask stays denied.
func TestGuardDeniesAskWithoutRequest(t *testing.T) {
	g := openTestGovernance(t)
	g.AddGuard(func(_, _, _, _ string, _ map[string]interface{}) string {
		return "always denied"
	})
	// Consequential risk would otherwise ask.
	res := g.AuthorizeStandalone("req-x", "sess-x", "calendar", "calendar_create",
		map[string]interface{}{}, permissions.RiskConsequential)
	if res.Allowed || res.PendingID != "" {
		t.Fatalf("guard denial must not create a request: %+v", res)
	}
}

// Concurrent guard registration and evaluation must be race-free.
func TestGuardConcurrent(t *testing.T) {
	g := openTestGovernance(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g.AddGuard(func(_, _, _, _ string, _ map[string]interface{}) string { return "" })
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.AuthorizeStandalone("r", "s", "weather.current", "t",
				map[string]interface{}{}, permissions.RiskReadOnly)
		}()
	}
	wg.Wait()
}
