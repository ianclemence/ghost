package tools

import "context"

// requestMessageKey carries the owner's own words for the current turn into
// tools that need to check a model's restatement against them. Like the
// timezone carrier, it lives on the context rather than in shared tool state,
// so concurrent turns cannot see each other's messages.
type requestMessageKey struct{}

// WithRequestMessage returns a context carrying the owner's raw turn text.
// An empty message is a no-op, so callers that do not have one keep the
// previous behaviour.
func WithRequestMessage(ctx context.Context, message string) context.Context {
	if message == "" {
		return ctx
	}
	return context.WithValue(ctx, requestMessageKey{}, message)
}

// RequestMessage returns the owner's own words for this turn, or "" when the
// caller did not supply them (direct tool use, background work).
func RequestMessage(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	msg, _ := ctx.Value(requestMessageKey{}).(string)
	return msg
}
