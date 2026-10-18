package main

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestUpdateOutcomeSaysWhatHappened(t *testing.T) {
	ok := []tools.SelfCheckRow{{Name: "database", Status: "ok"}}
	bad := []tools.SelfCheckRow{{Name: "database", Status: "ok"}, {Name: "browser", Label: "Browser", Status: "error", Message: "crashed"}}

	if got := updateOutcomeText("v1", "v2", ok); !strings.Contains(got, "now on v2 (it was v1)") || !strings.Contains(got, "everything is working") {
		t.Errorf("healthy update: %q", got)
	}
	if got := updateOutcomeText("v1", "v1", ok); !strings.Contains(got, "didn't take") {
		t.Errorf("rolled back: %q", got)
	}
	got := updateOutcomeText("v1", "v2", bad)
	if !strings.Contains(got, "problems") || !strings.Contains(got, "Browser [error]: crashed") || strings.Contains(got, "everything is working") {
		t.Errorf("unhealthy update must not claim health: %q", got)
	}
	if got := updateOutcomeText("v1", "v2", nil); !strings.Contains(got, "can't say how I'm doing") {
		t.Errorf("no check: %q", got)
	}
}
