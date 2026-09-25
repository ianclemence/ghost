package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

// A surface that withholds a tool must say so before authority is requested.
// The live failure this guards: the owner approved `exec` from the phone and
// execution then refused it ("disabled for this channel/session") — an
// approval that could never take effect.
func TestSurfaceToolBlockedNeverAsksForWithheldTools(t *testing.T) {
	reg := tools.NewToolRegistry()
	reg.SetToolEnabledForChannel("mobile", "exec", false)

	if msg, blocked := surfaceToolBlocked(tools.ProfileMobileSafe, reg, "exec", "mobile", "s1"); !blocked {
		t.Fatal("mobile + mobile-safe profile must be blocked")
	} else if !strings.Contains(msg, "web_search") {
		t.Fatalf("refusal must point at what to use instead, got %q", msg)
	}

	if _, blocked := surfaceToolBlocked(tools.ProfileFull, reg, "exec", "terminal", "s1"); blocked {
		t.Fatal("full surface without a policy must not be blocked")
	}

	if _, blocked := surfaceToolBlocked(tools.ProfileFull, reg, "exec", "mobile", "s1"); !blocked {
		t.Fatal("a channel policy must block even under the full profile")
	}
}
