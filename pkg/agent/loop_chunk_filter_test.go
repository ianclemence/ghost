package agent

import "testing"

func TestShouldFilterAssistantChunk(t *testing.T) {
	tests := []struct {
		name   string
		chunk  string
		filter bool
	}{
		{name: "empty", chunk: "  ", filter: true},
		{name: "tool call prefix", chunk: "tool_call: read_file", filter: true},
		{name: "internal bracket log", chunk: "[Reading file...]", filter: true},
		{name: "skills xml", chunk: "<skills><skill></skill></skills>", filter: true},
		{name: "skill frontmatter", chunk: "name: weather\ndescription: test", filter: true},
		{name: "json metadata dump", chunk: "{\"metadata\":{},\"homepage\":\"x\",\"description\":\"y\"}", filter: true},
		{name: "normal assistant text", chunk: "Here is the forecast for today.", filter: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFilterAssistantChunk(tt.chunk)
			if got != tt.filter {
				t.Fatalf("expected filter=%v got=%v for chunk=%q", tt.filter, got, tt.chunk)
			}
		})
	}
}
