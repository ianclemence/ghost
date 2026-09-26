package agent

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/tools"
)

type promoteTestTool struct{ name string }

func (t *promoteTestTool) Name() string        { return t.name }
func (t *promoteTestTool) Description() string { return "test tool" }
func (t *promoteTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (t *promoteTestTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.NewToolResult("ok")
}

// An owner asking for a command must be able to reach the approval gate:
// a hidden primitive on an allowed surface promotes on attempt, becomes
// visible and executable, and the broker still governs the run. Profile
// refusals, explicit policy denials, and non-hidden misses stay refused.
func TestAttemptHiddenPrimitive(t *testing.T) {
	t.Run("full profile promotes", func(t *testing.T) {
		reg := tools.NewToolRegistry()
		reg.RegisterHidden(&promoteTestTool{name: "exec"}, time.Hour)
		active := tools.NewToolRegistry()

		if !attemptHiddenPrimitive(tools.ProfileFull, reg, active, "exec", "cli", "s1") {
			t.Fatal("hidden exec on an owner-allowed surface must promote on attempt")
		}
		if reg.IsHidden("exec") {
			t.Fatal("exec must no longer be hidden after promotion")
		}
		if !reg.AllowedFor("exec", "cli", "s1") {
			t.Fatal("promoted exec must be executable")
		}
		if _, ok := active.Get("exec"); !ok {
			t.Fatal("the active set must contain the promoted tool for this turn")
		}
	})

	t.Run("profile refusal stays blocked", func(t *testing.T) {
		reg := tools.NewToolRegistry()
		reg.RegisterHidden(&promoteTestTool{name: "exec"}, time.Hour)
		active := tools.NewToolRegistry()

		if attemptHiddenPrimitive(tools.ProfileMinimal, reg, active, "exec", "cli", "s1") {
			t.Fatal("a profile that withholds exec must never promote it")
		}
		if !reg.IsHidden("exec") {
			t.Fatal("a refused attempt must leave exec hidden")
		}
		if _, ok := active.Get("exec"); ok {
			t.Fatal("a refused attempt must not touch the active set")
		}
	})

	t.Run("explicit policy denial stays blocked", func(t *testing.T) {
		reg := tools.NewToolRegistry()
		reg.RegisterHidden(&promoteTestTool{name: "exec"}, time.Hour)
		reg.SetToolEnabledForChannel("cli", "exec", false)
		active := tools.NewToolRegistry()

		if attemptHiddenPrimitive(tools.ProfileFull, reg, active, "exec", "cli", "s1") {
			t.Fatal("an explicit channel denial must win over the profile")
		}
		if !reg.IsHidden("exec") {
			t.Fatal("a denied attempt must leave exec hidden")
		}
	})

	t.Run("non-hidden tools skip the path", func(t *testing.T) {
		reg := tools.NewToolRegistry()
		reg.Register(&promoteTestTool{name: "grep_search"})
		active := tools.NewToolRegistry()

		if attemptHiddenPrimitive(tools.ProfileMinimal, reg, active, "grep_search", "cli", "s1") {
			t.Fatal("only hidden tools may go through hidden promotion")
		}
	})
}

// The owner asking for a command must be detectable deterministically: this
// is what makes exec offerable at turn start. False positives would merely
// widen visibility (the broker still gates execution); false negatives leave
// the model refusing again.
func TestOwnerRequestsCommand(t *testing.T) {
	yes := []string{
		"run df -h on this machine",
		"run the command uname -a and show me the output",
		"execute `ls -la`",
		"can you run df -h?",
		"please execute /var/tmp/report.sh",
		"df -h",
		"ls -la /var/lib/ghost",
		"now run ps aux | head",
	}
	no := []string{
		"check the free disk space",
		"what is the status of the device",
		"run a business",
		"run it",
		"run the server and tell me when it's up",
		"please run tests", // no flag/path shape; model may still attempt exec itself
		"hello",
	}
	for _, m := range yes {
		if !ownerRequestsCommand(m) {
			t.Errorf("expected command request: %q", m)
		}
	}
	for _, m := range no {
		if ownerRequestsCommand(m) {
			t.Errorf("must not treat as command request: %q", m)
		}
	}
}
