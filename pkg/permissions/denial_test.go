package permissions

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every code must construct a complete envelope: code, reason, remedy.
// A denial missing any field is a programmer error, caught here.
func TestDenialEnvelopeComplete(t *testing.T) {
	codes := []DenialCode{
		CodePolicyDenied, CodeApprovalRequired, CodeScopeRoutine,
		CodeScopeContext, CodeSessionExpired, CodeRefStale,
		CodeEvidenceAbsent, CodeGrantRevoked, CodeSubagentUnauthorized,
		CodeBindingMismatch, CodeUnavailable, CodePreconditionFailed,
	}
	for _, c := range codes {
		d := Deny(c, "reason for "+string(c), "do this next")
		if d.Code != c || d.Reason == "" || d.Remedy == "" {
			t.Fatalf("incomplete envelope for %s: %+v", c, d)
		}
		if !strings.Contains(d.Chat(), "reason for") || !strings.Contains(d.Chat(), "do this next") {
			t.Fatalf("Chat must carry reason+remedy: %q", d.Chat())
		}
		if !strings.Contains(d.String(), string(c)) {
			t.Fatalf("String must carry the code: %q", d.String())
		}
	}
}

// Empty fields fall back to safe defaults, never empty output.
func TestDenialDefaults(t *testing.T) {
	d := Deny(CodePolicyDenied, "", "")
	if d.Chat() == "" || d.String() == "" {
		t.Fatal("defaults must produce non-empty output")
	}
}

// Migration lint: the legacy free-text denials must not reappear in the
// migrated surfaces. Every refusal there carries an envelope now; a
// reintroduced bare string fails this test at the PR, not in production.
func TestNoLegacyDenialStrings(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller unavailable")
	}
	repo := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	files := []string{
		"pkg/agent/governance.go",
		"pkg/agent/browser_gate.go",
		"pkg/agent/computer_gate.go",
		"pkg/tools/browser.go",
		"pkg/tools/computer_tool.go",
		"pkg/tools/registry.go",
		"pkg/tools/toolloop.go",
	}
	forbidden := []string{
		"declined by permission policy",
		"Nothing was run.",
		"Nothing was proven to run.",
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, phrase := range forbidden {
			if strings.Contains(string(raw), phrase) {
				t.Errorf("%s: legacy denial %q reintroduced — use permissions.Deny", f, phrase)
			}
		}
	}
}
