package agent

import (
	"testing"
)

// skill_manage modifies Ghost's durable procedural behavior. It must be
// broker-governed: the model cannot create/patch/delete/enable/disable
// skills without an explicit decision.
func TestSkillManageGovernedByBroker(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop

	// delete/create/patch are high impact: never auto-allowed.
	for _, action := range []string{"delete", "create", "patch"} {
		res, handled := al.authorizeStandaloneTool("req-"+action, "sess-sm", "skill_manage",
			map[string]interface{}{"action": action, "name": "x"})
		if !handled {
			t.Fatalf("%s must be handled by the broker", action)
		}
		if res.Allowed {
			t.Fatalf("%s must not auto-allow without a grant", action)
		}
		if res.PendingID == "" {
			t.Fatalf("%s must leave a durable approval request", action)
		}
	}

	// enable/disable are consequential: also require a decision.
	res, handled := al.authorizeStandaloneTool("req-en", "sess-sm", "skill_manage",
		map[string]interface{}{"action": "enable", "name": "x"})
	if !handled || res.Allowed || res.PendingID == "" {
		t.Fatalf("enable must require durable approval: %+v", res)
	}

	// A standing grant for the exact governed action authorizes it. The
	// broker action is "<tool>:<action>" (see toolAction), matching what an
	// allow-always approval would store.
	if err := h.broker.GrantStanding("skills.manage", "skill_manage:disable", "session:sess-sm", false); err != nil {
		t.Fatal(err)
	}
	res2, _ := al.authorizeStandaloneTool("req-en2", "sess-sm", "skill_manage",
		map[string]interface{}{"action": "disable", "name": "x"})
	if !res2.Allowed {
		t.Fatal("standing grant must authorize skill management")
	}

	// Unrelated tools are untouched (not routed here).
	if _, handled := al.authorizeStandaloneTool("req-ro", "sess-sm", "read_file",
		map[string]interface{}{"path": "x"}); handled {
		t.Fatal("read_file must not be routed through standalone governance")
	}
}
