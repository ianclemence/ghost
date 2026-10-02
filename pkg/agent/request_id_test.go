package agent

import (
	"context"
	"testing"
)

// The phone's request id must reach the turn unchanged: the chat endpoint
// looks up the turn's pending approval under it.
func TestRequestIDTravelsWithTheTurn(t *testing.T) {
	ctx := WithRequestID(context.Background(), "m-1790972082342")
	if got := requestIDFrom(ctx); got != "m-1790972082342" {
		t.Fatalf("requestIDFrom = %q", got)
	}
	if got := requestIDFrom(WithRequestID(context.Background(), "")); got != "" {
		t.Fatalf("an empty id must not be carried, got %q", got)
	}
	if got := requestIDFrom(nil); got != "" { //nolint:staticcheck // nil-safe by contract
		t.Fatalf("nil context must be safe, got %q", got)
	}
}
