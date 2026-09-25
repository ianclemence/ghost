package tools

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/browser"
)

// The persistent profile reaches the browser launch: without it, cookies and
// logins die with the process.
func TestBrowserEnvironmentCarriesPersistentProfile(t *testing.T) {
	t.Setenv(browser.ProfileEnv, "")
	env := browserEnvironment("/var/lib/ghost/workspace/state/browser-profiles/personal/default")
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, browser.ProfileEnv+"=/var/lib/ghost/workspace/state/browser-profiles/personal/default") {
			found = true
		}
	}
	if !found {
		t.Fatal("browser environment must point AGENT_BROWSER_PROFILE at the session profile")
	}
}

func TestBrowserEnvironmentRespectsOperatorProfile(t *testing.T) {
	t.Setenv(browser.ProfileEnv, "/operator/chosen/profile")
	env := browserEnvironment("/session/profile")
	for _, e := range env {
		if strings.HasPrefix(e, browser.ProfileEnv+"=") && e != browser.ProfileEnv+"=/operator/chosen/profile" {
			t.Fatalf("operator profile must win, got %q", e)
		}
	}
}
