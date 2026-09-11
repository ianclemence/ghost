package main

import (
	"strings"
	"testing"
)

// A stored next-run in the past must not be shown verbatim: the scheduler
// only recomputes while it runs, so the list command recomputes recurring
// schedules live for display (stored state untouched).

// The dashboard header must report the gateway's version when connected
// (never a hardcoded constant), falling back to the binary's own version.
func TestDisplayVersionPrefersGateway(t *testing.T) {
	m := dashboardModel{doctor: doctorPayload{Version: "9.9.9"}}
	if got := m.displayVersion(); got != "9.9.9" {
		t.Fatalf("expected gateway version, got %q", got)
	}
	m2 := dashboardModel{}
	want := strings.TrimPrefix(version, "v")
	if want == "" {
		want = "dev"
	}
	if got := m2.displayVersion(); got != want {
		t.Fatalf("expected binary version fallback %q, got %q", want, got)
	}
}
