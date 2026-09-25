package main

import "testing"

// The gateway's transcript safety net: a turn that produced text but
// streamed nothing (approval resumes, denials, deterministic answers,
// non-streaming providers) must still reach the SSE client as one frame.
// Turns that already streamed must NOT be double-printed.
func TestEmitUnstreamedReply(t *testing.T) {
	cases := []struct {
		name     string
		response string
		streamed bool
		want     []string
	}{
		{name: "silent turn with text", response: "2 pending", streamed: false, want: []string{"2 pending"}},
		{name: "streamed turn is not repeated", response: "full text", streamed: true, want: nil},
		{name: "silent empty turn emits nothing", response: "", streamed: false, want: nil},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			emitUnstreamedReply(tt.response, tt.streamed, func(s string) { got = append(got, s) })
			if len(got) != len(tt.want) {
				t.Fatalf("emitted %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("emitted[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
