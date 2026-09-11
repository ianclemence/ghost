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
