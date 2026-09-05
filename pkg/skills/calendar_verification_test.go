package skills

import (
	"strings"
	"testing"
)

func TestScopeJustificationComplete(t *testing.T) {
	j := CalendarScopeJustification()
	if len(j) != 2 {
		t.Fatalf("must justify exactly the requested scopes, got %d", len(j))
	}
	seen := map[string]bool{}
	for _, s := range j {
		if s.Scope == "" || s.Purpose == "" || s.WhyNarrow == "" {
			t.Fatalf("incomplete justification: %+v", s)
		}
		seen[s.Scope] = true
	}
	if !seen[ScopeCalendarReadonly] || !seen[ScopeCalendarEvents] {
		t.Fatal("both scopes must be justified")
	}
	// No full-scope request, ever.
	for _, s := range j {
		if s.Scope == "https://www.googleapis.com/auth/calendar" {
			t.Fatal("full calendar scope must never appear")
		}
	}
}

func TestRequestedScopesNeverBroad(t *testing.T) {
	if !RequestedScopesNeverBroad(ScopesFor(false)) || !RequestedScopesNeverBroad(ScopesFor(true)) {
		t.Fatal("runtime scopes must be within the justified set")
	}
	if RequestedScopesNeverBroad([]string{"https://www.googleapis.com/auth/calendar"}) {
		t.Fatal("full scope must be rejected")
	}
	if RequestedScopesNeverBroad([]string{"https://www.googleapis.com/auth/drive"}) {
		t.Fatal("unrelated scope must be rejected")
	}
}

func TestChecklistCoversVerification(t *testing.T) {
	items := CalendarVerificationChecklist()
	if len(items) < 5 {
		t.Fatal("checklist must cover the deployment path")
	}
	automatable := 0
	for _, it := range items {
		if it.Step == "" || it.Detail == "" {
			t.Fatalf("incomplete item: %+v", it)
		}
		if it.Automatable {
			automatable++
		}
	}
	if automatable == 0 {
		t.Fatal("must identify which steps are code-enforced")
	}
	if !strings.Contains(items[0].Detail, "readonly") {
		t.Fatal("checklist must state the readonly default")
	}
}
