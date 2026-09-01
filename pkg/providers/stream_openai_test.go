package providers

import (
	"strings"
	"testing"
)

func TestReadOpenAIStreamContent(t *testing.T) {
	p := &HTTPProvider{}
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: [DONE]\n\n"

	var chunks []string
	resp, err := p.readOpenAIStream(strings.NewReader(sse), func(c string) { chunks = append(chunks, c) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if resp.Content != "Hello" {
		t.Fatalf("expected 'Hello', got %q", resp.Content)
	}
	if strings.Join(chunks, "") != "Hello" {
		t.Fatalf("expected onChunk deltas, got %v", chunks)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("expected no tool calls, got %d", len(resp.ToolCalls))
	}
}

func TestReadOpenAIStreamToolCalls(t *testing.T) {
	p := &HTTPProvider{}
	sse := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"run\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"cmd\\\":\\\"ls\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"

	var chunks []string
	resp, err := p.readOpenAIStream(strings.NewReader(sse), func(c string) { chunks = append(chunks, c) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" || tc.Name != "run" {
		t.Fatalf("unexpected tool call: %+v", tc)
	}
	if tc.Arguments["cmd"] != "ls" {
		t.Fatalf("expected args {cmd:ls}, got %+v", tc.Arguments)
	}
}

func TestReadOpenAIStreamFinishReason(t *testing.T) {
	p := &HTTPProvider{}
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	resp, err := p.readOpenAIStream(strings.NewReader(sse), func(string) {})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("expected finish_reason=stop, got %q", resp.FinishReason)
	}
}
