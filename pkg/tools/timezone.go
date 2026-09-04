package tools

import (
	"context"
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
	City     string
	Latitude string
	Longitude string
	Timezone string
	Source   string
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
