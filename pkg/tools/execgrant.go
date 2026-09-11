package tools

import (
	"context"
	"strings"
)

// Execution grants close the last bypass around the permission broker: even
// a broker-authorized turn executes primitives only with a grant stamped on
// the turn context, and direct registry calls without one are denied loudly.
//
// Grant-required tools are the process, actuation, and third-party-code
// primitives: exec, sandbox, update, hardware/device actuators, the
// browser/computer families (their gates stamp the grant after approval),
// and mcp_* (third-party out-of-process servers). Read-only and data tools
// need no grant — the broker and capability gates still govern them.
//
// Grants flow through context, so nested execution (subagents, resume
// paths, cron jobs) inherits exactly the approved scope — never more.
type grantKey struct{}

// grantWildcard covers every grant-required tool. Used only for trusted
// internal callers (never model-reachable paths) and tests.
const grantWildcard = "*"

// GrantExec returns a context carrying an execution grant for the named
// tools. Grants accumulate across calls.
func GrantExec(ctx context.Context, names ...string) context.Context {
	set := execGrants(ctx)
	next := make(map[string]bool, len(set)+len(names))
	for n := range set {
		next[n] = true
	}
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			next[n] = true
		}
	}
	return context.WithValue(ctx, grantKey{}, next)
}

// WithSystemGrant returns a context with a wildcard grant for trusted
// internal callers (durable automation executors, tests). It must never be
// attached to model-driven turn contexts.
func WithSystemGrant(ctx context.Context) context.Context {
	return GrantExec(ctx, grantWildcard)
}

func execGrants(ctx context.Context) map[string]bool {
	if ctx == nil {
		return nil
	}
	if set, ok := ctx.Value(grantKey{}).(map[string]bool); ok {
		return set
	}
	return nil
}

// execGranted reports whether ctx carries a grant for the tool.
func execGranted(ctx context.Context, name string) bool {
	set := execGrants(ctx)
	if len(set) == 0 {
		return false
	}
	return set[grantWildcard] || set[name]
}

// GrantRequired reports whether the tool is an execution primitive that
// needs a turn-scoped grant on top of broker authorization.
func GrantRequired(name string) bool {
	switch name {
	case "exec", "sandbox", "update", "i2c", "spi", "hass":
		return true
	}
	for _, prefix := range []string{"computer_", "browser_", "mcp_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
