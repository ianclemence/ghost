package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestResolveRequestChannel(t *testing.T) {
	if got := resolveRequestChannel("telegram", "mobile", "expo"); got != "telegram" {
		t.Fatalf("expected explicit channel to win, got %q", got)
	}
	if got := resolveRequestChannel("", "mobile", "curl/8.0"); got != "mobile" {
		t.Fatalf("expected mobile via header, got %q", got)
	}
	if got := resolveRequestChannel("", "", "Expo Go/2.0"); got != "mobile" {
		t.Fatalf("expected mobile via user-agent, got %q", got)
	}
	if got := resolveRequestChannel("", "", "curl/8.0"); got != "cli" {
		t.Fatalf("expected non-mobile default cli, got %q", got)
	}
}

func TestRequestLocationMetadata(t *testing.T) {
	loc := tools.RequestLocationFromMetadata(map[string]string{
		"city": "Bangkok", "latitude": "13.75", "longitude": "100.5",
		"timezone": "Asia/Bangkok", "location_source": "mobile_ip",
	})
	if loc.City != "Bangkok" || !loc.HasCoordinates() || loc.Timezone != "Asia/Bangkok" {
		t.Fatalf("location metadata not parsed: %+v", loc)
	}
	empty := tools.RequestLocationFromMetadata(nil)
	if empty.City != "" || empty.HasCoordinates() {
		t.Fatalf("nil metadata must be empty: %+v", empty)
	}
}

func TestIsUserVisibleHistoryMessage(t *testing.T) {
	if !isUserVisibleHistoryMessage("user", "hello", nil) {
		t.Fatalf("expected user message to be visible")
	}
	metaWithToolCalls := []byte(`{"tool_calls":[{"id":"tc-1"}]}`)
	if isUserVisibleHistoryMessage("assistant", "intermediate planning", metaWithToolCalls) {
		t.Fatalf("expected assistant tool-call turn to be hidden")
	}
	leakySkill := "name: weather\ndescription: Get current weather and forecast"
	if isUserVisibleHistoryMessage("assistant", leakySkill, nil) {
		t.Fatalf("expected skill frontmatter-like leak to be hidden")
	}
	finalResponse := "Weather in Singapore is 31°C and humid."
	if !isUserVisibleHistoryMessage("assistant", finalResponse, nil) {
		t.Fatalf("expected clean assistant response to be visible")
	}
}
