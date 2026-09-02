package scheduled

import (
	"testing"
	"time"
)

func TestNextCronRun(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) // Wed 10:00 UTC

	tests := []struct {
		name   string
		expr   string
		tz     string
		want   string
	}{
		{"daily 9am", "0 9 * * *", "UTC", "2026-09-03 09:00:00 +0000 UTC"},
		{"monday 9am", "0 9 * * 1", "UTC", "2026-09-07 09:00:00 +0000 UTC"},
		{"month 1st 9am", "0 9 1 * *", "UTC", "2026-10-01 09:00:00 +0000 UTC"},
		{"monday midnight", "0 0 * * 1", "UTC", "2026-09-07 00:00:00 +0000 UTC"},
		{"past today already fired", "0 8 * * *", "UTC", "2026-09-03 08:00:00 +0000 UTC"},
		{"active timezone converted", "0 9 * * *", "Asia/Bangkok", "2026-09-03 02:00:00 +0000 UTC"},
		{"invalid expr returns nil", "not a cron", "UTC", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NextCronRun(tt.expr, tt.tz, now)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("expected nil for invalid expr, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("unexpected nil next run for %q", tt.expr)
			}
			if got.UTC().String() != tt.want {
				t.Errorf("NextCronRun(%q) = %v, want %v", tt.expr, got.UTC(), tt.want)
			}
		})
	}
}
