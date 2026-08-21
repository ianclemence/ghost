package proto

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		frame Frame
	}{
		{"CTL", Frame{Kind: KindCTL, StreamID: 0, Payload: []byte(`{"op":"welcome"}`)}},
		{"OPEN", Frame{Kind: KindOPEN, StreamID: 1, Payload: []byte(`{"type":"http","method":"POST","path":"/v1/chat"}`)}},
		{"DATA", Frame{Kind: KindDATA, StreamID: 1, Payload: []byte("hello world")}},
		{"END", Frame{Kind: KindEND, StreamID: 1, Payload: nil}},
		{"ERROR", Frame{Kind: KindERROR, StreamID: 1, Payload: []byte("timeout")}},
		{"PING", Frame{Kind: KindPING, StreamID: 0, Payload: nil}},
		{"PONG", Frame{Kind: KindPONG, StreamID: 0, Payload: nil}},
		{"large payload", Frame{Kind: KindDATA, StreamID: 42, Payload: bytes.Repeat([]byte("x"), 64*1024)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, &tt.frame); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}

			got, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}

			if got.Kind != tt.frame.Kind {
				t.Errorf("kind = %d, want %d", got.Kind, tt.frame.Kind)
			}
			if got.StreamID != tt.frame.StreamID {
				t.Errorf("streamID = %d, want %d", got.StreamID, tt.frame.StreamID)
			}
			if !bytes.Equal(got.Payload, tt.frame.Payload) {
				t.Errorf("payload mismatch: got %d bytes, want %d bytes", len(got.Payload), len(tt.frame.Payload))
			}
		})
	}
}

func TestWriteCTL(t *testing.T) {
	var buf bytes.Buffer
	ctrl := &Control{Op: OpWelcome, Version: 1}
	if err := WriteCTL(&buf, 0, ctrl); err != nil {
		t.Fatalf("WriteCTL: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.Kind != KindCTL {
		t.Errorf("kind = %d, want KindCTL", got.Kind)
	}
	if got.StreamID != 0 {
		t.Errorf("streamID = %d, want 0", got.StreamID)
	}

	parsed, err := ParseControl(got)
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}
	if parsed.Op != OpWelcome {
		t.Errorf("op = %q, want %q", parsed.Op, OpWelcome)
	}
	if parsed.Version != 1 {
		t.Errorf("version = %d, want 1", parsed.Version)
	}
}

func TestWriteOPEN(t *testing.T) {
	var buf bytes.Buffer
	meta := &HTTPMetadata{
		Type:   StreamHTTP,
		Method: "POST",
		Path:   "/v1/chat",
		Headers: map[string][]string{
			"Content-Type": {"application/json"},
		},
	}
	if err := WriteOPEN(&buf, 1, meta); err != nil {
		t.Fatalf("WriteOPEN: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.Kind != KindOPEN {
		t.Errorf("kind = %d, want KindOPEN", got.Kind)
	}
	if got.StreamID != 1 {
		t.Errorf("streamID = %d, want 1", got.StreamID)
	}

	parsed, err := ParseHTTPMeta(got)
	if err != nil {
		t.Fatalf("ParseHTTPMeta: %v", err)
	}
	if parsed.Method != "POST" {
		t.Errorf("method = %q, want POST", parsed.Method)
	}
	if parsed.Path != "/v1/chat" {
		t.Errorf("path = %q, want /v1/chat", parsed.Path)
	}
}

func TestWriteDATA(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("streaming data chunk")
	if err := WriteDATA(&buf, 5, data); err != nil {
		t.Fatalf("WriteDATA: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.Kind != KindDATA {
		t.Errorf("kind = %d, want KindDATA", got.Kind)
	}
	if got.StreamID != 5 {
		t.Errorf("streamID = %d, want 5", got.StreamID)
	}
	if !bytes.Equal(got.Payload, data) {
		t.Errorf("payload mismatch")
	}
}

func TestWriteEND(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteEND(&buf, 3); err != nil {
		t.Fatalf("WriteEND: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.Kind != KindEND {
		t.Errorf("kind = %d, want KindEND", got.Kind)
	}
	if got.StreamID != 3 {
		t.Errorf("streamID = %d, want 3", got.StreamID)
	}
	if len(got.Payload) != 0 {
		t.Errorf("payload should be empty, got %d bytes", len(got.Payload))
	}
}

func TestWriteERROR(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteERROR(&buf, 7, "device offline"); err != nil {
		t.Fatalf("WriteERROR: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if got.Kind != KindERROR {
		t.Errorf("kind = %d, want KindERROR", got.Kind)
	}
	if string(got.Payload) != "device offline" {
		t.Errorf("payload = %q, want %q", string(got.Payload), "device offline")
	}
}

func TestNextStreamID(t *testing.T) {
	if got := NextStreamID(0); got != 1 {
		t.Errorf("NextStreamID(0) = %d, want 1", got)
	}
	if got := NextStreamID(1); got != 3 {
		t.Errorf("NextStreamID(1) = %d, want 3", got)
	}
	if got := NextStreamID(3); got != 5 {
		t.Errorf("NextStreamID(3) = %d, want 5", got)
	}
}

func TestMultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	frames := []Frame{
		{Kind: KindCTL, StreamID: 0, Payload: []byte(`{"op":"welcome"}`)},
		{Kind: KindOPEN, StreamID: 1, Payload: []byte(`{"type":"http","method":"GET","path":"/v1/health"}`)},
		{Kind: KindEND, StreamID: 1},
		{Kind: KindPING, StreamID: 0},
	}

	for i := range frames {
		if err := WriteFrame(&buf, &frames[i]); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}

	for i, want := range frames {
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("ReadFrame %d: %v", i, err)
		}
		if got.Kind != want.Kind {
			t.Errorf("frame %d: kind = %d, want %d", i, got.Kind, want.Kind)
		}
		if got.StreamID != want.StreamID {
			t.Errorf("frame %d: streamID = %d, want %d", i, got.StreamID, want.StreamID)
		}
	}
}

func TestControlRoundTrip(t *testing.T) {
	ctrl := &Control{
		Op:      OpAddClients,
		Clients: []ClientEntry{{TokenHash: "abc123", Name: "phone"}, {TokenHash: "def456"}},
	}

	var buf bytes.Buffer
	if err := WriteCTL(&buf, 0, ctrl); err != nil {
		t.Fatalf("WriteCTL: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	parsed, err := ParseControl(got)
	if err != nil {
		t.Fatalf("ParseControl: %v", err)
	}

	if parsed.Op != OpAddClients {
		t.Errorf("op = %q, want %q", parsed.Op, OpAddClients)
	}
	if len(parsed.Clients) != 2 {
		t.Errorf("clients len = %d, want 2", len(parsed.Clients))
	}
	if parsed.Clients[0].Name != "phone" {
		t.Errorf("client 0 name = %q, want phone", parsed.Clients[0].Name)
	}
}

func TestHTTPResponseMetaRoundTrip(t *testing.T) {
	meta := &HTTPResponseMeta{
		Type:   StreamHTTP,
		Status: 200,
		Headers: map[string][]string{
			"Content-Type":  {"text/event-stream"},
			"Cache-Control": {"no-cache"},
		},
	}

	var buf bytes.Buffer
	if err := WriteOPEN(&buf, 1, meta); err != nil {
		t.Fatalf("WriteOPEN: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	parsed, err := ParseHTTPResponseMeta(got)
	if err != nil {
		t.Fatalf("ParseHTTPResponseMeta: %v", err)
	}

	if parsed.Status != 200 {
		t.Errorf("status = %d, want 200", parsed.Status)
	}
	if parsed.Headers["Content-Type"][0] != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", parsed.Headers["Content-Type"][0])
	}
}
