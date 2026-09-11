package capability

import "testing"

func TestForToolResolvesCanonicalIdentity(t *testing.T) {
	cases := map[string]string{
		"message":         "message.send",
		"write_file":      "file.write",
		"hass":            "device.control",
		"browser_click":   "browser.control",
		"computer_type":   "computer.control",
		"web_fetch":       "web.fetch",
		"mcp_github_read": "mcp.execute",
	}
	for tool, want := range cases {
		spec, ok := ForTool(tool)
		if !ok || spec.ID != want {
			t.Errorf("ForTool(%q) = %q ok=%v, want %q", tool, spec.ID, ok, want)
		}
	}
	if _, ok := ForTool("definitely_not_a_tool"); ok {
		t.Error("unknown tool must not resolve")
	}
}

func TestConsequentialCapabilitiesRequireEvidence(t *testing.T) {
	required := []string{"message.send", "calendar.modify", "device.control", "browser.control", "computer.control", "artifact.create", "file.write"}
	for _, id := range required {
		spec, ok := Get(id)
		if !ok {
			t.Errorf("capability %q missing", id)
			continue
		}
		if !spec.RequiresEvidence() {
			t.Errorf("%q must require evidence", id)
		}
	}
}

func TestReadOnlyCapabilitiesNeedNoEvidence(t *testing.T) {
	for _, id := range []string{"web.fetch", "weather.get", "file.read", "memory.recall", "browser.inspect"} {
		spec, ok := Get(id)
		if !ok {
			t.Fatalf("capability %q missing", id)
		}
		if spec.RequiresEvidence() {
			t.Errorf("%q must not require evidence", id)
		}
	}
}

func TestCanonicalIDsPresent(t *testing.T) {
	ids := map[string]bool{}
	for _, id := range IDs() {
		ids[id] = true
	}
	for _, want := range []string{
		"memory.remember", "memory.recall", "calendar.read", "calendar.modify",
		"message.send", "device.read", "device.control", "browser.inspect",
		"browser.control", "computer.control", "artifact.create", "exec.shell",
		"mcp.execute", "skills.manage",
	} {
		if !ids[want] {
			t.Errorf("canonical capability %q missing from registry", want)
		}
	}
}

// The calendar surface resolves to read vs modify by action, so authority
// and evidence are operation-specific.
func TestForToolActionCalendar(t *testing.T) {
	read, ok := ForToolAction("calendar", map[string]interface{}{"action": "list"})
	if !ok || read.ID != "calendar.read" || read.RequiresEvidence() {
		t.Fatalf("list must resolve to calendar.read with no evidence: %+v ok=%v", read, ok)
	}
	mod, ok := ForToolAction("calendar", map[string]interface{}{"action": "create"})
	if !ok || mod.ID != "calendar.modify" || !mod.RequiresEvidence() {
		t.Fatalf("create must resolve to calendar.modify with evidence: %+v ok=%v", mod, ok)
	}
	del, ok := ForToolAction("calendar", map[string]interface{}{"action": "delete"})
	if !ok || del.ID != "calendar.modify" {
		t.Fatalf("delete must resolve to calendar.modify: %+v ok=%v", del, ok)
	}
}

// MCP semantic mapping is conservative.
func TestMCPMapping(t *testing.T) {
	if spec, ok := ForTool("mcp_github_search"); !ok || spec.ID != "repository.search" {
		t.Fatalf("known MCP tool must map semantically: %+v ok=%v", spec, ok)
	}
	if spec, ok := ForTool("mcp_calendar_delete"); !ok || spec.ID != "mcp.execute" {
		t.Fatalf("write-shaped/unknown MCP tool must stay mcp.execute: %+v ok=%v", spec, ok)
	}
}

// Delegated capability scope is deny-by-default and never broadens authority.
func TestInScopeDenyByDefault(t *testing.T) {
	scope := []string{"calendar.read", "web.search"}
	if !InScope(scope, "calendar", map[string]interface{}{"action": "list"}) {
		t.Fatal("calendar.read must be in scope")
	}
	if InScope(scope, "calendar", map[string]interface{}{"action": "create"}) {
		t.Fatal("calendar.modify must NOT be in scope when only read is delegated")
	}
	if InScope(scope, "message", map[string]interface{}{"action": "send"}) {
		t.Fatal("message.send must NOT be in scope")
	}
	if InScope(scope, "exec", nil) {
		t.Fatal("exec must NOT be in scope")
	}
	// Unknown tool is denied when scoped.
	if InScope(scope, "totally_unknown_tool", nil) {
		t.Fatal("unknown tool must be denied when scoped")
	}
	// Empty scope is legacy-unscoped (allowed); the blocklist and broker
	// still bound it.
	if !InScope(nil, "exec", nil) {
		t.Fatal("empty scope is legacy unscoped")
	}
}
