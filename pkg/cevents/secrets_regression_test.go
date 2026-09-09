package cevents

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/product"
)

// Secret-shaped values injected into event payloads, credentials, browser
// output, and errors must never survive into the canonical warehouse or
// any SSE/activity projection. Publish redacts at the boundary; this is
// the permanent regression for that contract.
func TestEventsNeverCarrySecretsIntoProjections(t *testing.T) {
	s := openReplayStream(t)
	const liveToken = "sk-live-0123456789abcdefghijklmnop"
	const bearer = "Bearer eyJhbGciOiJIUzI1NiJ9.abcdefghijklmnopqrstuvwxyz0123456789"
	ev := s.Publish(&Event{
		Type: ToolCompleted, RequestID: "req-sec", SessionID: "sess-sec",
		Status: "success", Visibility: product.VisInternalTrace,
		Payload: map[string]interface{}{
			"tool":          "browser",
			"api_token":     liveToken,
			"password":      "hunter2secret",
			"authorization": bearer,
			"page_text":     "page shows " + liveToken + " and " + bearer,
			"owner":         "ian",
		},
	})
	// Warehouse read: keys masked/shape-free.
	for _, e := range s.ByRequest("req-sec") {
		if p := e.Payload; p != nil {
			if p["api_token"] == liveToken || p["password"] == "hunter2secret" || p["authorization"] == bearer {
				t.Fatalf("secret leaked into warehouse payload: %+v", p)
			}
			text := joinAll(p)
			if strings.Contains(text, liveToken) || strings.Contains(text, "hunter2secret") {
				t.Fatalf("secret value leaked into warehouse payload: %+v", p)
			}
			if p["owner"] != "ian" {
				t.Fatalf("innocent fields must survive redaction: %+v", p)
			}
		}
	}
	// Recent + durable replay carry the same redacted payload.
	for _, e := range s.Recent(10, Filter{}) {
		if strings.Contains(joinAll(e.Payload), liveToken) {
			t.Fatal("secret leaked into Recent projection")
		}
	}
	for _, e := range s.Replay("sec-cons", 10, Filter{}) {
		if strings.Contains(joinAll(e.Payload), liveToken) {
			t.Fatal("secret leaked into Replay")
		}
	}
	// A user-visible event's SSE form never carries the secret either.
	s.Publish(&Event{
		Type: MessageReceived, RequestID: "req-sec", SessionID: "sess-sec",
		Visibility: product.VisUserMessage,
		Payload: map[string]interface{}{
			"content": "success — token " + liveToken,
		},
	})
	_ = ev
	for _, e := range s.Recent(10, Filter{}) {
		if _, data, ok := e.SSEForm(); ok {
			if strings.Contains(data, liveToken) {
				t.Fatalf("secret leaked into SSE form: %s", data)
			}
		}
	}
}

func joinAll(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	var parts []string
	for _, v := range m {
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}
