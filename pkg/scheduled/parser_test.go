package scheduled

import (
	"testing"
	"time"
)

func TestParseNaturalLanguage(t *testing.T) {
	referenceTime := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	timezone := "UTC"

	tests := []struct {
		name        string
		input       string
		wantErr     bool
		wantRecur   bool
		wantOneTime bool
	}{
		// Recurring schedules
		{
			name:      "every day at 8 AM",
			input:     "every day at 8 AM",
			wantRecur: true,
		},
		{
			name:      "every Monday at 9 AM",
			input:     "every monday at 9 AM",
			wantRecur: true,
		},
		{
			name:      "every week at 10 AM",
			input:     "every week at 10 AM",
			wantRecur: true,
		},
		{
			name:      "every month on the 1st at 9 AM",
			input:     "every month on the 1st at 9 AM",
			wantRecur: true,
		},
		{
			name:      "every 2 hours",
			input:     "every 2 hours",
			wantRecur: true,
		},
		{
			name:      "every 30 minutes",
			input:     "every 30 minutes",
			wantRecur: true,
		},
		// One-time schedules
		{
			name:        "tomorrow at 9 AM",
			input:       "tomorrow at 9 AM",
			wantOneTime: true,
		},
		{
			name:        "today at 3 PM",
			input:       "today at 3 PM",
			wantOneTime: true,
		},
		{
			name:        "Friday at 3 PM",
			input:       "Friday at 3 PM",
			wantOneTime: true,
		},
		{
			name:        "in 2 hours",
			input:       "in 2 hours",
			wantOneTime: true,
		},
		{
			name:        "in 30 minutes",
			input:       "in 30 minutes",
			wantOneTime: true,
		},
		// Invalid
		{
			name:    "invalid input",
			input:   "invalid schedule",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseNaturalLanguage(tt.input, referenceTime, timezone)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseNaturalLanguage(%q) = %v, want error", tt.input, result)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseNaturalLanguage(%q) returned error: %v", tt.input, err)
				return
			}
			if tt.wantRecur && !result.IsRecurring {
				t.Errorf("ParseNaturalLanguage(%q) is not recurring", tt.input)
			}
			if tt.wantOneTime && !result.IsOneTime {
				t.Errorf("ParseNaturalLanguage(%q) is not one-time", tt.input)
			}
		})
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		hour   int
		minute int
		want   string
	}{
		{9, 0, "9 AM"},
		{14, 30, "2:30 PM"},
		{0, 0, "12 AM"},
		{12, 0, "12 PM"},
		{17, 45, "5:45 PM"},
	}

	for _, tt := range tests {
		result := formatTimeDisplay(tt.hour, tt.minute)
		if result != tt.want {
			t.Errorf("formatTimeDisplay(%d, %d) = %q, want %q", tt.hour, tt.minute, result, tt.want)
		}
	}
}

func TestParseHour(t *testing.T) {
	tests := []struct {
		hourStr string
		ampm    string
		want    int
	}{
		{"9", "AM", 9},
		{"2", "PM", 14},
		{"12", "PM", 12},
		{"12", "AM", 0},
		{"9", "", 9},
	}

	for _, tt := range tests {
		result := parseHour(tt.hourStr, tt.ampm)
		if result != tt.want {
			t.Errorf("parseHour(%q, %q) = %d, want %d", tt.hourStr, tt.ampm, result, tt.want)
		}
	}
}
