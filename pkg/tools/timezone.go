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
