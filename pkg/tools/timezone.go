package tools

import (
	"context"
	"os"
	"strings"
	"time"
)

// requestTimezoneKey carries the per-request IANA timezone (e.g. from the
// mobile client's device locale) through the agent turn into tools that need
// it. It intentionally avoids mutating shared tool state, so concurrent turns
// from different devices cannot leak timezones into each other.
type requestTimezoneKey struct{}

// WithRequestTimezone returns a context carrying tz for the current turn.
// An empty or invalid tz is stored as "" and callers fall back to their default.
func WithRequestTimezone(ctx context.Context, tz string) context.Context {
	tz = ValidateTimezone(tz)
	if tz == "" {
		return ctx
	}
	return context.WithValue(ctx, requestTimezoneKey{}, tz)
}

// RequestTimezone returns the per-request timezone, or "" when none was set.
func RequestTimezone(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	tz, _ := ctx.Value(requestTimezoneKey{}).(string)
	return tz
}

// ValidateTimezone reports whether tz is a known IANA timezone name.
// It returns the cleaned name, or "" when tz is empty or unknown.
func ValidateTimezone(tz string) string {
	if tz == "" {
		return ""
	}
	if len(tz) > 64 {
		return ""
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ""
	}
	return tz
}

// DeviceTimezone returns the host device's configured IANA timezone, the same
// way a smart speaker uses its device's configured region. It is Ghost's
// default for user-facing dates/times and scheduling when the client does not
// provide one.
//
// Precedence: /etc/localtime symlink, then /etc/timezone, then the TZ env,
// then the Go process local zone, then UTC. Reading the system configuration
// first means a stray TZ=UTC in an env file cannot make Ghost report the
// wrong day for the owner.
func DeviceTimezone() string {
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			if tz := ValidateTimezone(link[i+len("zoneinfo/"):]); tz != "" {
				return tz
			}
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := ValidateTimezone(strings.TrimSpace(string(b))); tz != "" {
			return tz
		}
	}
	if tz := ValidateTimezone(strings.TrimSpace(os.Getenv("TZ"))); tz != "" {
		return tz
	}
	if time.Local != nil {
		if tz := ValidateTimezone(time.Local.String()); tz != "" {
			return tz
		}
	}
	return "UTC"
}

// DeviceLocation returns the time.Location for the device timezone.
func DeviceLocation() *time.Location {
	if l, err := time.LoadLocation(DeviceTimezone()); err == nil {
		return l
	}
	return time.UTC
}

// Mobile metadata contract for /v1/chat (same path the mobile app uses).
//
//	Optional request metadata (all optional, all validated, never trusted blindly):
//	  timezone  IANA name, e.g. "Asia/Bangkok". Authoritative for scheduling
//	            ("9 AM" means 9 AM there). Validated via ValidateTimezone;
//	            unknown/empty falls back to tool default (UTC).
//	  city      Free-form city, e.g. "Bangkok". Preferred for weather/aqi/
//	            nearby when present. Trimmed, max 64 chars.
//	  latitude  Decimal degrees as string, e.g. "13.7563".
//	  longitude Decimal degrees as string, e.g. "100.5018".
//	  location_source  Opaque source label, e.g. "mobile_ip", for prompting.
//
//	Fallback behavior:
//	  - If location (city or lat+lon) is present, skills use it directly.
//	  - If absent, Ghost asks once ("Which city should I check?") and resumes
//	    when the user replies with a short value — no repeat of full request.
//	  - Timezone absent/unknown never blocks; scheduling falls back to UTC
//	    and labels the fallback explicitly.
type RequestLocation struct {
	City      string
	Latitude  string
	Longitude string
	Timezone  string
	Source    string
}

// RequestLocationFromMetadata extracts the validated location contract.
func RequestLocationFromMetadata(metadata map[string]string) RequestLocation {
	if metadata == nil {
		return RequestLocation{}
	}
	loc := RequestLocation{
		Timezone: ValidateTimezone(metadata["timezone"]),
		Source:   metadata["location_source"],
	}
	if c := metadata["city"]; c != "" && len(c) <= 64 {
		loc.City = c
	}
	if lat, lon := metadata["latitude"], metadata["longitude"]; lat != "" && lon != "" {
		loc.Latitude = lat
		loc.Longitude = lon
	}
	return loc
}

// requestLocationKey carries the per-request device location through the
// agent turn into tools that need it (weather, aqi, nearby). It avoids
// appending location prose to the user message — the runtime resolves the
// location directly, so it never leaks into the model's reply.
type requestLocationKey struct{}

// WithRequestLocation returns a context carrying the device location for the
// current turn. An empty location is a no-op.
func WithRequestLocation(ctx context.Context, loc RequestLocation) context.Context {
	if loc.City == "" && (loc.Latitude == "" || loc.Longitude == "") {
		return ctx
	}
	return context.WithValue(ctx, requestLocationKey{}, loc)
}

// RequestLocationFrom returns the per-request device location, or a zero
// value when none was set.
func RequestLocationFrom(ctx context.Context) RequestLocation {
	if ctx == nil {
		return RequestLocation{}
	}
	loc, _ := ctx.Value(requestLocationKey{}).(RequestLocation)
	return loc
}

// HasCoordinates reports whether the location carries usable lat/lon.
func (l RequestLocation) HasCoordinates() bool {
	return l.Latitude != "" && l.Longitude != ""
}
