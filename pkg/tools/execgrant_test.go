package tools

import (
	"context"
	"strings"
	"testing"
)

func TestGrantRequiredTable(t *testing.T) {
	for _, name := range []string{"exec", "sandbox", "update", "i2c", "spi", "hass", "computer_screenshot", "browser_navigate", "mcp_github_search"} {
		if !GrantRequired(name) {
			t.Errorf("%q must require an execution grant", name)
		}
	}
	for _, name := range []string{"read_file", "write_file", "memory", "web_fetch", "message", "exec_status", "computer", "browser", "mcp"} {
		if GrantRequired(name) {
			t.Errorf("%q must not require an execution grant", name)
		}
	}
}

// Primitives are denied by default — even when enabled for the channel.
func TestRegistryDeniesPrimitiveWithoutGrant(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&stubGrantTool{name: "exec"})
	res := r.ExecuteWithContext(context.Background(), "exec", map[string]interface{}{}, "cli", "direct", "s1", nil)
	if !res.IsError {
		t.Fatal("exec without a grant must be denied")
	}
	if !strings.Contains(res.ForLLM, "execution policy") {
		t.Fatalf("denial must name the policy, got %q", res.ForLLM)
	}
}

// A turn-scoped grant admits exactly the approved tool.
func TestRegistryAdmitsGrantedPrimitive(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&stubGrantTool{name: "exec"})
	ctx := GrantExec(context.Background(), "exec")
	res := r.ExecuteWithContext(ctx, "exec", map[string]interface{}{}, "cli", "direct", "s1", nil)
	if res.IsError {
		t.Fatalf("granted exec must run, got %q", res.ForLLM)
	}
	// Sibling primitive stays denied on the same context.
	r.Register(&stubGrantTool{name: "sandbox"})
	res = r.ExecuteWithContext(ctx, "sandbox", map[string]interface{}{}, "cli", "direct", "s1", nil)
	if !res.IsError {
		t.Fatal("ungranted sibling primitive must stay denied")
	}
}

// MCP servers are third-party code: the prefix rule covers them all.
func TestRegistryDeniesMCPWithoutGrant(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&stubGrantTool{name: "mcp_github_search"})
	if res := r.ExecuteWithContext(context.Background(), "mcp_github_search", nil, "", "", "", nil); !res.IsError {
		t.Fatal("mcp_* without a grant must be denied")
	}
	if res := r.ExecuteWithContext(WithSystemGrant(context.Background()), "mcp_github_search", nil, "", "", "", nil); res.IsError {
		t.Fatalf("system grant must admit, got %q", res.ForLLM)
	}
}

// Non-primitives are unaffected by the grant gate.
func TestRegistryIgnoresGrantsForDataTools(t *testing.T) {
	r := NewToolRegistry()
	r.Register(&stubGrantTool{name: "read_file"})
	if res := r.ExecuteWithContext(context.Background(), "read_file", nil, "", "", "", nil); res.IsError {
		t.Fatalf("data tools need no grant, got %q", res.ForLLM)
	}
}

type stubGrantTool struct{ name string }

func (s *stubGrantTool) Name() string        { return s.name }
func (s *stubGrantTool) Description() string { return "stub" }
func (s *stubGrantTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s *stubGrantTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return &ToolResult{ForLLM: "ok"}
}

func TestRestrictToKeepsClosedPath(t *testing.T) {
	r := NewToolRegistry()
	for _, n := range []string{"read_file", "exec", "web_search", "memory"} {
		r.Register(&stubGrantTool{name: n})
	}
	r.RestrictTo([]string{"exec"})
	for _, n := range []string{"read_file", "exec"} {
		if _, ok := r.Get(n); !ok {
			t.Errorf("RestrictTo must keep %q", n)
		}
	}
	for _, n := range []string{"web_search", "memory"} {
		if _, ok := r.Get(n); ok {
			t.Errorf("RestrictTo must drop %q", n)
		}
	}
	if defs := r.ToProviderDefs(); len(defs) != 2 {
		t.Fatalf("provider defs must shrink to the closed path, got %d", len(defs))
	}
}
