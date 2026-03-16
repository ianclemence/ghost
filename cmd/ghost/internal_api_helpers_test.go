package main

import (
	"net/http"
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
