package product

import (
	"strings"
	"testing"
)

func TestFailureReportLayers(t *testing.T) {
	r := ReportFailure("calendar", "gcalcli", "req-1", 2, ErrProvider, "provider_5xx")
	if r.Completion == CompletionSuccess {
		t.Fatal("failure report must never be success")
	}
	if strings.Contains(strings.ToLower(r.UserMessage), "gcalcli") {
		t.Fatalf("product layer leaked provider: %q", r.UserMessage)
	}
	if r.Diagnostic["provider"] != "gcalcli" || r.Diagnostic["request_id"] != "req-1" || r.Diagnostic["attempt"] != "2" {
		t.Fatalf("diagnostics missing safe fields: %v", r.Diagnostic)
	}
	for k, v := range r.Diagnostic {
		for _, banned := range []string{"sk-", "token", "secret", "key="} {
			if strings.Contains(v, banned) {
				t.Fatalf("diagnostic %s leaked %q", k, banned)
			}
		}
	}
}
