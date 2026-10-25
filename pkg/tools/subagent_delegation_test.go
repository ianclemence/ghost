package tools

import (
	"strings"
	"testing"
)

// Delegation is for independent, context-isolating or conflicting work — not
// for a sequential step the main loop could do itself and then wait on a
// lossy summary.
func TestDelegationDescriptionsTeachTheThreeTriggers(t *testing.T) {
	sm := NewSubagentManager(&noopProvider{}, "test-model", t.TempDir(), nil)
	descriptions := map[string]string{
		"subagent": NewSubagentTool(sm).Description(),
		"spawn":    NewSpawnTool(sm).Description(),
		"batch":    NewBatchDelegateTool(sm).Description(),
	}
	for name, desc := range descriptions {
		lower := strings.ToLower(desc)
		if !strings.Contains(lower, "independent") {
			t.Errorf("%s description must name independent work: %s", name, desc)
		}
		if !strings.Contains(lower, "context") {
			t.Errorf("%s description must name context isolation: %s", name, desc)
		}
		if !strings.Contains(lower, "sequential") {
			t.Errorf("%s description must warn against sequential delegation: %s", name, desc)
		}
	}
}

// A subagent starts with the least agency it needs. Adding capabilities must
// be an explicit act, never the absence of a setting.
func TestSubagentScopeDefaultsToReadOnly(t *testing.T) {
	sm := NewSubagentManager(&noopProvider{}, "test-model", t.TempDir(), nil)
	sm.mu.RLock()
	caps := append([]string(nil), sm.allowedCapabilities...)
	sm.mu.RUnlock()

	has := func(want string) bool {
		for _, c := range caps {
			if c == want {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"file.read", "web.search", "web.fetch", "memory.recall"} {
		if !has(want) {
			t.Errorf("default subagent scope is missing %q: %v", want, caps)
		}
	}
	for _, forbidden := range []string{"file.write", "exec", "message.send", "device.control"} {
		if has(forbidden) {
			t.Errorf("default subagent scope must not include %q: %v", forbidden, caps)
		}
	}

	// An explicit empty list must not mean "everything".
	sm.SetAllowedCapabilities(nil)
	sm.mu.RLock()
	after := append([]string(nil), sm.allowedCapabilities...)
	sm.mu.RUnlock()
	if len(after) == 0 {
		t.Fatal("empty capability scope must keep the read-only default, not widen to everything")
	}
}
