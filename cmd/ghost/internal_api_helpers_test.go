package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestDecodeCronPatchRequest(t *testing.T) {
	reqWithBodyID, _ := http.NewRequest(http.MethodPatch, "/v1/cron/jobs", strings.NewReader(`{"id":"job-1","updates":{"message":"x"}}`))
	id, updates, err := decodeCronPatchRequest(reqWithBodyID, "")
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if id != "job-1" {
		t.Fatalf("expected id from body, got %q", id)
	}
	if updates.Message == nil || *updates.Message != "x" {
		t.Fatalf("expected message update from body")
	}

	reqWithPathID, _ := http.NewRequest(http.MethodPatch, "/v1/cron/jobs/job-2", strings.NewReader(`{"updates":{"message":"y"}}`))
	id, updates, err = decodeCronPatchRequest(reqWithPathID, "job-2")
	if err != nil {
		t.Fatalf("unexpected decode error for path id: %v", err)
	}
	if id != "job-2" {
		t.Fatalf("expected id from path, got %q", id)
	}
	if updates.Message == nil || *updates.Message != "y" {
		t.Fatalf("expected message update from path-based request")
	}

	reqMissingID, _ := http.NewRequest(http.MethodPatch, "/v1/cron/jobs", strings.NewReader(`{"updates":{"message":"z"}}`))
	if _, _, err := decodeCronPatchRequest(reqMissingID, ""); err == nil {
		t.Fatalf("expected error when no id is provided")
	}
}

func TestBuildCronStateResponseShape(t *testing.T) {
	now := time.Now().UTC()
	next := now.Add(1 * time.Hour)
	resp := buildCronStateResponse("job-1", "paused", &now, nil, &next)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal cron state response: %v", err)
	}
	raw := string(data)
	for _, key := range []string{`"id":"job-1"`, `"state":"paused"`, `"paused_at"`, `"next_run_at"`} {
		if !strings.Contains(raw, key) {
			t.Fatalf("expected key/value %s in response: %s", key, raw)
		}
	}
	if strings.Contains(raw, `"resumed_at"`) {
		t.Fatalf("did not expect resumed_at in paused response: %s", raw)
	}
}

func TestBuildCronTriggerResponseShape(t *testing.T) {
	now := time.Now().UTC()
	resp := buildCronTriggerResponse("job-2", now)

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal cron trigger response: %v", err)
	}
	raw := string(data)
	for _, key := range []string{`"id":"job-2"`, `"triggered":true`, `"run_async":true`, `"triggered_at"`} {
		if !strings.Contains(raw, key) {
			t.Fatalf("expected key/value %s in response: %s", key, raw)
		}
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
