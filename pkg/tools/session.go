package tools

import "context"

type sessionKeyContextKey struct{}

// WithSessionKey carries the calling session into tool execution so
// session-scoped enforcement (memory visibility, permissions) works
// without trusting model-supplied arguments.
func WithSessionKey(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil || sessionKey == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionKeyContextKey{}, sessionKey)
}

// SessionKeyFromContext returns the calling session, or "" when absent.
func SessionKeyFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	key, _ := ctx.Value(sessionKeyContextKey{}).(string)
	return key
}
