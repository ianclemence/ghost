package skills

import "testing"

func TestGetCapabilityGeneric(t *testing.T) {
	cap := GetCapability("weather")
	if cap.ID != "weather.current" {
		t.Fatalf("expected weather.current, got %s", cap.ID)
	}
	if !cap.Allows("exec") || !cap.Allows("read_file") {
		t.Fatalf("weather must allow exec/read_file")
	}
	if cap.Allows("web_search") || cap.Allows("list_dir") || cap.Allows("subagent") {
		t.Fatalf("weather must block wandering tools")
	}
	// Presenting results is never wandering: a committed capability must
	// not take away the card the routine exists to show.
	if !cap.Allows("present_card") {
		t.Fatalf("weather must allow present_card")
	}
	if !GetCapability("calendar").Allows("present_card") {
		t.Fatalf("calendar must allow present_card (morning brief renders a card)")
	}
	if cap.MaxAttempts < 1 || cap.MaxAttempts > 3 {
		t.Fatalf("weather MaxAttempts should be bounded, got %d", cap.MaxAttempts)
	}

	// Unknown skill stays unrestricted (no code change needed for new skills).
	unknown := GetCapability("my-new-skill")
	if unknown.ID == "" || unknown.Skill == "" {
		t.Fatalf("unknown skill should get default capability")
	}
	if !unknown.Allows("web_search") {
		t.Fatalf("unknown skill should be unrestricted")
	}
}

// HasCapability is the gate the standing-grant confirm path relies on
// (standing_turn.go): undeclared capabilities can never be granted, so pin
// both directions.
func TestHasCapabilityPinsDeclared(t *testing.T) {
	for _, id := range []string{"calendar.read", "calendar.create", "weather.current", "email.read"} {
		if !HasCapability(id) {
			t.Fatalf("declared capability %q must be recognized", id)
		}
	}
	if HasCapability("no.such.capability") {
		t.Fatal("undeclared capability must not be recognized")
	}
}

// Exemptions are data (ToolClass), not branches: every class-exempt tool
// stays allowed under every committed capability, and ordinary tools stay
// bound by AllowedTools.
func TestToolClassExemptions(t *testing.T) {
	if ToolClass("read_file") == "" || ToolClass("present_card") == "" {
		t.Fatal("read_file and present_card must have exemption classes")
	}
	if ToolClass("exec") != "" || ToolClass("web_search") != "" {
		t.Fatal("exec and web_search must stay capability-bound")
	}
	for _, skill := range []string{"weather", "calendar", "email"} {
		cap := GetCapability(skill)
		if !cap.Allows("read_file") || !cap.Allows("present_card") {
			t.Fatalf("%s must allow class-exempt tools", skill)
		}
		if cap.Allows("web_search") {
			t.Fatalf("%s must block web_search", skill)
		}
	}
}

func TestValidateResult(t *testing.T) {
	cap := GetCapability("weather")
	if !cap.ValidateResult(`Bangkok: +27C cloudy humidity 75%`) {
		t.Fatalf("valid weather result rejected")
	}
	for _, bad := range []string{"", "   ", "command not found", "metadata only", "context canceled", "No OAuth token"} {
		if cap.ValidateResult(bad) {
			t.Fatalf("invalid result %q accepted", bad)
		}
	}
}

func TestCleanFailureNoLeak(t *testing.T) {
	for _, skill := range []string{"weather", "currency", "flight", "calendar"} {
		msg := GetCapability(skill).CleanFailure()
		for _, leak := range []string{"SKILL.md", ".bundled", "DIR:", "/var/lib", "gcalcli", "API_KEY"} {
			if len(msg) > 0 && contains(msg, leak) {
				t.Fatalf("skill %s CleanFailure leaks %q: %q", skill, leak, msg)
			}
		}
		if msg == "" {
			t.Fatalf("skill %s empty CleanFailure", skill)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestPendingTTL(t *testing.T) {
	SetPending("sess-test", PendingContinuation{CapabilityID: "flight.status", Skill: "flight", MissingField: "flight_number", Question: "Which flight?", OriginalTask: "status"})
	if _, ok := GetPending("sess-test"); !ok {
		t.Fatalf("pending not stored")
	}
	ClearPending("sess-test")
	if _, ok := GetPending("sess-test"); ok {
		t.Fatalf("pending not cleared")
	}
}
