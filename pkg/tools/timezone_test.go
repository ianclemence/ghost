package tools

import (
	"context"
	"testing"
)

// DeviceTimezone must always yield a usable zone (a valid IANA name or UTC)
// so user-facing dates never silently fall back to an undefined local zone.
func TestDeviceTimezoneUsable(t *testing.T) {
	tz := DeviceTimezone()
	if tz == "" {
		t.Fatal("DeviceTimezone must not be empty")
	}
	if tz != "UTC" && ValidateTimezone(tz) == "" {
		t.Fatalf("DeviceTimezone returned an invalid zone: %q", tz)
	}
	if DeviceLocation() == nil {
		t.Fatal("DeviceLocation must not be nil")
	}
}

func TestRequestLocationRoundTrip(t *testing.T) {
	loc := RequestLocation{City: "Bangkok", Latitude: "13.75", Longitude: "100.5"}
	ctx := WithRequestLocation(context.Background(), loc)
	got := RequestLocationFrom(ctx)
	if got.City != "Bangkok" || !got.HasCoordinates() {
		t.Fatalf("location did not round-trip: %+v", got)
	}
	if RequestLocationFrom(nil).City != "" {
		t.Fatal("nil context must yield empty location")
	}
}
