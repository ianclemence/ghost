package presentation

import (
	"testing"
	"time"
)

func TestMemory(t *testing.T) {
	learned := time.Now().Add(-24 * time.Hour)
	result := Memory("Prefers tea over coffee", "preference", "food", "User prefers tea over coffee", 0.95, learned)
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Prefers tea over coffee") {
		t.Error("expected result to contain title")
	}
	if !contains(result, "[Food]") {
		t.Error("expected result to contain domain badge")
	}
	if !contains(result, "Learned") {
		t.Error("expected result to contain learned time")
	}
}

func TestSession(t *testing.T) {
	lastActive := time.Now().Add(-2 * time.Hour)
	result := Session("Weather in Bangkok", 15, lastActive, "User asked about weather in Bangkok")
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Weather in Bangkok") {
		t.Error("expected result to contain title")
	}
	if !contains(result, "15 messages") {
		t.Error("expected result to contain message count")
	}
	if !contains(result, "2 hours ago") {
		t.Error("expected result to contain last active time")
	}
}

func TestActivity(t *testing.T) {
	ts := time.Now().Add(-5 * time.Minute)
	result := Activity("web_search", "weather in Bangkok", ts)
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Searched the web") {
		t.Error("expected result to contain event description")
	}
	if !contains(result, "weather in Bangkok") {
		t.Error("expected result to contain detail")
	}
}

func TestAutomation(t *testing.T) {
	lastRun := time.Now().Add(-1 * time.Hour)
	result := Automation("Weather Check", "0 8 * * *", lastRun, true)
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Weather Check") {
		t.Error("expected result to contain name")
	}
	if !contains(result, "Every day at 8:00 AM") {
		t.Error("expected result to contain human-readable schedule")
	}
}

func TestSkill(t *testing.T) {
	result := Skill("weather", "Check the weather for any location", true)
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "weather") {
		t.Error("expected result to contain name")
	}
	if !contains(result, "Check the weather") {
		t.Error("expected result to contain description")
	}
}

func TestSkillDisabled(t *testing.T) {
	result := Skill("weather", "Check the weather", false)
	
	if !contains(result, "[disabled]") {
		t.Error("expected result to contain disabled badge")
	}
}

func TestDevice(t *testing.T) {
	lastSeen := time.Now().Add(-3 * time.Minute)
	result := Device("iPhone", "connected", lastSeen)
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "iPhone") {
		t.Error("expected result to contain name")
	}
	if !contains(result, "Connected") {
		t.Error("expected result to contain status")
	}
}

func TestChannel(t *testing.T) {
	result := Channel("Telegram", "connected")
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "Telegram") {
		t.Error("expected result to contain name")
	}
	if !contains(result, "Connected") {
		t.Error("expected result to contain status")
	}
}

func TestSystemHealthy(t *testing.T) {
	result := System(true, nil)
	
	if !contains(result, "Everything is running normally") {
		t.Error("expected result to contain healthy message")
	}
}

func TestSystemUnhealthy(t *testing.T) {
	details := map[string]string{
		"cpu": "85%",
		"memory": "90%",
	}
	result := System(false, details)
	
	if !contains(result, "System needs attention") {
		t.Error("expected result to contain unhealthy message")
	}
	if !contains(result, "Cpu: 85%") {
		t.Error("expected result to contain details")
	}
}

func TestError(t *testing.T) {
	result := Error("model_unavailable", "", "Provider timeout after 30 seconds")
	
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "cloud AI service") {
		t.Error("expected result to contain user-friendly message")
	}
	if !contains(result, "Provider timeout") {
		t.Error("expected result to contain technical details")
	}
}

func TestTimeAgo(t *testing.T) {
	cases := []struct {
		input time.Time
		want  string
	}{
		{time.Now(), "just now"},
		{time.Now().Add(-1 * time.Minute), "1 minute ago"},
		{time.Now().Add(-5 * time.Minute), "5 minutes ago"},
		{time.Now().Add(-1 * time.Hour), "1 hour ago"},
		{time.Now().Add(-3 * time.Hour), "3 hours ago"},
		{time.Now().Add(-24 * time.Hour), "yesterday"},
		{time.Now().Add(-7 * 24 * time.Hour), "1 week ago"},
	}
	
	for _, tc := range cases {
		got := TimeAgo(tc.input)
		if got != tc.want {
			t.Errorf("TimeAgo(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestFormatSchedule(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"0 8 * * *", "Every day at 8:00 AM"},
		{"0 9 * * 1-5", "Weekdays at 9:00 AM"},
		{"0 0 * * *", "Every day at midnight"},
		{"0 */2 * * *", "Every 2 hours"},
		{"*/15 * * * *", "Every 15 minutes"},
	}
	
	for _, tc := range cases {
		got := FormatSchedule(tc.input)
		if got != tc.want {
			t.Errorf("FormatSchedule(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		max   int
		want  string
	}{
		{"Short", 10, "Short"},
		{"This is a longer string that needs truncation", 20, "This is a longer..."},
		{"Hello World", 5, "Hell..."},
	}
	
	for _, tc := range cases {
		got := Truncate(tc.input, tc.max)
		if got != tc.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
