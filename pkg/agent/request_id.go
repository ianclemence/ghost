package agent

import "context"

type requestIDKey struct{}

// WithRequestID carries the caller's request id into the turn. The chat
// endpoint answers the phone under the id the phone sent; the turn must file
// everything it does (approvals above all) under that same id. When the turn
// minted its own, the endpoint could not find the turn's pending approval,
// reported "success", and settling that "finished" turn closed the very
// browser session the approval was waiting to resume.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func requestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
