package agent

import (
	"context"
	"os"
	"time"
)

// DefaultUnattendedLimit is how long one turn Ghost starts on its own (a
// routine, a promise it made, an idea it acted on) may run. Nobody is watching
// these, so a hung page or a provider that never answers would otherwise hold
// the Pod's one agent for hours. The tool-call cap bounds how much a turn
// does; this bounds how long it takes.
const DefaultUnattendedLimit = 20 * time.Minute

// UnattendedLimit reads GHOST_UNATTENDED_TIMEOUT (a Go duration such as "45m";
// "0" removes the limit) and falls back to DefaultUnattendedLimit.
func UnattendedLimit() time.Duration {
	if v := os.Getenv("GHOST_UNATTENDED_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return d
		}
	}
	return DefaultUnattendedLimit
}

// UnattendedContext bounds ctx for a turn no one is watching.
func UnattendedContext(ctx context.Context) (context.Context, context.CancelFunc) {
	d := UnattendedLimit()
	if d == 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}
