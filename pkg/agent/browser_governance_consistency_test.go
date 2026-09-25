package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// classRisk is the gate's risk for a tool class. upload is the one
// deliberate split: the tool classifies act (it drives a file input),
// the gate raises it to high impact because local files leave the
// device.
func classRisk(class string, name string) permissions.Risk {
	switch {
	case class == "observe":
		return permissions.RiskReadOnly
	case name == "browser_submit" || name == "browser_upload":
		return permissions.RiskHighImpact
	default:
		return permissions.RiskConsequential
	}
}

// Every browser_* tool reachable from a turn must be gated: the op
// whitelist must know it, and the gate's declared risk must match the
// tool's own classification (observe→read_only, act→consequential,
// transact/high-impact→high_impact). A tool that ships visible but
// un-whitelisted would be denied at runtime; a tool whose risk drifts
// below its class would be auto-authorized — both fail here.
func TestBrowserGovernanceConsistency(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	names := al.tools.RegisteredNames()

	seen := 0
	for _, name := range names {
		if !isBrowserTool(name) {
			continue
		}
		seen++
		op, ok := browserOp(name)
		if !ok {
			t.Errorf("%s registered but missing from the browser gate whitelist (would be denied at runtime)", name)
			continue
		}
		if op != strings.TrimPrefix(name, "browser_") {
			t.Errorf("%s: gate op %q must equal the tool action", name, op)
		}
		tool, found := al.tools.Get(name)
		if !found {
			t.Errorf("%s: registry Get failed", name)
			continue
		}
		clsf, has := tool.(interface{ Classify() string })
		if !has {
			t.Errorf("%s: browser tool must expose Classify()", name)
			continue
		}
		want := classRisk(clsf.Classify(), name)
		if got := browserRisk(op); got != want {
			t.Errorf("%s: gate risk = %s, tool class %q implies %s", name, got, clsf.Classify(), want)
		}
	}
	if seen < 20 {
		t.Fatalf("expected the full browser surface registered, found %d browser tools", seen)
	}
}

// Only submit and upload may ever be high impact — a new action must
// declare its risk consciously, never inherit it.
func TestBrowserRiskHighImpactIsClosed(t *testing.T) {
	for _, op := range []string{"navigate", "snapshot", "wait", "find", "screenshot", "scroll",
		"console", "network", "a11y", "click", "type", "press", "fill", "fill_form",
		"select", "check", "hover", "drag", "dialog", "download"} {
		if browserRisk(op) == permissions.RiskHighImpact {
			t.Errorf("%s must not be high impact", op)
		}
	}
	for _, op := range []string{"submit", "upload"} {
		if browserRisk(op) != permissions.RiskHighImpact {
			t.Errorf("%s must be high impact", op)
		}
	}
}

// Unknown browser_* names stay denied outright (the belt to the
// whitelist's suspenders).
func TestBrowserOpRejectsUnknown(t *testing.T) {
	for _, name := range []string{"browser_eval", "browser_upload_all", "browser", "browser_transact", "browser_cookies"} {
		if op, ok := browserOp(name); ok {
			t.Errorf("%s must be denied, got op %q", name, op)
		}
	}
}
