package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHTTPProvider_ModelPrefixStripping verifies that the provider prefix is
// stripped from model names regardless of whether the separator is a slash
// (moonshot/kimi-k2.5) or a colon (deepseek:deepseek-flash).
func TestHTTPProvider_ModelPrefixStripping(t *testing.T) {
	cases := []struct {
		name     string
		inModel  string
		wantBody string
	}{
		{"slash separator", "deepseek/deepseek-flash", "deepseek-flash"},
		{"colon separator", "deepseek:deepseek-flash", "deepseek-flash"},
		{"no prefix", "deepseek-v4-pro", "deepseek-v4-pro"},
		{"unknown prefix left intact", "custom/x-model", "custom/x-model"},
		{"copilot prefix", "copilot/gpt-4.1", "gpt-4.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("failed to decode body: %v", err)
					return
				}
				gotModel = req.Model
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id":      "test",
					"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}},
				})
			}))
			defer server.Close()

			p := NewHTTPProvider("test-key", server.URL, "", "")
			_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, tc.inModel, map[string]interface{}{"max_tokens": 30})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotModel != tc.wantBody {
				t.Errorf("expected model %q, got %q", tc.wantBody, gotModel)
			}
		})
	}
}

// TestHTTPProvider_GetDefaultModel verifies the provider reports the model it
// was configured with, so the doctor health check can validate it.
func TestHTTPProvider_GetDefaultModel(t *testing.T) {
	p := NewHTTPProvider("test-key", "https://example.com", "", "")
	p.SetDefaultModel("deepseek:deepseek-flash")
	if got := p.GetDefaultModel(); got != "deepseek:deepseek-flash" {
		t.Errorf("expected default model %q, got %q", "deepseek:deepseek-flash", got)
	}
}

func TestToOpenAIMessages_VisualContentArray(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "look", MultiContent: []ContentPart{
			{Type: "text", Text: "what color"},
			{Type: "image_url", ImageURL: &ImageURL{URL: "data:image/png;base64,abc"}},
		}},
		{Role: "assistant", Content: "hi"},
	}
	out := toOpenAIMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	// The visual message must carry content as an array of blocks.
	b, _ := json.Marshal(out[0])
	s := string(b)
	for _, want := range []string{`"type":"text"`, `"type":"image_url"`, `data:image/png;base64,abc`} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in %s", want, s)
		}
	}
	// The non-visual message keeps a plain string content.
	s2, _ := json.Marshal(out[1])
	if !strings.Contains(string(s2), `"content":"hi"`) {
		t.Errorf("expected plain content in non-visual message, got %s", string(s2))
	}
}

func TestImageBase64Part(t *testing.T) {
	got := imageBase64Part("data:image/png;base64,QUJD")
	if got != "QUJD" {
		t.Errorf("got %q, want QUJD", got)
	}
	if imageBase64Part("https://x/y.png") != "" {
		t.Errorf("expected empty for non-data url")
	}
}

// TestHTTPProvider_DeepSeekThinkingDefaultsOff verifies Ghost disables
// DeepSeek thinking mode unless explicitly requested, using the provider's
// object toggle {"thinking":{"type":"disabled"|"enabled"}}.
func TestHTTPProvider_DeepSeekThinkingDefaultsOff(t *testing.T) {
	cases := []struct {
		name    string
		options map[string]interface{}
		want    string // want thinking.type; "" = field absent
	}{
		{"default off", map[string]interface{}{"max_tokens": 30}, "disabled"},
		{"explicit bool true", map[string]interface{}{"thinking": true}, "enabled"},
		{"explicit bool false", map[string]interface{}{"thinking": false}, "disabled"},
		{"level off", map[string]interface{}{"thinking_level": "off"}, "disabled"},
		{"level medium", map[string]interface{}{"thinking_level": "medium"}, "enabled"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotType string
			var hasThinking bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Model    string                 `json:"model"`
					Thinking map[string]interface{} `json:"thinking"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Errorf("failed to decode body: %v", err)
					return
				}
				if req.Thinking != nil {
					hasThinking = true
					gotType, _ = req.Thinking["type"].(string)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{
					"id":      "test",
					"choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "OK"}, "finish_reason": "stop"}},
				})
			}))
			defer server.Close()

			p := NewHTTPProvider("test-key", server.URL, "", "")
			_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil, "deepseek/deepseek-flash", tc.options)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !hasThinking {
				t.Fatalf("expected explicit thinking toggle, field absent")
			}
			if gotType != tc.want {
				t.Errorf("expected thinking.type %q, got %q", tc.want, gotType)
			}
		})
	}
}
