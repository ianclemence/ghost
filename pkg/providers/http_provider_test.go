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
// (moonshot/kimi-k2.5) or a colon (deepseek:deepseek-v4-flash).
func TestHTTPProvider_ModelPrefixStripping(t *testing.T) {
	cases := []struct {
		name     string
		inModel  string
		wantBody string
	}{
		{"slash separator", "deepseek/deepseek-v4-flash", "deepseek-v4-flash"},
		{"colon separator", "deepseek:deepseek-v4-flash", "deepseek-v4-flash"},
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
	p.SetDefaultModel("deepseek:deepseek-v4-flash")
	if got := p.GetDefaultModel(); got != "deepseek:deepseek-v4-flash" {
		t.Errorf("expected default model %q, got %q", "deepseek:deepseek-v4-flash", got)
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
