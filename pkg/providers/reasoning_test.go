// Ghost - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 Ghost contributors

package providers

import (
	"strings"
	"testing"
)

func TestReasoningFieldValue(t *testing.T) {
	for _, f := range reasoningFieldNames {
		if got := reasoningFieldValue(map[string]interface{}{f: "thought"}); got != "thought" {
			t.Fatalf("reasoningFieldValue(%q) = %q", f, got)
		}
	}
	if reasoningFieldValue(map[string]interface{}{"content": "hi"}) != "" {
		t.Fatal("content must not be reported as reasoning")
	}
}

func TestStripInlineReasoning(t *testing.T) {
	cases := map[string]string{
		"answer":                              "answer",
		"<think>secret</think>answer":         "answer",
		"<thinking>secret</thinking>answer":   "answer",
		"a<think>secret</think>b":             "ab",
		"<think>unclosed reasoning":           "",
		"answer<think>reasoning</think> more": "answer more",
	}
	for in, want := range cases {
		if got := stripInlineReasoning(in); got != want {
			t.Fatalf("stripInlineReasoning(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReasoningStreamFilterAcrossChunks(t *testing.T) {
	f := &reasoningStreamFilter{}
	var out strings.Builder
	// Tags deliberately split across chunks.
	for _, c := range []string{"Hel", "lo <thi", "nk>SECRET", "</thi", "nk> world"} {
		out.WriteString(f.Write(c))
	}
	out.WriteString(f.Flush())
	if got := out.String(); got != "Hello  world" {
		t.Fatalf("streamed sanitize = %q", got)
	}
}

// TestReadOpenAIStreamDropsReasoning covers interleaved reasoning fields and
// an inline <think> block: the stream must carry only answer text.
func TestReadOpenAIStreamDropsReasoning(t *testing.T) {
	p := &HTTPProvider{}
	sse := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning\":\"alt field\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_text\":\"third field\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" <think>inline secret</think>world\"}}]}\n\n" +
		"data: [DONE]\n\n"

	var chunks []string
	resp, err := p.readOpenAIStream(strings.NewReader(sse), func(c string) { chunks = append(chunks, c) })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Fatalf("content leaked reasoning: %q", resp.Content)
	}
	if got := strings.Join(chunks, ""); got != "Hello world" {
		t.Fatalf("onChunk leaked reasoning: %q", got)
	}
	if !strings.Contains(resp.ReasoningContent, "think ") {
		t.Fatalf("reasoning should be collected separately, got %q", resp.ReasoningContent)
	}
	if strings.Contains(resp.Content, "secret") || strings.Contains(resp.Content, "alt field") {
		t.Fatalf("reasoning field leaked into content: %q", resp.Content)
	}
}
