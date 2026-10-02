package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
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

// "Yes, run it" means what Ghost just offered: its tools come from the offer.
func TestShortReplyChoosesToolsFromTheOffer(t *testing.T) {
	msgs := []providers.Message{
		{Role: "assistant", Content: "Do you want me to run the Google Flights search for Mon 26 October now?"},
		{Role: "user", Content: "Yes, run it."},
	}
	got := toolIntentText("Yes, run it.", msgs)
	if !strings.Contains(got, "google flights") && !strings.Contains(strings.ToLower(got), "google flights") {
		t.Fatalf("a short reply must carry the offer it answers, got %q", got)
	}
	long := strings.Repeat("tell me about the history of the Raspberry Pi in detail please ", 3)
	if toolIntentText(long, msgs) != long {
		t.Fatalf("a full request stands on its own words")
	}
}
