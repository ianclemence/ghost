package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// Reading a file is never consequential, even under a consequential skill
// (email/calendar list read_file so the skill can read its own SKILL.md).
// Before this override, reading skills/email/SKILL.md produced a
// consequential "Send this email?" approval — a false consent prompt.
func TestAuthorizedToolRiskReadOnlyTools(t *testing.T) {
	for _, tool := range []string{"read_file", "list_dir", "search_files", "web_fetch", "memory_recall"} {
		if got := authorizedToolRisk("email.read", tool); got != permissions.RiskReadOnly {
			t.Errorf("authorizedToolRisk(email.read, %q) = %q, want read_only", tool, got)
		}
	}
}

// Privileged execution primitives are always high_impact regardless of skill.
func TestAuthorizedToolRiskExecAlwaysHighImpact(t *testing.T) {
	for _, tool := range []string{"exec", "sandbox"} {
		if got := authorizedToolRisk("weather.current", tool); got != permissions.RiskHighImpact {
			t.Errorf("authorizedToolRisk(weather.current, %q) = %q, want high_impact", tool, got)
		}
	}
}

// A genuinely consequential tool still inherits the skill's risk.
func TestAuthorizedToolRiskSendStaysConsequential(t *testing.T) {
	if got := authorizedToolRisk("email.send", "email_send"); got != permissions.RiskConsequential {
		t.Errorf("email_send under email.send = %q, want consequential", got)
	}
}
