package tools

import (
	"testing"
	"time"
)

// Hidden is visibility; policy is authority. The split is what lets an
// owner-requested attempt promote a hidden primitive (the broker still asks
// before anything runs) while an explicit channel/session denial keeps its
// hard refusal.
func TestHiddenAndPolicyAreDistinct(t *testing.T) {
	reg := NewToolRegistry()
	reg.RegisterHidden(testTool{name: "exec"}, time.Hour)

	if !reg.IsHidden("exec") {
		t.Fatal("freshly hidden exec must report hidden")
	}
	if reg.AllowedFor("exec", "cli", "s1") {
		t.Fatal("hidden exec must not be allowed")
	}
	if !reg.PolicyAllows("exec", "cli", "s1") {
		t.Fatal("hidden state must not masquerade as a policy denial")
	}

	reg.SetToolEnabledForChannel("cli", "exec", false)
	if reg.PolicyAllows("exec", "cli", "s1") {
		t.Fatal("an explicit channel denial must be reported by PolicyAllows")
	}
	reg.SetToolEnabledForChannel("cli", "exec", true)

	reg.Promote("exec")
	if reg.IsHidden("exec") {
		t.Fatal("promoted exec must no longer be hidden")
	}
	if !reg.AllowedFor("exec", "cli", "s1") {
		t.Fatal("promoted exec must be allowed")
	}
}
