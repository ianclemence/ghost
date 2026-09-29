package providers

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// toolLoopTranscript is one assistant turn that called two tools in
// parallel, followed by both results — the shape every multi-step turn
// sends on its second model call.
func toolLoopTranscript() []Message {
	return []Message{
		{Role: "system", Content: "You are Ghost."},
		{Role: "user", Content: "Weather and time in Paris?"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "tu_1", Name: "weather", Arguments: map[string]interface{}{"city": "Paris"}},
			{ID: "tu_2", Type: "function", Function: &FunctionCall{Name: "clock", Arguments: `{"tz":"Europe/Paris"}`}},
		}},
		{Role: "tool", ToolCallID: "tu_1", Content: "sunny"},
		{Role: "tool", ToolCallID: "tu_2", Content: "12:00"},
	}
}

func requestJSON(t *testing.T, model string, msgs []Message, tools []ToolDefinition, opts map[string]interface{}) map[string]interface{} {
	t.Helper()
	params, err := buildAnthropicParams(msgs, tools, model, 8192, opts)
	if err != nil {
		t.Fatalf("%s: build: %v", model, err)
	}
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("%s: marshal: %v", model, err)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}

// The assistant's tool_use blocks must be sent back, every tool_result must
// answer one, results of a parallel call share one user turn, and no text
// block is empty. Each violation is a 400 from the Messages API; before this
// the assistant turn went out as a single empty text block.
func TestAnthropicToolLoopRequestShape(t *testing.T) {
	req := requestJSON(t, "claude-opus-5-5", toolLoopTranscript(), nil, loopDefaultOptions())
	msgs := req["messages"].([]interface{})
	if len(msgs) != 3 {
		t.Fatalf("want user, assistant(tool_use), user(tool_results); got %d messages: %v", len(msgs), msgs)
	}
	asst := msgs[1].(map[string]interface{})
	blocks := asst["content"].([]interface{})
	var uses []string
	for _, b := range blocks {
		blk := b.(map[string]interface{})
		if blk["type"] == "text" && strings.TrimSpace(blk["text"].(string)) == "" {
			t.Fatal("empty text block sent")
		}
		if blk["type"] == "tool_use" {
			uses = append(uses, blk["id"].(string))
			if _, ok := blk["input"].(map[string]interface{}); !ok {
				t.Fatalf("tool_use input must be an object: %v", blk["input"])
			}
		}
	}
	if strings.Join(uses, ",") != "tu_1,tu_2" {
		t.Fatalf("assistant tool_use blocks = %v, want tu_1,tu_2", uses)
	}
	results := msgs[2].(map[string]interface{})["content"].([]interface{})
	if len(results) != 2 {
		t.Fatalf("parallel results must share one user turn, got %d blocks", len(results))
	}
	for i, id := range []string{"tu_1", "tu_2"} {
		if got := results[i].(map[string]interface{})["tool_use_id"]; got != id {
			t.Fatalf("result %d answers %v, want %s", i, got, id)
		}
	}
	// The clock call came in OpenAI shape; its JSON-string arguments must
	// arrive parsed.
	second := blocks[len(blocks)-1].(map[string]interface{})
	if second["name"] != "clock" || second["input"].(map[string]interface{})["tz"] != "Europe/Paris" {
		t.Fatalf("function-shaped tool call not converted: %v", second)
	}
}

// Tool descriptions are how the model chooses a tool; they were dropped.
func TestAnthropicToolDescriptionsSent(t *testing.T) {
	tools := []ToolDefinition{{Type: "function", Function: ToolFunctionDefinition{
		Name: "weather", Description: "Current weather for a city.",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
	}}}
	req := requestJSON(t, "claude-opus-5-5", []Message{{Role: "user", Content: "hi"}}, tools, loopDefaultOptions())
	tool := req["tools"].([]interface{})[0].(map[string]interface{})
	if tool["description"] != "Current weather for a city." {
		t.Fatalf("tool description missing: %v", tool)
	}
}

// Per-model request rules. Each forbidden field below is a 400 on that
// model; the default loop options (temperature 0.7, thinking off) must
// never produce one.
func TestAnthropicPerModelRequestRules(t *testing.T) {
	cases := []struct {
		model           string
		wantTemperature bool
		wantThinking    string // "" = absent
		wantEffort      string // "" = absent
	}{
		{"claude-opus-5-5", false, "", "low"},
		{"claude-sonnet-5-5", false, "", "low"},
		{"claude-fable-5-1", false, "", "low"},
		{"claude-fable-5", false, "", "low"},
		{"claude-opus-5", false, "disabled", ""},
		{"claude-sonnet-5", false, "disabled", ""},
		{"claude-opus-4-8", false, "", ""},
		{"claude-opus-4-7", false, "", ""},
		{"claude-sonnet-4-6", true, "", ""},
		{"claude-haiku-4-5", true, "", ""},
		{"claude-haiku-4-5-20251001", true, "", ""},
		{"anthropic/claude-opus-5-5", false, "", "low"},
	}
	for _, tc := range cases {
		req := requestJSON(t, tc.model, []Message{{Role: "user", Content: "hi"}}, nil, loopDefaultOptions())
		_, hasTemp := req["temperature"]
		if hasTemp != tc.wantTemperature {
			t.Errorf("%s: temperature present=%v, want %v", tc.model, hasTemp, tc.wantTemperature)
		}
		gotThinking := ""
		if th, ok := req["thinking"].(map[string]interface{}); ok {
			gotThinking, _ = th["type"].(string)
		}
		if gotThinking != tc.wantThinking {
			t.Errorf("%s: thinking=%q, want %q", tc.model, gotThinking, tc.wantThinking)
		}
		gotEffort := ""
		if oc, ok := req["output_config"].(map[string]interface{}); ok {
			gotEffort, _ = oc["effort"].(string)
		}
		if gotEffort != tc.wantEffort {
			t.Errorf("%s: effort=%q, want %q", tc.model, gotEffort, tc.wantEffort)
		}
	}
}

// Opting into thinking on the frontier models is adaptive with an effort;
// xhigh is a real level from 4.7 on and steps up to max before that.
func TestAnthropicThinkingOnFrontier(t *testing.T) {
	opts := map[string]interface{}{"temperature": 0.7, "thinking_level": "xhigh"}
	req := requestJSON(t, "claude-opus-5-5", []Message{{Role: "user", Content: "hi"}}, nil, opts)
	if th := req["thinking"].(map[string]interface{}); th["type"] != "adaptive" {
		t.Fatalf("want adaptive, got %v", th)
	}
	if e := req["output_config"].(map[string]interface{})["effort"]; e != "xhigh" {
		t.Fatalf("opus 5.5 xhigh effort = %v", e)
	}
	if _, ok := req["temperature"]; ok {
		t.Fatal("temperature must not accompany thinking")
	}
	req = requestJSON(t, "claude-opus-4-6", []Message{{Role: "user", Content: "hi"}}, nil, opts)
	if e := req["output_config"].(map[string]interface{})["effort"]; e != "max" {
		t.Fatalf("opus 4.6 has no xhigh; want max, got %v", e)
	}
}

// A refusal is never an answer: partial text is discarded and the empty
// response lets the fallback chain move on. Cache usage is reported and
// counted in the prompt total.
func TestParseAnthropicRefusalAndCacheUsage(t *testing.T) {
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5",
		"content":[{"type":"text","text":"Sure, here is"}],"stop_reason":"refusal",
		"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":900,"cache_creation_input_tokens":100}}`), &msg); err != nil {
		t.Fatal(err)
	}
	resp := parseAnthropicResponse(&msg)
	if resp.FinishReason != "refusal" || resp.Content != "" {
		t.Fatalf("refusal must yield empty content, got %q (%s)", resp.Content, resp.FinishReason)
	}
	if usableResponse(resp) {
		t.Fatal("a refusal must not count as a usable response")
	}
	u := resp.Usage
	if u.PromptTokens != 1010 || u.CacheReadTokens != 900 || u.CacheWriteTokens != 100 || u.TotalTokens != 1013 {
		t.Fatalf("usage = %+v", u)
	}
}

func TestAnthropicGenerationParseDated(t *testing.T) {
	for model, want := range map[string][2]int{
		"claude-haiku-4-5-20251001":  {4, 5},
		"claude-opus-5-5":            {5, 5},
		"claude-fable-5-1":           {5, 1},
		"claude-3-5-sonnet-20240620": {3, 5},
		"claude-opus-4-5@20251101":   {4, 5},
		"anthropic/claude-sonnet-5":  {5, 0},
	} {
		maj, min, ok := anthropicModelGeneration(model)
		if !ok || maj != want[0] || min != want[1] {
			t.Errorf("%s: got %d.%d ok=%v, want %d.%d", model, maj, min, ok, want[0], want[1])
		}
	}
}

// The cache breakpoint sits on the stable part of the system prompt only;
// per-turn state (time) follows it uncached, and the marker never reaches
// the model.
func TestAnthropicSystemCacheBoundary(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "# Ghost\nstable rules\n\n" + SystemPromptCacheBoundary + "\n\n## Current Time\n2026-09-29 15:04", CacheControl: &CacheControl{Type: "ephemeral"}},
		{Role: "user", Content: "hi"},
	}
	req := requestJSON(t, "claude-opus-5-5", msgs, nil, loopDefaultOptions())
	sys := req["system"].([]interface{})
	if len(sys) != 2 {
		t.Fatalf("want stable + volatile blocks, got %d: %v", len(sys), sys)
	}
	stable, volatile := sys[0].(map[string]interface{}), sys[1].(map[string]interface{})
	if stable["cache_control"] == nil || strings.Contains(stable["text"].(string), "Current Time") {
		t.Fatalf("stable block must be cached and time-free: %v", stable)
	}
	if volatile["cache_control"] != nil || !strings.Contains(volatile["text"].(string), "15:04") {
		t.Fatalf("volatile block must follow uncached: %v", volatile)
	}
	for _, b := range sys {
		if strings.Contains(b.(map[string]interface{})["text"].(string), SystemPromptCacheBoundary) {
			t.Fatal("boundary marker leaked to the model")
		}
	}
}

// Cache hits from OpenAI-compatible providers are normalized so turn
// telemetry can show whether the prompt prefix is actually being reused.
func TestUsageInfoReadsProviderCacheHits(t *testing.T) {
	for body, want := range map[string]int{
		`{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_cache_hit_tokens":64}`: 64,
		`{"prompt_tokens":100,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":32}}`:    32,
		`{"prompt_tokens":100,"completion_tokens":5,"cache_read_tokens":16}`:                          16,
		`{"prompt_tokens":100,"completion_tokens":5}`:                                                 0,
	} {
		var u UsageInfo
		if err := json.Unmarshal([]byte(body), &u); err != nil {
			t.Fatal(err)
		}
		if u.CacheReadTokens != want || u.PromptTokens != 100 {
			t.Errorf("%s: got %+v, want cache %d", body, u, want)
		}
	}
}

// A failed tool call reaches the model flagged as an error.
func TestAnthropicToolErrorFlag(t *testing.T) {
	msgs := toolLoopTranscript()
	msgs[3].ToolError = true
	msgs[4].Content = ""
	req := requestJSON(t, "claude-opus-5-5", msgs, nil, loopDefaultOptions())
	results := req["messages"].([]interface{})[2].(map[string]interface{})["content"].([]interface{})
	if results[0].(map[string]interface{})["is_error"] != true {
		t.Fatalf("failed tool must carry is_error: %v", results[0])
	}
	if results[1].(map[string]interface{})["is_error"] == true {
		t.Fatal("successful tool flagged as error")
	}
	if !strings.Contains(fmt.Sprint(results[1]), "(no output)") {
		t.Fatalf("empty result must be explicit: %v", results[1])
	}
}
