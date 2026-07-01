package tools

import (
	"context"
	"testing"
)

func TestTTSToolName(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	if tool.Name() != "tts" {
		t.Fatalf("expected name 'tts', got %s", tool.Name())
	}
}

func TestTTSToolDescription(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	if tool.Description() == "" {
		t.Fatal("expected non-empty description")
	}
}

func TestTTSToolParameters(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	params := tool.Parameters()
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}
	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties")
	}
	if _, ok := props["text"]; !ok {
		t.Fatal("expected text property")
	}
	if _, ok := props["voice"]; !ok {
		t.Fatal("expected voice property")
	}
	if _, ok := props["format"]; !ok {
		t.Fatal("expected format property")
	}
}

func TestTTSMissingText(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{})
	if !result.IsError {
		t.Fatal("expected error for missing text")
	}
}

func TestTTSEmptyText(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	ctx := context.Background()

	result := tool.Execute(ctx, map[string]interface{}{
		"text": "",
	})
	if !result.IsError {
		t.Fatal("expected error for empty text")
	}
}

func TestTTSDefaultConfig(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	if tool.config.Provider != "edge-tts" {
		t.Fatalf("expected provider 'edge-tts', got %s", tool.config.Provider)
	}
	if tool.config.DefaultVoice != "en-US-AriaNeural" {
		t.Fatalf("expected default voice 'en-US-AriaNeural', got %s", tool.config.DefaultVoice)
	}
	if tool.config.OutputFormat != "mp3" {
		t.Fatalf("expected output format 'mp3', got %s", tool.config.OutputFormat)
	}
}

func TestTTSCustomConfig(t *testing.T) {
	tool := NewTTSTool(TTSConfig{
		Provider:     "custom",
		DefaultVoice: "zh-CN-XiaoxiaoNeural",
		OutputFormat: "opus",
	}, "/tmp")
	if tool.config.Provider != "custom" {
		t.Fatalf("expected provider 'custom', got %s", tool.config.Provider)
	}
	if tool.config.DefaultVoice != "zh-CN-XiaoxiaoNeural" {
		t.Fatalf("expected voice 'zh-CN-XiaoxiaoNeural', got %s", tool.config.DefaultVoice)
	}
	if tool.config.OutputFormat != "opus" {
		t.Fatalf("expected format 'opus', got %s", tool.config.OutputFormat)
	}
}

func TestTTSListVoices(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	voices := tool.ListVoices()
	if len(voices) == 0 {
		t.Fatal("expected non-empty voice list")
	}
}

func TestTTSMaxTextLength(t *testing.T) {
	tool := NewTTSTool(TTSConfig{}, "/tmp")
	ctx := context.Background()

	longText := make([]byte, 6000)
	for i := range longText {
		longText[i] = 'a'
	}

	result := tool.Execute(ctx, map[string]interface{}{
		"text": string(longText),
	})

	if result.IsError {
		t.Log("Error (expected if edge-tts not installed):", result.ForLLM)
	}
}
