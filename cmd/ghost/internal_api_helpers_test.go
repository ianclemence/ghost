package main

import (
	"strings"
	"testing"
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

func TestEnrichWeatherPrompt(t *testing.T) {
	base := "what is the weather today"
	out := enrichWeatherPrompt(base, map[string]string{
		"city":            "Bangkok",
		"country":         "Thailand",
		"timezone":        "Asia/Bangkok",
		"location_source": "mobile_ip",
	})
	if out == base {
		t.Fatalf("expected enriched prompt with location context")
	}
	if !strings.Contains(out, "Bangkok") {
		t.Fatalf("expected city in enriched prompt, got %q", out)
	}

	explicit := enrichWeatherPrompt("weather in London", map[string]string{
		"city": "Bangkok",
	})
	if explicit != "weather in London" {
		t.Fatalf("expected explicit city prompt unchanged")
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
