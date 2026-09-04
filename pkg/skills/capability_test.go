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
