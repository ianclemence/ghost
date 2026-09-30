package main

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/push"
)

func TestPushCategoryFor(t *testing.T) {
	cases := []struct {
		name string
		msg  bus.OutboundMessage
		want push.Category
		ok   bool
	}{
		{"a question", bus.OutboundMessage{Channel: "mobile", Metadata: map[string]interface{}{"type": "clarify_request"}}, push.Question, true},
		{"a finished background task", bus.OutboundMessage{Channel: "system", Content: "done", Metadata: map[string]interface{}{"type": "background_done"}}, push.Update, true},
		{"a reply in the shared conversation", bus.OutboundMessage{Channel: "mobile", Content: "Your reminder", Metadata: map[string]interface{}{"type": "assistant_message", "session_id": "main"}}, push.Update, true},
		{"a proactive message on the terminal channel", bus.OutboundMessage{Channel: "cli", Content: "Heads up"}, push.Update, true},
		{"progress telemetry", bus.OutboundMessage{Channel: "mobile", Content: "x", Metadata: map[string]interface{}{"type": "progress_event"}}, "", false},
		{"a surface update", bus.OutboundMessage{Channel: "mobile", Metadata: map[string]interface{}{"type": "surface_update"}}, "", false},
		{"an empty reply", bus.OutboundMessage{Channel: "mobile", Metadata: map[string]interface{}{"type": "assistant_message"}}, "", false},
		{"a routine outside the conversation", bus.OutboundMessage{Channel: "mobile", Content: "x", Metadata: map[string]interface{}{"type": "assistant_message", "session_id": "routine:abc"}}, "", false},
	}
	for _, c := range cases {
		got, ok := pushCategoryFor(c.msg)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
