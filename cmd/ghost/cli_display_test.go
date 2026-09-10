package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/cron"
)

// A stored next-run in the past must not be shown verbatim: the scheduler
// only recomputes while it runs, so the list command recomputes recurring
// schedules live for display (stored state untouched).
func TestCronNextRunForDisplayRecomputesStale(t *testing.T) {
	now := time.Now()
	every := int64(7 * 24 * 3600 * 1000)
	cases := []struct {
		name  string
		sched cron.CronSchedule
	}{
		{"cron", cron.CronSchedule{Kind: "cron", Expr: "0 8 * * 1"}},
		{"every", cron.CronSchedule{Kind: "every", EveryMS: &every}},
	}
	for _, c := range cases {
		got := cronNextRunForDisplay(&c.sched, now)
		if got == nil {
			t.Fatalf("%s: expected a recomputed next run", c.name)
		}
		if !got.After(now) {
			t.Fatalf("%s: recomputed run must be in the future, got %v", c.name, got)
		}
	}
	// One-shot schedules and garbage must not fabricate a future run.
	if got := cronNextRunForDisplay(&cron.CronSchedule{Kind: "at"}, now); got != nil {
		t.Fatalf("one-shot 'at' must not produce a run, got %v", got)
	}
	if got := cronNextRunForDisplay(&cron.CronSchedule{Kind: "cron", Expr: "not an expr"}, now); got != nil {
		t.Fatalf("invalid expr must not produce a run, got %v", got)
	}
	if got := cronNextRunForDisplay(nil, now); got != nil {
		t.Fatalf("nil schedule must not produce a run, got %v", got)
	}
}

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
