package tools

import (
	"strings"
	"testing"
)

func TestAttachPageEvidence(t *testing.T) {
	res := &ToolResult{
		ForLLM:   `{"url":"https://example.com/pricing","title":"Pricing","text":"Plans start at $9"}`,
		Evidence: map[string]interface{}{"op": "browser.snapshot"},
	}
	attachPageEvidence(res)
	if res.Evidence["url"] != "https://example.com/pricing" {
		t.Fatalf("url missing: %v", res.Evidence)
	}
	if res.Evidence["domain"] != "example.com" {
		t.Fatalf("domain missing: %v", res.Evidence)
	}
	if res.Evidence["title"] != "Pricing" {
		t.Fatalf("title missing: %v", res.Evidence)
	}
	if res.Evidence["text"] != "Plans start at $9" {
		t.Fatalf("text missing: %v", res.Evidence)
	}
}

func TestAttachPageEvidenceBoundsAndRedacts(t *testing.T) {
	res := &ToolResult{
		ForLLM:   `{"url":"https://example.com/","text":"token sk-live-0123456789abcdef ` + strings.Repeat("x", 9000) + `"}`,
		Evidence: map[string]interface{}{},
	}
	attachPageEvidence(res)
	text, _ := res.Evidence["text"].(string)
	if strings.Contains(text, "sk-live-0123456789abcdef") {
		t.Fatalf("secret-shaped page text must be redacted")
	}
	if len([]rune(text)) > pageEvidenceBound {
		t.Fatalf("text exceeds bound: %d", len([]rune(text)))
	}
}

func TestAttachPageEvidenceIgnoresGarbage(t *testing.T) {
	for _, raw := range []string{"", "not json", `{"nope":true}`, `["a"]`, `42`} {
		res := &ToolResult{ForLLM: raw, Evidence: map[string]interface{}{"op": "x"}}
		attachPageEvidence(res)
		if len(res.Evidence) != 1 {
			t.Fatalf("garbage %q must leave evidence untouched: %v", raw, res.Evidence)
		}
	}
	attachPageEvidence(nil)
	attachPageEvidence(&ToolResult{Evidence: nil})
}
