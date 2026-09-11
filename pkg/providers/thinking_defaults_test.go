package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/openai/openai-go/v3/shared"
)

// The agent loop's default options: thinking off unless /think opts in.
// Every case below uses exactly these — thinking must stay off on every
// provider, including deepseek-flash, without any explicit suppression.
func loopDefaultOptions() map[string]interface{} {
	return map[string]interface{}{
		"max_tokens":     30,
		"temperature":    0.6,
		"thinking_level": "off",
	}
}

func openAIChatReply(t *testing.T, w http.ResponseWriter, r *http.Request) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Errorf("decode request body: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      "test",
		"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}},
	})
	return body
}

// Generic OpenAI-compatible providers (openai/groq/openrouter/...) must
// carry zero thinking footprint by default.
func TestThinkingOffGenericProviders(t *testing.T) {
	for _, model := range []string{"gpt-4o", "llama-3.3-70b-versatile", "openrouter/anthropic:claude-sonnet-4-5"} {
		var body map[string]interface{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body = openAIChatReply(t, w, r)
		}))
		p := NewHTTPProvider("test-key", server.URL, "", "")
		_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, model, loopDefaultOptions())
		server.Close()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", model, err)
		}
		if _, ok := body["thinking"]; ok {
			t.Errorf("%s: thinking key must be absent by default, body has %v", model, body["thinking"])
		}
		if _, ok := body["reasoning_effort"]; ok {
			t.Errorf("%s: reasoning_effort must be absent by default", model)
		}
	}
}

// DeepSeek-flash (the production model) must state disabled explicitly:
// the server default is effort-high thinking, which slows responses and
// 400s tool loops that don't echo reasoning_content.
func TestThinkingOffDeepSeekFlash(t *testing.T) {
	var thinkType string
	hasThinking := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := openAIChatReply(t, w, r)
		if th, ok := body["thinking"].(map[string]interface{}); ok {
			hasThinking = true
			thinkType, _ = th["type"].(string)
		}
	}))
	defer server.Close()
	p := NewHTTPProvider("test-key", server.URL, "", "")
	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "deepseek/deepseek-flash", loopDefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasThinking || thinkType != "disabled" {
		t.Fatalf("deepseek-flash must send explicit disabled toggle, got present=%v type=%q", hasThinking, thinkType)
	}
}

// Ollama native mapping defaults to think:false.
func TestOllamaThinkDefaultsOff(t *testing.T) {
	if got := ollamaThinkParam(loopDefaultOptions()); got != false {
		t.Fatalf("default must map to false, got %v", got)
	}
	if got := ollamaThinkParam(nil); got != false {
		t.Fatalf("nil options must map to false, got %v", got)
	}
	if got := ollamaThinkParam(map[string]interface{}{"thinking": true}); got != true {
		t.Fatalf("explicit opt-in must survive, got %v", got)
	}
}

// Moonshot sends no thinking object by default.
func TestMoonshotDefaultsOff(t *testing.T) {
	var got *kimiThinking
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req kimiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		got = req.Thinking
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(kimiResponse{ID: "t", Choices: []struct {
			Index        int         `json:"index"`
			Message      kimiMessage `json:"message"`
			FinishReason string      `json:"finish_reason"`
		}{{Index: 0, Message: kimiMessage{Role: "assistant", Content: "OK"}, FinishReason: "stop"}}})
	}))
	defer server.Close()
	p := NewMoonshotProvider("test-key", server.URL)
	if _, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "kimi-k2.5", loopDefaultOptions()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil && got.Type != "disabled" {
		t.Fatalf("moonshot must send no thinking object, or explicit disabled, by default; got %+v", got)
	}
}

// Anthropic sends no thinking param by default (opt-in only).
func TestAnthropicDefaultsOff(t *testing.T) {
	params, err := buildAnthropicParams([]Message{{Role: "user", Content: "hi"}}, nil, "claude-opus-4-6", 1024, loopDefaultOptions())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	empty := anthropic.MessageNewParams{}
	if params.Thinking != empty.Thinking {
		t.Fatalf("anthropic must send no thinking param by default: %+v", params.Thinking)
	}
	if params.Temperature.Value == 0 {
		t.Fatal("temperature must survive (thinking must not blank it when off)")
	}
}

// Codex resolves off to explicit none where supported, else backend
// default (never a requested effort).
func TestCodexDefaultsOff(t *testing.T) {
	got := codexReasoning("gpt-5.2", loopDefaultOptions())
	if got.Effort != shared.ReasoningEffortNone && got != (shared.ReasoningParam{}) {
		t.Fatalf("codex off must be none-or-absent, got %+v", got)
	}
}
